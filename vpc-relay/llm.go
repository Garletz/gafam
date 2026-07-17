package main

// LLM providers (Vātāyana-tier external models — e.g. Kimi K3) and the
// orchestration engine router. One entry point — chatWithActiveEngine — is
// what Kāraka tools, quests and future lucioles call, no matter which engine
// the user picked in the Suparna → Provider tab.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// ─── Types & storage ───

type LLMProvider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key,omitempty"`
	KeyHint string `json:"key_hint,omitempty"`
	Model   string `json:"model"`
	Enabled bool   `json:"enabled"`
}

const (
	settingLLMProviders = "llm_providers"
	settingLLMEngine    = "llm_engine"
)

func getSetting(key string) string {
	var v string
	row := db.QueryRow("SELECT value FROM gafam_settings WHERE key = ?", key)
	if err := row.Scan(&v); err != nil {
		return ""
	}
	return v
}

func setSetting(key, value string) error {
	_, err := db.Exec(
		"INSERT INTO gafam_settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	return err
}

func loadLLMProviders() []LLMProvider {
	raw := getSetting(settingLLMProviders)
	if raw == "" {
		return []LLMProvider{}
	}
	var out []LLMProvider
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		log.Printf("llm: corrupt providers setting, resetting: %v", err)
		return []LLMProvider{}
	}
	return out
}

func saveLLMProviders(list []LLMProvider) error {
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return setSetting(settingLLMProviders, string(raw))
}

func maskedProvider(p LLMProvider) LLMProvider {
	if p.APIKey != "" {
		n := len(p.APIKey)
		if n > 4 {
			p.KeyHint = "…" + p.APIKey[n-4:]
		} else {
			p.KeyHint = "…"
		}
		p.APIKey = ""
	}
	return p
}

func findProvider(id string) (LLMProvider, bool) {
	for _, p := range loadLLMProviders() {
		if p.ID == id {
			return p, true
		}
	}
	return LLMProvider{}, false
}

func newProviderID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "p_" + hex.EncodeToString(b)
}

// activeEngine returns "vpc" | "phone" | "provider:<id>".
func activeEngine() string {
	e := getSetting(settingLLMEngine)
	if e == "" {
		return "vpc"
	}
	return e
}

// ─── Engine router ───

type chatResult struct {
	Content   string `json:"content"`
	Engine    string `json:"engine"`
	Model     string `json:"model,omitempty"`
	LatencyMs int64  `json:"latency_ms"`
}

func chatWithActiveEngine(ctx context.Context, system, prompt, engineOverride string, maxTokens int) (*chatResult, error) {
	engine := engineOverride
	if engine == "" {
		engine = activeEngine()
	}
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	switch {
	case engine == "vpc":
		return chatVPC(ctx, prompt, maxTokens)
	case engine == "phone":
		return nil, fmt.Errorf("phone engine is not wired for orchestrator chat yet — use /api/web/edge/infer directly")
	case strings.HasPrefix(engine, "provider:"):
		p, ok := findProvider(strings.TrimPrefix(engine, "provider:"))
		if !ok {
			return nil, fmt.Errorf("provider not found: %s", engine)
		}
		if !p.Enabled {
			return nil, fmt.Errorf("provider %s is disabled", p.Name)
		}
		if p.APIKey == "" {
			return nil, fmt.Errorf("provider %s has no API key", p.Name)
		}
		return chatProvider(ctx, p, system, prompt, maxTokens)
	default:
		return nil, fmt.Errorf("unknown engine: %s", engine)
	}
}

// chatProvider calls an OpenAI-compatible /chat/completions endpoint.
func chatProvider(ctx context.Context, p LLMProvider, system, prompt string, maxTokens int) (*chatResult, error) {
	messages := []map[string]string{}
	if system != "" {
		messages = append(messages, map[string]string{"role": "system", "content": system})
	}
	messages = append(messages, map[string]string{"role": "user", "content": prompt})

	payload, _ := json.Marshal(map[string]interface{}{
		"model":       p.Model,
		"messages":    messages,
		"max_tokens":  maxTokens,
		"temperature": 1.0, // kimi-k3 accepts only 1; 1 is also the OpenAI default — the safe universal value.
	})

	base := strings.TrimRight(p.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	client := &http.Client{Timeout: 300 * time.Second} // reasoning models on big prompts can take minutes
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider %s unreachable: %w", p.Name, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	latency := time.Since(start).Milliseconds()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("provider %s HTTP %d: %s", p.Name, resp.StatusCode, truncateStr(string(raw), 300))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content           string `json:"content"`
				ReasoningContent  string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("provider %s: bad response: %w", p.Name, err)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("provider %s: empty choices", p.Name)
	}
	msg := out.Choices[0].Message
	content := strings.TrimSpace(msg.Content)
	if content == "" && strings.TrimSpace(msg.ReasoningContent) != "" {
		// Reasoning model (kimi-k3): all budget went to reasoning — tell the caller to raise max_tokens.
		return nil, fmt.Errorf("provider %s: empty content (reasoning model consumed the token budget — raise max_tokens)", p.Name)
	}
	return &chatResult{
		Content:   content,
		Engine:    "provider:" + p.ID,
		Model:     p.Model,
		LatencyMs: latency,
	}, nil
}

// chatVPC talks to the local Qwen sidecar (llama.cpp /completion).
// The container is woken on demand by Suparna analysis; if it's asleep we
// fail fast with a clear message instead of blocking for minutes.
func chatVPC(ctx context.Context, prompt string, maxTokens int) (*chatResult, error) {
	payload, _ := json.Marshal(map[string]interface{}{
		"prompt":      prompt,
		"n_predict":   maxTokens,
		"temperature": 0.4,
		"top_p":       0.9,
		"stream":      false,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, qwenURL()+"/completion", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vpc engine: qwen sidecar not running — run a Suparna reading first or pick another engine")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	latency := time.Since(start).Milliseconds()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("vpc engine: qwen HTTP %d (model still loading?)", resp.StatusCode)
	}
	var out struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("vpc engine: bad response: %w", err)
	}
	return &chatResult{
		Content:   strings.TrimSpace(out.Content),
		Engine:    "vpc",
		Model:     "qwen-gguf",
		LatencyMs: latency,
	}, nil
}

func qwenURL() string {
	if u := os.Getenv("QWEN_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://gafam-qwen:8080"
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ─── HTTP handlers (session-protected) ───

// llmProvidersHandler — GET (list, keys masked) / POST (upsert) / DELETE (?id=)
func llmProvidersHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list := loadLLMProviders()
		for i := range list {
			list[i] = maskedProvider(list[i])
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{"providers": list})

	case http.MethodPost:
		var in LLMProvider
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		in.BaseURL = strings.TrimSpace(in.BaseURL)
		in.Model = strings.TrimSpace(in.Model)
		if in.Name == "" || in.BaseURL == "" || in.Model == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "name, base_url and model are required"})
			return
		}
		list := loadLLMProviders()
		if in.ID == "" {
			in.ID = newProviderID()
			list = append(list, in)
		} else {
			found := false
			for i, p := range list {
				if p.ID == in.ID {
					// Keep the stored key if the form sent none (masked round-trip).
					if in.APIKey == "" {
						in.APIKey = p.APIKey
					}
					list[i] = in
					found = true
					break
				}
			}
			if !found {
				sendJSON(w, http.StatusNotFound, map[string]string{"error": "provider not found: " + in.ID})
				return
			}
		}
		if err := saveLLMProviders(list); err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("llm: provider upserted: %s (%s)", in.Name, in.ID)
		sendJSON(w, http.StatusOK, map[string]interface{}{"provider": maskedProvider(in)})

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
			return
		}
		list := loadLLMProviders()
		out := make([]LLMProvider, 0, len(list))
		removed := false
		for _, p := range list {
			if p.ID == id {
				removed = true
				continue
			}
			out = append(out, p)
		}
		if !removed {
			sendJSON(w, http.StatusNotFound, map[string]string{"error": "provider not found: " + id})
			return
		}
		if err := saveLLMProviders(out); err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// If the deleted provider was the active engine, fall back to vpc.
		if activeEngine() == "provider:"+id {
			_ = setSetting(settingLLMEngine, "vpc")
		}
		log.Printf("llm: provider deleted: %s", id)
		sendJSON(w, http.StatusOK, map[string]interface{}{"deleted": id})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// llmEngineHandler — GET (current + available) / POST ({engine})
func llmEngineHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		available := []map[string]string{
			{"engine": "vpc", "label": "VPC Qwen (L1)"},
			{"engine": "phone", "label": "Phone Edge (L2)"},
		}
		for _, p := range loadLLMProviders() {
			if p.Enabled {
				available = append(available, map[string]string{
					"engine": "provider:" + p.ID,
					"label":  p.Name + " — " + p.Model,
				})
			}
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"engine":    activeEngine(),
			"available": available,
		})

	case http.MethodPost:
		var in struct {
			Engine string `json:"engine"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if in.Engine != "vpc" && in.Engine != "phone" && !strings.HasPrefix(in.Engine, "provider:") {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid engine"})
			return
		}
		if strings.HasPrefix(in.Engine, "provider:") {
			p, ok := findProvider(strings.TrimPrefix(in.Engine, "provider:"))
			if !ok {
				sendJSON(w, http.StatusNotFound, map[string]string{"error": "provider not found"})
				return
			}
			if !p.Enabled {
				sendJSON(w, http.StatusBadRequest, map[string]string{"error": "provider is disabled"})
				return
			}
		}
		if err := setSetting(settingLLMEngine, in.Engine); err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("llm: orchestration engine set to %s", in.Engine)
		sendJSON(w, http.StatusOK, map[string]string{"engine": in.Engine})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// llmTestHandler — POST {id} — ping a provider with a tiny completion.
func llmTestHandler(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ID == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
		return
	}
	p, ok := findProvider(in.ID)
	if !ok {
		sendJSON(w, http.StatusNotFound, map[string]string{"error": "provider not found"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	res, err := chatProvider(ctx, p, "", "Reply with exactly: OK", 512)
	if err != nil {
		sendJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"ok":         true,
		"reply":      res.Content,
		"latency_ms": res.LatencyMs,
		"model":      res.Model,
	})
}

// llmChatHandler — POST {prompt, system?, engine?, max_tokens?}
// The single entry point the web console (and humans) use to talk to the
// active orchestration engine. Kāraka use the llm.chat tool instead.
func llmChatHandler(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt    string `json:"prompt"`
		System    string `json:"system"`
		Engine    string `json:"engine"`
		MaxTokens int    `json:"max_tokens"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Prompt) == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing prompt"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	res, err := chatWithActiveEngine(ctx, in.System, in.Prompt, in.Engine, in.MaxTokens)
	if err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, res)
}
