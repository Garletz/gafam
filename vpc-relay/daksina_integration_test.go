package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestDaksinaRunEndToEnd exercises the real relay path without Docker:
// startDaksinaRun → ensureReady (external OPENCODE_URL) → provider/context →
// POST /task → status polling → SMS result in gafam_outbox.
func TestDaksinaRunEndToEnd(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("GAFAM_DATA_DIR", dataDir)
	t.Setenv("DAKSINA_POLL_INTERVAL_MS", "10")

	var taskBody atomic.Value
	var polls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/health":
			io.WriteString(w, `{"ok":true,"version":"test"}`)
		case r.URL.Path == "/task" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			taskBody.Store(body)
			w.WriteHeader(http.StatusAccepted)
			io.WriteString(w, `{"accepted":true}`)
		case strings.HasPrefix(r.URL.Path, "/task/"):
			n := atomic.AddInt32(&polls, 1)
			if n < 2 {
				io.WriteString(w, `{"id":"x","status":"running","phase":"agent"}`)
				return
			}
			io.WriteString(w, `{"id":"x","status":"done","phase":"done","summary":"mission OK","merged":true,"pr_url":"https://github.com/Garletz/gafam/pull/42","test_summary":"go test ./...=ok"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("OPENCODE_URL", srv.URL)

	oldDB := db
	var err error
	db, err = sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer func() {
		db.Close()
		db = oldDB
	}()

	for _, stmt := range []string{
		`CREATE TABLE gafam_settings (key TEXT PRIMARY KEY, value TEXT)`,
		`CREATE TABLE daksina_runs (
			id TEXT PRIMARY KEY, instruction TEXT, status TEXT DEFAULT 'queued',
			phase TEXT DEFAULT 'queued', repo TEXT, branch TEXT, pr_url TEXT,
			summary TEXT, error TEXT, log TEXT, auto_merge INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE gafam_outbox (
			id INTEGER PRIMARY KEY AUTOINCREMENT, recipient TEXT, body TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE gafam_sms (
			id INTEGER PRIMARY KEY AUTOINCREMENT, sender TEXT, body TEXT,
			timestamp INTEGER, status TEXT DEFAULT 'inbox',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO gafam_settings (key, value) VALUES (?, ?)`,
		settingDaksinaToken, "test-token"); err != nil {
		t.Fatal(err)
	}
	providers, _ := json.Marshal([]LLMProvider{{
		ID: "p1", Name: "Fake", BaseURL: "https://llm.test/v1",
		APIKey: "test-key", Model: "fake-model", Enabled: true,
	}})
	if _, err := db.Exec(`INSERT INTO gafam_settings (key, value) VALUES (?, ?)`,
		settingLLMProviders, string(providers)); err != nil {
		t.Fatal(err)
	}

	run, err := startDaksinaRun("corrige le bug des logs", true, "+33612345678")
	if err != nil {
		t.Fatalf("startDaksinaRun: %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	var final *daksinaRun
	for time.Now().Before(deadline) {
		got, err := getDaksinaRun(run.ID)
		if err == nil && daksinaTerminal(got.Status) {
			final = got
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if final == nil {
		t.Fatalf("run %s did not reach a terminal state", run.ID)
	}
	if final.Status != "done" {
		t.Fatalf("status = %q (error=%q summary=%q)", final.Status, final.Error, final.Summary)
	}
	if final.PRURL != "https://github.com/Garletz/gafam/pull/42" {
		t.Fatalf("pr_url = %q", final.PRURL)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "daksina", "context", run.ID, "gafam-history.sqlite")); err != nil {
		t.Fatalf("history snapshot missing: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(dataDir, "daksina", "context", run.ID, "history.md")); err != nil || !bytes.Contains(raw, []byte("Historique GAFAM")) {
		t.Fatalf("history.md missing/invalid: %v %q", err, raw)
	}

	// The sidecar must have received the selected provider + repo + token.
	raw, _ := taskBody.Load().([]byte)
	if len(raw) == 0 {
		t.Fatal("sidecar never received a /task request")
	}
	var req struct {
		Repo  string            `json:"repo"`
		Token string            `json:"github_token"`
		Model map[string]string `json:"model"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("task body: %v", err)
	}
	if req.Repo != "Garletz/gafam" || req.Token != "test-token" {
		t.Fatalf("unexpected task routing: repo=%q token=%q", req.Repo, req.Token)
	}
	if req.Model["base_url"] != "https://llm.test/v1" ||
		req.Model["api_key"] != "test-key" ||
		req.Model["model"] != "fake-model" {
		t.Fatalf("unexpected model routing: %#v", req.Model)
	}

	// The final SMS must be queued in the outbox (SMS is sent after the
	// terminal status update, so poll briefly).
	var body string
	outboxDeadline := time.Now().Add(2 * time.Second)
	for {
		err := db.QueryRow(`SELECT body FROM gafam_outbox ORDER BY id DESC LIMIT 1`).Scan(&body)
		if err == nil {
			break
		}
		if time.Now().After(outboxDeadline) {
			t.Fatalf("outbox: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(body, "✅") || !strings.Contains(body, "pull/42") {
		t.Fatalf("unexpected final SMS: %q", body)
	}

	// Wait until the run goroutine has released the global mutex before the
	// test restores the shared DB.
	unlockDeadline := time.Now().Add(2 * time.Second)
	for {
		if daksinaMu.TryLock() {
			daksinaMu.Unlock()
			break
		}
		if time.Now().After(unlockDeadline) {
			t.Fatal("Dakṣiṇā run did not release its mutex")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
