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
	settingLLMScopes    = "llm_scopes"
)

// ScopeRouting maps scopes to primary engine + fallback chain.
type ScopeRouting struct {
	Primary   string   `json:"primary"`
	Fallbacks []string `json:"fallbacks"`
}

func getSetting(key string) string {
	if db == nil {
		return ""
	}
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
	if decrypted, err := unsealSettingsValue(raw); err == nil {
		raw = string(decrypted)
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
	sealed, err := sealSettingsValue(raw)
	if err != nil {
		return fmt.Errorf("llm: seal providers failed: %w", err)
	}
	return setSetting(settingLLMProviders, sealed)
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

// getScopeRouting returns the engine routing table for a scope.
// Falls back to sensible defaults if nothing configured.
func getScopeRouting(scope string) ScopeRouting {
	raw := getSetting(settingLLMScopes)
	if raw != "" {
		var table map[string]ScopeRouting
		if json.Unmarshal([]byte(raw), &table) == nil {
			if r, ok := table[scope]; ok {
				return r
			}
		}
	}
	// Smart defaults: pick first available enabled provider for platform scopes
	switch scope {
	case "orchestrator":
		for _, p := range loadLLMProviders() {
			if p.Enabled && p.APIKey != "" {
				return ScopeRouting{Primary: "provider:" + p.ID, Fallbacks: []string{"vpc"}}
			}
		}
		return ScopeRouting{Primary: "vpc"}
	case "light_task":
		for _, p := range loadLLMProviders() {
			if p.Enabled && p.APIKey != "" {
				return ScopeRouting{Primary: "provider:" + p.ID, Fallbacks: []string{"vpc"}}
			}
		}
		return ScopeRouting{Primary: "vpc"}
	case "read_only":
		return ScopeRouting{Primary: "vpc"}
	default:
		return ScopeRouting{Primary: activeEngine()}
	}
}

func setScopeRouting(table map[string]ScopeRouting) error {
	raw, err := json.Marshal(table)
	if err != nil {
		return err
	}
	return setSetting(settingLLMScopes, string(raw))
}

// isTransientError returns true for errors that are worth retrying on a fallback.
func isTransientError(err error) bool {
	s := err.Error()
	return strings.Contains(s, "unreachable") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "429") ||
		strings.Contains(s, "503") ||
		strings.Contains(s, "502") ||
		strings.Contains(s, "EOF") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "context deadline exceeded")
}

// ─── Engine router with scope support ───

type chatResult struct {
	Content   string `json:"content"`
	Engine    string `json:"engine"`
	Model     string `json:"model,omitempty"`
	LatencyMs int64  `json:"latency_ms"`
}

// chatWithEngine routes by scope, with failover across the fallback chain.
func chatWithEngine(ctx context.Context, scope, system, prompt string, maxTokens int) (*chatResult, error) {
	routing := getScopeRouting(scope)
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	candidates := append([]string{routing.Primary}, routing.Fallbacks...)
	var lastErr error
	for _, engine := range candidates {
		if engine == "" {
			continue
		}
		res, err := callOneEngine(ctx, engine, system, prompt, maxTokens)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if !isTransientError(err) {
			return nil, err // non-retriable
		}
		log.Printf("llm: scope=%s engine=%s failed (transient), trying next: %v", scope, engine, err)
	}
	if lastErr != nil {
		return nil, fmt.Errorf("scope=%s: all engines exhausted: %w", scope, lastErr)
	}
	return nil, fmt.Errorf("scope=%s: no engines configured", scope)
}

func callOneEngine(ctx context.Context, engine, system, prompt string, maxTokens int) (*chatResult, error) {
	switch {
	case engine == "vpc":
		return chatVPC(ctx, system, prompt, maxTokens)
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

// chatWithActiveEngine — backward-compat wrapper: if engineOverride is set, bypass scope routing.
func chatWithActiveEngine(ctx context.Context, system, prompt, engineOverride string, maxTokens int) (*chatResult, error) {
	if engineOverride != "" {
		return callOneEngine(ctx, engineOverride, system, prompt, maxTokens)
	}
	// Default scope for the global chat handler: orchestrator
	return chatWithEngine(ctx, "orchestrator", system, prompt, maxTokens)
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

// chatVPC talks to the local Qwen sidecar (llama.cpp). It prefers the
// OpenAI-compatible /v1/chat/completions endpoint so the system prompt
// survives (the planner's instructions live there); falls back to the raw
// /completion endpoint with system+prompt concatenated for older servers.
// The container is woken on demand by Suparna analysis; if it's asleep we
// fail fast with a clear message instead of blocking for minutes.
func chatVPC(ctx context.Context, system, prompt string, maxTokens int) (*chatResult, error) {
	start := time.Now()

	// Attempt 1: chat completions (system prompt preserved).
	messages := []map[string]string{}
	if system != "" {
		messages = append(messages, map[string]string{"role": "system", "content": system})
	}
	messages = append(messages, map[string]string{"role": "user", "content": prompt})
	chatPayload, _ := json.Marshal(map[string]interface{}{
		"messages":    messages,
		"max_tokens":  maxTokens,
		"temperature": 0.4,
		"top_p":       0.9,
		"stream":      false,
	})
	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, qwenURL()+"/v1/chat/completions", bytes.NewReader(chatPayload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vpc engine: qwen sidecar not running — run a Suparna reading first or pick another engine")
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode < 400 {
		var out struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(raw, &out); err == nil && len(out.Choices) > 0 {
			return &chatResult{
				Content:   strings.TrimSpace(out.Choices[0].Message.Content),
				Engine:    "vpc",
				Model:     "qwen-gguf",
				LatencyMs: time.Since(start).Milliseconds(),
			}, nil
		}
	}

	// Attempt 2 (fallback): raw /completion with system prepended.
	full := prompt
	if system != "" {
		full = system + "\n\n" + prompt
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"prompt":      full,
		"n_predict":   maxTokens,
		"temperature": 0.4,
		"top_p":       0.9,
		"stream":      false,
	})
	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, qwenURL()+"/completion", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req2)
	if err != nil {
		return nil, fmt.Errorf("vpc engine: qwen sidecar not running — run a Suparna reading first or pick another engine")
	}
	defer resp2.Body.Close()
	raw2, _ := io.ReadAll(io.LimitReader(resp2.Body, 1<<20))
	if resp2.StatusCode >= 400 {
		return nil, fmt.Errorf("vpc engine: qwen HTTP %d (model still loading?)", resp2.StatusCode)
	}
	var out2 struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw2, &out2); err != nil {
		return nil, fmt.Errorf("vpc engine: bad response: %w", err)
	}
	return &chatResult{
		Content:   strings.TrimSpace(out2.Content),
		Engine:    "vpc",
		Model:     "qwen-gguf",
		LatencyMs: time.Since(start).Milliseconds(),
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

// llmChatHandler — POST {prompt, system?, engine?, scope?, max_tokens?}
func llmChatHandler(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt    string `json:"prompt"`
		System    string `json:"system"`
		Engine    string `json:"engine"`
		Scope     string `json:"scope"`
		MaxTokens int    `json:"max_tokens"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Prompt) == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing prompt"})
		return
	}
	if in.Scope == "" {
		in.Scope = "orchestrator"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	var res *chatResult
	var err error
	if in.Engine != "" {
		res, err = chatWithActiveEngine(ctx, in.System, in.Prompt, in.Engine, in.MaxTokens)
	} else {
		res, err = chatWithEngine(ctx, in.Scope, in.System, in.Prompt, in.MaxTokens)
	}
	if err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, res)
}

// llmScopesHandler — GET (list) / POST (upsert)
func llmScopesHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		scopes := map[string]ScopeRouting{}
		raw := getSetting(settingLLMScopes)
		if raw != "" {
			json.Unmarshal([]byte(raw), &scopes)
		}
		// Fill in active defaults for reference
		for _, s := range []string{"orchestrator", "light_task", "read_only"} {
			if _, ok := scopes[s]; !ok {
				scopes[s] = getScopeRouting(s)
			}
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{"scopes": scopes})

	case http.MethodPost:
		var in struct {
			Scope     string   `json:"scope"`
			Primary   string   `json:"primary"`
			Fallbacks []string `json:"fallbacks"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Scope == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "scope required"})
			return
		}
		if in.Fallbacks == nil {
			in.Fallbacks = []string{}
		}
		scopes := map[string]ScopeRouting{}
		raw := getSetting(settingLLMScopes)
		if raw != "" {
			json.Unmarshal([]byte(raw), &scopes)
		}
		scopes[in.Scope] = ScopeRouting{Primary: in.Primary, Fallbacks: in.Fallbacks}
		if err := setScopeRouting(scopes); err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("llm: scope %s → primary=%s fallbacks=%v", in.Scope, in.Primary, in.Fallbacks)
		sendJSON(w, http.StatusOK, map[string]string{"scope": in.Scope, "primary": in.Primary})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
