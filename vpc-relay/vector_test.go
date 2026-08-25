package main

import (
	"context"
	"database/sql"
	"math"
	"testing"

	_ "modernc.org/sqlite"
)

func TestFloatsBytesRoundtrip(t *testing.T) {
	in := []float32{0.1, -0.5, 1.0, 0.0, 3.14159}
	back := bytesToFloats(floatsToBytes(in))
	if len(back) != len(in) {
		t.Fatalf("length mismatch %d != %d", len(back), len(in))
	}
	for i := range in {
		if math.Abs(float64(back[i]-in[i])) > 1e-6 {
			t.Fatalf("value mismatch at %d: %f != %f", i, back[i], in[i])
		}
	}
}

func TestCosine(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}
	if cosine(a, b) != 1 {
		t.Errorf("cosine(same) = %f, want 1", cosine(a, b))
	}
	c := []float32{0, 1, 0}
	if cosine(a, c) != 0 {
		t.Errorf("cosine(orthogonal) = %f, want 0", cosine(a, c))
	}
	d := []float32{-1, 0, 0}
	if cosine(a, d) != -1 {
		t.Errorf("cosine(opposite) = %f, want -1", cosine(a, d))
	}
	if cosine([]float32{}, []float32{1}) != 0 {
		t.Error("cosine(mismatched dims) should be 0")
	}
}

func TestHashEmbedDeterministicAndNormalized(t *testing.T) {
	v1 := hashEmbed("qui peut m'aider à réparer un vélo")
	v2 := hashEmbed("qui peut m'aider à réparer un vélo")
	if len(v1) != hashEmbedDims {
		t.Fatalf("dims = %d, want %d", len(v1), hashEmbedDims)
	}
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Fatal("hash embedding is not deterministic")
		}
	}
	var norm float32
	for _, x := range v1 {
		norm += x * x
	}
	if norm == 0 {
		t.Fatal("hash embedding is all zeros")
	}
	if math.Abs(float64(norm)-1.0) > 1e-3 {
		t.Errorf("norm = %f, want ~1", norm)
	}
	// Related texts should be more similar than unrelated ones.
	rel := hashEmbed("réparer un vélo, réparation de bicyclette")
	unrel := hashEmbed("météo demain à Paris")
	if cosine(v1, rel) <= cosine(v1, unrel) {
		t.Errorf("related text should score higher: rel=%f unrel=%f", cosine(v1, rel), cosine(v1, unrel))
	}
}

func TestSemanticSearchRanking(t *testing.T) {
	openTestDB(t)
	defer db.Close()
	// Fresh vector index state (other tests in the package share the globals).
	globalVectorIndex.mu.Lock()
	globalVectorIndex.byType = map[string][]vectorEntry{}
	globalVectorIndex.loaded = false
	globalVectorIndex.mu.Unlock()
	initVectorStore()

	// Store known embeddings directly (bypassing the embed backend).
	if err := upsertEmbedding("contact", "+33611111111", "test", "vélo", []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := upsertEmbedding("contact", "+33622222222", "test", "météo", []float32{0, 1, 0}); err != nil {
		t.Fatal(err)
	}
	if err := upsertEmbedding("note", "n1", "test", "vélo aussi", []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}

	// Fake the embedding backend to return a query vector + "test" model.
	orig := embedTextFn
	defer func() { embedTextFn = orig }()
	embedTextFn = func(ctx context.Context, text string) ([]float32, string, error) {
		return []float32{1, 0, 0}, "test", nil
	}

	hits, err := SemanticSearch(context.Background(), "", "vélo", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Fatalf("hits = %d, want 3", len(hits))
	}
	// "+33611111111" and "n1" (vélo) should rank above "+33622222222" (météo).
	if hits[0].EntityID != "+33611111111" && hits[0].EntityID != "n1" {
		t.Errorf("top hit = %s, want a vélo entity", hits[0].EntityID)
	}
	if hits[2].EntityID != "+33622222222" {
		t.Errorf("last hit = %s, want +33622222222", hits[2].EntityID)
	}
}

func openTestDB(t *testing.T) {
	t.Helper()
	var err error
	db, err = sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// :memory: is per-connection; pin to one so DDL and DML share the same DB.
	db.SetMaxOpenConns(1)
}
