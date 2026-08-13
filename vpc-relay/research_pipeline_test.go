package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/Garletz/gafam/vpc-relay/karaka"
	_ "modernc.org/sqlite"
)

// Stub LLM provider (OpenAI-compatible) that returns canned content.
func stubLLMServer(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := json.Marshal(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": reply}},
			},
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
}

func setupPipelineEnv(t *testing.T, llmReply string) (cleanup func()) {
	t.Helper()
	// SSRF guard off — pipeline tests fetch local httptest servers.
	karaka.AllowPrivateURLs = true
	t.Cleanup(func() { karaka.AllowPrivateURLs = false })
	var err error
	db, err = sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// settings table (engine + providers)
	_, err = db.Exec(`CREATE TABLE gafam_settings (key TEXT PRIMARY KEY, value TEXT)`)
	if err != nil {
		t.Fatal(err)
	}
	initVault()

	llm := stubLLMServer(t, llmReply)
	providers := []LLMProvider{{
		ID: "p_test", Name: "stub", BaseURL: llm.URL, APIKey: "sk-test", Model: "stub-model", Enabled: true,
	}}
	raw, _ := json.Marshal(providers)
	if err := setSetting(settingLLMProviders, string(raw)); err != nil {
		t.Fatal(err)
	}
	if err := setSetting(settingLLMEngine, "provider:p_test"); err != nil {
		t.Fatal(err)
	}
	return func() {
		db.Close()
		llm.Close()
	}
}

func TestResearchDecompose(t *testing.T) {
	reply := `{"questions": ["What is Qwen3?", "Can it run on 1GB?"], "sweep": [{"angle": "docs", "queries": ["qwen3 0.6b requirements"]}, {"angle": "limits", "queries": ["qwen3 0.6b limitations"]}]}`
	cleanup := setupPipelineEnv(t, reply)
	defer cleanup()

	plan, err := researchDecompose(context.Background(), "Research Qwen3 on small VPS")
	if err != nil {
		t.Fatalf("researchDecompose: %v", err)
	}
	if len(plan.Questions) != 2 || len(plan.Sweep) != 2 {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.Sweep[0].Queries[0] != "qwen3 0.6b requirements" {
		t.Errorf("unexpected query: %v", plan.Sweep[0].Queries)
	}
}

func TestVaultWebSearch(t *testing.T) {
	// Stub DuckDuckGo HTML endpoint.
	ddg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target1 := url.QueryEscape("https://example.com/article-1")
		target2 := url.QueryEscape("https://example.org/paper-2")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body>
<a class="result__a" href="//duckduckgo.com/l/?uddg=%s">Article One</a>
<a class="result__a" href="//duckduckgo.com/l/?uddg=%s">Paper Two</a>
<a class="result__a" href="https://duckduckgo.com/settings">Settings</a>
</body></html>`, target1, target2)
	}))
	defer ddg.Close()

	old := vaultSearchBaseURL
	vaultSearchBaseURL = ddg.URL + "/?q="
	defer func() { vaultSearchBaseURL = old }()

	results, err := vaultWebSearch(context.Background(), "test query", 5)
	if err != nil {
		t.Fatalf("vaultWebSearch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %v", results)
	}
	if results[0]["url"] != "https://example.com/article-1" {
		t.Errorf("uddg unwrap failed: %v", results[0])
	}
	if results[1]["url"] != "https://example.org/paper-2" {
		t.Errorf("second result: %v", results[1])
	}
}

func TestResearchSweepVaultFirst(t *testing.T) {
	cleanup := setupPipelineEnv(t, "{}")
	defer cleanup()

	// Pre-seed the vault with a note (the "memory").
	var writtenPath string
	sandbox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			b, _ := io.ReadAll(r.Body)
			writtenPath = r.URL.Path
			_ = b
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(404)
	}))
	defer sandbox.Close()
	os.Setenv("SANDBOX_URL", sandbox.URL)
	defer os.Unsetenv("SANDBOX_URL")

	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<html><head><title>Existing Qwen Note</title></head><body><p>qwen edge inference on tiny VPS</p></body></html>`)
	}))
	defer page.Close()

	if _, err := vaultFetchNote(context.Background(), page.URL+"/existing", "", []string{"qwen"}); err != nil {
		t.Fatalf("seed fetch: %v", err)
	}

	// DDG returns nothing new — sweep must still succeed from the vault alone.
	ddg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<html><body><a class="result__a" href="https://duckduckgo.com/x">none</a></body></html>`)
	}))
	defer ddg.Close()
	old := vaultSearchBaseURL
	vaultSearchBaseURL = ddg.URL + "/?q="
	defer func() { vaultSearchBaseURL = old }()

	plan := &decomposeResult{
		Questions: []string{"q?"},
		Sweep: []struct {
			Angle   string   `json:"angle"`
			Queries []string `json:"queries"`
		}{{Angle: "a", Queries: []string{"qwen edge"}}},
	}

	sources, err := researchSweep(context.Background(), plan)
	if err != nil {
		t.Fatalf("researchSweep: %v", err)
	}
	if len(sources) != 1 || !sources[0].FromVault {
		t.Fatalf("expected 1 vault source, got %+v", sources)
	}
	if writtenPath == "" {
		t.Errorf("sandbox never received the seed note")
	}
}
