package main

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestHtmlToText(t *testing.T) {
	raw := `<html><head><title>Test Page</title><style>body{color:red}</style></head>
<body><h1>Hello Kāraka</h1><p>Some <b>text</b> here.</p>
<script>var x=1;</script>
<ul><li>Item one</li><li>Item two</li></ul>
<a href="https://example.com/a">Link A</a>
</body></html>`

	title, text, links := htmlToText(raw)
	if title != "Test Page" {
		t.Errorf("title = %q", title)
	}
	if strings.Contains(text, "var x") {
		t.Errorf("script leaked into text")
	}
	if !strings.Contains(text, "# Hello Kāraka") {
		t.Errorf("heading not markdown-ized: %q", text)
	}
	if len(links) != 1 || links[0]["href"] != "https://example.com/a" {
		t.Errorf("links = %v", links)
	}
}

func TestVaultRoundtrip(t *testing.T) {
	// In-memory relay DB with the FTS index.
	var err error
	db, err = sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	initVault()

	// Fake web page to fetch.
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<html><head><title>Vault Test</title></head><body><h1>Qwen edge inference</h1><p>ONNX on a 1GB VPS.</p></body></html>`)
	}))
	defer page.Close()

	// Fake sandbox file API (captures PUT writes).
	var writtenPath, writtenBody string
	sandbox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			b, _ := io.ReadAll(r.Body)
			writtenPath = r.URL.Path
			writtenBody = string(b)
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(404)
	}))
	defer sandbox.Close()
	os.Setenv("SANDBOX_URL", sandbox.URL)
	defer os.Unsetenv("SANDBOX_URL")

	note, err := vaultFetchNote(context.Background(), page.URL+"/article", "", []string{"llm", "edge"})
	if err != nil {
		t.Fatalf("vaultFetchNote: %v", err)
	}
	if note.Title != "Vault Test" {
		t.Errorf("note title = %q", note.Title)
	}
	if !strings.HasPrefix(writtenPath, "/files/research/notes/") || !strings.HasSuffix(writtenPath, ".md") {
		t.Errorf("written path = %q", writtenPath)
	}
	if !strings.Contains(writtenBody, "suggested_by") == true && !strings.Contains(writtenBody, "title:") {
		t.Errorf("markdown missing frontmatter")
	}
	if !strings.Contains(writtenBody, "Qwen edge inference") {
		t.Errorf("markdown missing body")
	}

	// FTS search must find it.
	results, err := vaultSearch("qwen edge", 5)
	if err != nil {
		t.Fatalf("vaultSearch: %v", err)
	}
	if len(results) != 1 || results[0]["id"] != note.ID {
		t.Fatalf("search results = %v", results)
	}
	if !strings.Contains(results[0]["snippet"].(string), "<b>") {
		t.Errorf("snippet not highlighted: %v", results[0]["snippet"])
	}

	// Cache read must return the body.
	cached, err := vaultNoteFromCache(note.ID)
	if err != nil {
		t.Fatalf("vaultNoteFromCache: %v", err)
	}
	if !strings.Contains(cached["text"].(string), "ONNX") {
		t.Errorf("cache body missing text")
	}

	// Recent list must include it.
	list, err := vaultList(10)
	if err != nil || len(list) != 1 {
		t.Fatalf("vaultList = %v, %v", list, err)
	}
}
