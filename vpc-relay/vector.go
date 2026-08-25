package main

// Vector memory — semantic search over the node's own knowledge (contacts,
// vault notes, missions, peers). Sovereignty-first: vectors live in SQLite
// (durable source of truth), an in-memory index is built at boot and kept
// warm for sub-millisecond cosine search. Embeddings are computed by a
// pluggable backend — local llama.cpp sidecar first (sovereign), a cloud
// OpenAI-compatible /embeddings endpoint second. No C extension, no vector
// DB engine: at the node's scale (hundreds/thousands of vectors) a pure-Go
// brute-force cosine is more than fast enough, and the heavy part (the
// neural embedding) already runs in C++ inside the llama.cpp sidecar.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// ─── Storage ───

type vectorEntry struct {
	ID       int64
	EntityID string
	Model    string
	Vec      []float32
}

// VectorHit is one semantic-search result.
type VectorHit struct {
	EntityType string  `json:"entity_type"`
	EntityID   string  `json:"entity_id"`
	Score      float32 `json:"score"`
	Model      string  `json:"model,omitempty"`
}

// vectorIndex keeps embeddings in RAM for fast cosine ranking. Loaded from
// SQLite at boot, refreshed whenever an embedding is upserted.
type vectorIndex struct {
	mu     sync.RWMutex
	byType map[string][]vectorEntry
	loaded bool
}

var globalVectorIndex = &vectorIndex{byType: map[string][]vectorEntry{}}

func initVectorStore() {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS gafam_embeddings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		entity_type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		model TEXT DEFAULT '',
		dims INTEGER NOT NULL DEFAULT 0,
		vec BLOB NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		log.Printf("vector: table creation failed: %v", err)
		return
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_embeddings_entity ON gafam_embeddings(entity_type, entity_id, model)`); err != nil {
		log.Printf("vector: index creation failed: %v", err)
	}
	// Synchronous at boot: a few thousand vectors load in microseconds.
	globalVectorIndex.reload()
	log.Println("vector: store ready")
}

func (ix *vectorIndex) reload() {
	rows, err := db.Query(`SELECT entity_type, entity_id, model, vec FROM gafam_embeddings`)
	if err != nil {
		log.Printf("vector: reload query failed: %v", err)
		return
	}
	defer rows.Close()

	byType := map[string][]vectorEntry{}
	for rows.Next() {
		var etype, eid, model string
		var blob []byte
		if err := rows.Scan(&etype, &eid, &model, &blob); err != nil {
			continue
		}
		byType[etype] = append(byType[etype], vectorEntry{EntityID: eid, Model: model, Vec: bytesToFloats(blob)})
	}
	ix.mu.Lock()
	ix.byType = byType
	ix.loaded = true
	ix.mu.Unlock()
}

func (ix *vectorIndex) ensureLoaded() {
	ix.mu.RLock()
	loaded := ix.loaded
	ix.mu.RUnlock()
	if !loaded {
		ix.reload()
	}
}

// upsertEntry stores an embedding and keeps the in-memory index warm.
func upsertEmbedding(entityType, entityID, model, text string, vec []float32) error {
	if len(vec) == 0 {
		return fmt.Errorf("empty vector")
	}
	blob := floatsToBytes(vec)
	_, err := db.Exec(
		`INSERT INTO gafam_embeddings (entity_type, entity_id, model, dims, vec, updated_at)
		 VALUES (?, ?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(entity_type, entity_id, model) DO UPDATE SET
		   vec = excluded.vec, dims = excluded.dims, updated_at = excluded.updated_at`,
		entityType, entityID, model, len(vec), blob,
	)
	if err != nil {
		return err
	}
	// Warm the in-memory index after the write.
	globalVectorIndex.reload()
	return nil
}

// ─── Serialization ───

func floatsToBytes(f []float32) []byte {
	b := make([]byte, len(f)*4)
	for i, v := range f {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
	}
	return b
}

func bytesToFloats(b []byte) []float32 {
	f := make([]float32, len(b)/4)
	for i := range f {
		f[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return f
}

// ─── Similarity ───

func cosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / float32(math.Sqrt(float64(na))*math.Sqrt(float64(nb)))
}

// SemanticSearch embeds the query and returns the top-k most similar stored
// entities (optionally restricted to one entity type).
func SemanticSearch(ctx context.Context, entityType, query string, k int) ([]VectorHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("empty query")
	}
	if k <= 0 || k > 50 {
		k = 5
	}
	qvec, qmodel, err := embedTextFn(ctx, query)
	if err != nil {
		return nil, err
	}

	globalVectorIndex.ensureLoaded()
	globalVectorIndex.mu.RLock()
	defer globalVectorIndex.mu.RUnlock()

	type scored struct {
		etype, eid string
		score      float32
	}
	var hits []scored
	for et, entries := range globalVectorIndex.byType {
		if entityType != "" && et != entityType {
			continue
		}
		for _, e := range entries {
			// Only compare vectors from the same embedding backend — mixing
			// hash/wordvec/llama vectors would rank garbage.
			if e.Model != qmodel {
				continue
			}
			hits = append(hits, scored{et, e.EntityID, cosine(qvec, e.Vec)})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > k {
		hits = hits[:k]
	}
	out := make([]VectorHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, VectorHit{EntityType: h.etype, EntityID: h.eid, Score: h.score})
	}
	return out, nil
}

// ─── Embedding backend (pluggable) ───

// embedTextFn is the active embedding implementation — a package var so tests
// can inject a deterministic fake.
var embedTextFn = embedText

// embedText computes an embedding for text, pure-Go first:
//  1. static word vectors (EMBED_VEC_PATH) — real semantics, offline
//  2. local llama.cpp sidecar (EMBED_URL) — sovereign, no API key
//  3. cloud OpenAI-compatible /embeddings (embedding_config)
//  4. feature hashing — always available, zero dependency (default)
func embedText(ctx context.Context, text string) ([]float32, string, error) {
	clean := strings.Join(strings.Fields(text), " ")
	if clean == "" {
		return nil, "", fmt.Errorf("empty text to embed")
	}

	if p := os.Getenv("EMBED_VEC_PATH"); p != "" {
		if v, ok := wordVecEmbed(clean); ok {
			return v, "wordvec", nil
		}
	}

	if u := os.Getenv("EMBED_URL"); u != "" {
		if v, model, err := embedLocal(ctx, strings.TrimRight(u, "/"), clean); err == nil {
			return v, model, nil
		}
	}

	if v, model, err := embedCloud(ctx, clean); err == nil {
		return v, model, nil
	}

	return hashEmbed(clean), "hash", nil
}

// embedLocal talks to llama.cpp's OpenAI-compatible /v1/embeddings, falling
// back to the legacy /embedding endpoint.
func embedLocal(ctx context.Context, base, text string) ([]float32, string, error) {
	client := &http.Client{Timeout: 60 * time.Second}

	// OpenAI-style
	payload, _ := json.Marshal(map[string]interface{}{"input": text})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/embeddings", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if resp, err := client.Do(req); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode < 400 {
			var out struct {
				Data []struct {
					Embedding []float32 `json:"embedding"`
				} `json:"data"`
			}
			if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out) == nil && len(out.Data) > 0 {
				return out.Data[0].Embedding, "local", nil
			}
		}
	}

	// Legacy /embedding
	legacy, _ := json.Marshal(map[string]string{"content": text})
	req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/embedding", bytes.NewReader(legacy))
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req2)
	if err != nil {
		return nil, "", err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode >= 400 {
		return nil, "", fmt.Errorf("embedding endpoint HTTP %d", resp2.StatusCode)
	}
	var out2 struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(io.LimitReader(resp2.Body, 1<<20)).Decode(&out2); err != nil {
		return nil, "", err
	}
	if len(out2.Embedding) == 0 {
		return nil, "", fmt.Errorf("empty embedding")
	}
	return out2.Embedding, "local", nil
}

// embedCloud uses a configured OpenAI-compatible provider's /embeddings.
func embedCloud(ctx context.Context, text string) ([]float32, string, error) {
	raw := getSetting("embedding_config")
	if raw == "" {
		return nil, "", fmt.Errorf("no embedding_config")
	}
	var cfg struct {
		ProviderID string `json:"provider_id"`
		Model      string `json:"model"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil || cfg.ProviderID == "" {
		return nil, "", fmt.Errorf("invalid embedding_config")
	}
	p, ok := findProvider(cfg.ProviderID)
	if !ok {
		return nil, "", fmt.Errorf("embedding provider not found: %s", cfg.ProviderID)
	}
	model := cfg.Model
	if model == "" {
		model = p.Model
	}
	payload, _ := json.Marshal(map[string]interface{}{"model": model, "input": text})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/embeddings", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, "", fmt.Errorf("embeddings HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, "", err
	}
	if len(out.Data) == 0 {
		return nil, "", fmt.Errorf("empty embeddings response")
	}
	return out.Data[0].Embedding, model, nil
}
