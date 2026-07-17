package main

// The Vault — persistent research memory for kāraka.
// Method absorbed from hyperresearch, rewritten our way:
// markdown notes are TRUTH (sandbox /files/research/notes/), the relay's
// SQLite FTS5 index is a rebuildable CACHE. Research compounds across
// missions — the 10th search reuses the notes of the previous 9.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Garletz/gafam/vpc-relay/karaka"
)

const (
	vaultNotesDir    = "/files/research/notes"
	vaultMissionsDir = "/files/research/missions"
	vaultMaxBytes    = 2 << 20 // 2 MB fetch cap
	vaultTextCap     = 60000
	vaultLinkCap     = 100
	vaultFetchUA     = "Mozilla/5.0 (compatible; GAFAM-Vault/1.0; +https://gafam.cloud)"
)

type VaultNote struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	FetchedAt   string   `json:"fetched_at"`
	SuggestedBy string   `json:"suggested_by,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Path        string   `json:"path"`
	Text        string   `json:"text,omitempty"`
	Truncated   bool     `json:"truncated,omitempty"`
}

// initVault creates the FTS5 index (cache — rebuildable from the markdown).
func initVault() {
	_, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS research_notes USING fts5(
		id UNINDEXED, title, url UNINDEXED, tags, body,
		fetched_at UNINDEXED, path UNINDEXED, suggested_by UNINDEXED
	)`)
	if err != nil {
		log.Printf("vault: failed to create FTS index: %v", err)
	}
}

func vaultSandboxURL() string {
	if u := os.Getenv("SANDBOX_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://gafam-sandbox:6091"
}

func newNoteID() string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return fmt.Sprintf("n%s-%s", time.Now().UTC().Format("20060102150405"), hex.EncodeToString(b))
}

// ─── Fetch & store ───

var (
	reTitle     = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reHeading   = regexp.MustCompile(`(?i)<h([1-6])[^>]*>`)
	reBlock     = regexp.MustCompile(`(?i)</?(p|div|br|ul|ol|tr|table|section|article|header|footer|blockquote|pre|form|figure|hr)[^>]*>`)
	reLi        = regexp.MustCompile(`(?i)<li[^>]*>`)
	reAnchor    = regexp.MustCompile(`(?is)<a\s[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	reAllTags   = regexp.MustCompile(`(?s)<[^>]+>`)
	reSpaces    = regexp.MustCompile(`[ \t]+`)
	reBlankLine = regexp.MustCompile(`\n{3,}`)
)

func stripSkippedBlocks(s string) string {
	for _, tag := range []string{"script", "style", "noscript", "svg", "template"} {
		re := regexp.MustCompile(`(?is)<` + tag + `[^>]*>.*?</` + tag + `>`)
		s = re.ReplaceAllString(s, " ")
	}
	return s
}

// htmlToText is the stdlib-only Khadyota extractor (Go twin of stream.py's).
func htmlToText(raw string) (title string, text string, links []map[string]string) {
	if m := reTitle.FindStringSubmatch(raw); m != nil {
		title = strings.TrimSpace(html.UnescapeString(reAllTags.ReplaceAllString(m[1], "")))
	}

	body := stripSkippedBlocks(raw)
	body = reHeading.ReplaceAllStringFunc(body, func(tag string) string {
		lvl := tag[2] - '0'
		return "\n" + strings.Repeat("#", int(lvl)) + " "
	})
	body = reLi.ReplaceAllString(body, "\n- ")
	body = reBlock.ReplaceAllString(body, "\n")

	links = make([]map[string]string, 0, 16)
	for _, m := range reAnchor.FindAllStringSubmatch(body, -1) {
		if len(links) >= vaultLinkCap {
			break
		}
		label := strings.TrimSpace(html.UnescapeString(reAllTags.ReplaceAllString(m[2], "")))
		href := strings.TrimSpace(m[1])
		if label != "" && href != "" && !strings.HasPrefix(href, "#") && !strings.HasPrefix(href, "javascript:") {
			links = append(links, map[string]string{"text": label, "href": href})
		}
	}

	text = reAllTags.ReplaceAllString(body, " ")
	text = html.UnescapeString(text)
	text = reSpaces.ReplaceAllString(text, " ")
	lines := make([]string, 0, 64)
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			lines = append(lines, ln)
		}
	}
	text = strings.Join(lines, "\n")
	text = reBlankLine.ReplaceAllString(text, "\n\n")
	return title, strings.TrimSpace(text), links
}

// vaultFetchNote fetches a URL, writes the markdown note (truth) into the
// sandbox vault, and indexes it in FTS5 (cache). No browser container needed.
func vaultFetchNote(ctx context.Context, rawURL, suggestedBy string, tags []string) (*VaultNote, error) {
	if !strings.HasPrefix(strings.ToLower(rawURL), "http://") && !strings.HasPrefix(strings.ToLower(rawURL), "https://") {
		return nil, fmt.Errorf("url must start with http:// or https://")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", vaultFetchUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")

	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, vaultMaxBytes))

	contentType := resp.Header.Get("Content-Type")
	var title, text string
	if strings.Contains(contentType, "html") || contentType == "" {
		title, text, _ = htmlToText(string(raw))
	} else {
		text = string(raw)
	}
	truncated := false
	if len(text) > vaultTextCap {
		text = text[:vaultTextCap]
		truncated = true
	}
	if title == "" {
		title = rawURL
	}

	note := &VaultNote{
		ID:          newNoteID(),
		Title:       title,
		URL:         resp.Request.URL.String(),
		FetchedAt:   time.Now().UTC().Format(time.RFC3339),
		SuggestedBy: suggestedBy,
		Tags:        tags,
		Text:        text,
		Truncated:   truncated,
	}
	note.Path = vaultNotesDir + "/" + note.ID + ".md"

	// ── Write the markdown (truth) via the sandbox file API ──
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %s\n", note.ID)
	fmt.Fprintf(&b, "title: %q\n", note.Title)
	fmt.Fprintf(&b, "url: %q\n", note.URL)
	fmt.Fprintf(&b, "fetched_at: %s\n", note.FetchedAt)
	if suggestedBy != "" {
		fmt.Fprintf(&b, "suggested_by: %s\n", suggestedBy)
	}
	if len(tags) > 0 {
		fmt.Fprintf(&b, "tags: [%s]\n", strings.Join(tags, ", "))
	}
	b.WriteString("---\n\n# ")
	b.WriteString(note.Title)
	b.WriteString("\n\n")
	b.WriteString(note.Text)
	b.WriteString("\n")

	md := b.String()
	putURL := vaultSandboxURL() + note.Path
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, strings.NewReader(md))
	if err != nil {
		return nil, err
	}
	putReq.Header.Set("Content-Type", "text/markdown")
	putResp, err := client.Do(putReq)
	if err != nil {
		return nil, fmt.Errorf("sandbox_not_running — wake the sandbox first (vault writes need it)")
	}
	defer putResp.Body.Close()
	if putResp.StatusCode >= 400 {
		return nil, fmt.Errorf("vault write failed: sandbox HTTP %d", putResp.StatusCode)
	}

	// ── Index in FTS5 (cache) ──
	_, err = db.Exec(
		`INSERT INTO research_notes (id, title, url, tags, body, fetched_at, path, suggested_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		note.ID, note.Title, note.URL, strings.Join(tags, " "), text, note.FetchedAt, note.Path, suggestedBy,
	)
	if err != nil {
		log.Printf("vault: FTS index failed for %s: %v", note.ID, err)
	}

	log.Printf("vault: note %s stored (%d chars, from %s)", note.ID, len(text), note.URL)
	return note, nil
}

// ─── Search & read ───

func vaultSearch(query string, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	// Sanitize: words become quoted terms, AND-ed by FTS5.
	words := strings.Fields(query)
	if len(words) == 0 {
		return nil, fmt.Errorf("empty query")
	}
	terms := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.ReplaceAll(w, `"`, "")
		if w != "" {
			terms = append(terms, `"`+w+`"`)
		}
	}
	match := strings.Join(terms, " ")

	rows, err := db.Query(
		`SELECT id, title, url, tags, fetched_at, path, snippet(research_notes, 4, '<b>', '</b>', '…', 14) AS snip
		 FROM research_notes WHERE research_notes MATCH ? ORDER BY rank LIMIT ?`,
		match, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []map[string]interface{}{}
	for rows.Next() {
		var id, title, url, tags, fetchedAt, path, snip string
		if err := rows.Scan(&id, &title, &url, &tags, &fetchedAt, &path, &snip); err == nil {
			out = append(out, map[string]interface{}{
				"id": id, "title": title, "url": url, "tags": tags,
				"fetched_at": fetchedAt, "path": path, "snippet": snip,
			})
		}
	}
	return out, nil
}

func vaultList(limit int) ([]map[string]interface{}, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := db.Query(
		`SELECT id, title, url, tags, fetched_at, path FROM research_notes ORDER BY fetched_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []map[string]interface{}{}
	for rows.Next() {
		var id, title, url, tags, fetchedAt, path string
		if err := rows.Scan(&id, &title, &url, &tags, &fetchedAt, &path); err == nil {
			out = append(out, map[string]interface{}{
				"id": id, "title": title, "url": url, "tags": tags,
				"fetched_at": fetchedAt, "path": path,
			})
		}
	}
	return out, nil
}

// vaultNoteFromCache reads the indexed body (always available, even sandbox asleep).
func vaultNoteFromCache(id string) (map[string]interface{}, error) {
	row := db.QueryRow(
		`SELECT id, title, url, tags, body, fetched_at, path, suggested_by FROM research_notes WHERE id = ?`, id,
	)
	var nid, title, url, tags, body, fetchedAt, path, suggestedBy string
	if err := row.Scan(&nid, &title, &url, &tags, &body, &fetchedAt, &path, &suggestedBy); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("note not found: %s", id)
		}
		return nil, err
	}
	return map[string]interface{}{
		"id": nid, "title": title, "url": url, "tags": tags, "text": body,
		"fetched_at": fetchedAt, "path": path, "suggested_by": suggestedBy, "source": "cache",
	}, nil
}

// vaultNoteFromFile reads the markdown truth from the sandbox (kāraka way).
func vaultNoteFromFile(ctx context.Context, id string) (map[string]interface{}, error) {
	if !regexp.MustCompile(`^n[0-9]{14}-[0-9a-f]{4}$`).MatchString(id) {
		return nil, fmt.Errorf("invalid note id")
	}
	path := vaultNotesDir + "/" + id + ".md"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, vaultSandboxURL()+path, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sandbox_not_running — note file unreadable right now")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("note file not found in vault (HTTP %d)", resp.StatusCode)
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return map[string]interface{}{
		"id": id, "path": path, "markdown": string(raw), "source": "file",
	}, nil
}

// vaultSearchBaseURL is the web search endpoint (var for tests).
var vaultSearchBaseURL = "https://html.duckduckgo.com/html/?q="

// vaultWebSearch runs a web search (DuckDuckGo HTML) and returns result
// links — the sweep's eyes. No API key, no browser container needed.
func vaultWebSearch(ctx context.Context, query string, maxResults int) ([]map[string]string, error) {
	if maxResults <= 0 || maxResults > 10 {
		maxResults = 5
	}
	searchURL := vaultSearchBaseURL + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", vaultFetchUA)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("web search failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, vaultMaxBytes))

	out := make([]map[string]string, 0, maxResults)
	seen := map[string]bool{}
	for _, m := range reAnchor.FindAllStringSubmatch(string(raw), -1) {
		href := html.UnescapeString(strings.TrimSpace(m[1]))
		label := strings.TrimSpace(html.UnescapeString(reAllTags.ReplaceAllString(m[2], "")))

		// DDG wraps results in /l/?uddg=<urlencoded target> — unwrap.
		if strings.Contains(href, "uddg=") {
			if u, err := url.Parse(href); err == nil {
				if target := u.Query().Get("uddg"); target != "" {
					href = target
				}
			}
		}
		if !strings.HasPrefix(href, "http") || strings.Contains(href, "duckduckgo.com") {
			continue
		}
		if label == "" || seen[href] {
			continue
		}
		seen[href] = true
		out = append(out, map[string]string{"title": label, "url": href})
		if len(out) >= maxResults {
			break
		}
	}
	return out, nil
}

// vaultURLKnown reports whether a URL is already in the vault (avoid refetch).
func vaultURLKnown(rawURL string) bool {
	var id string
	row := db.QueryRow(`SELECT id FROM research_notes WHERE url = ? LIMIT 1`, rawURL)
	return row.Scan(&id) == nil
}

// ─── Kāraka tool handlers ───

func vaultFetchTool(params map[string]interface{}) (interface{}, error) {
	u, _ := params["url"].(string)
	if u == "" {
		return nil, fmt.Errorf("missing 'url'")
	}
	suggestedBy, _ := params["suggested_by"].(string)
	var tags []string
	if t, _ := params["tags"].(string); t != "" {
		for _, tag := range strings.Split(t, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				tags = append(tags, tag)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	note, err := vaultFetchNote(ctx, u, suggestedBy, tags)
	if err != nil {
		return nil, err
	}
	// Don't flood the kāraka context: cap the returned text.
	preview := note.Text
	if len(preview) > 8000 {
		preview = preview[:8000] + "…"
	}
	return map[string]interface{}{
		"id": note.ID, "title": note.Title, "url": note.URL, "path": note.Path,
		"fetched_at": note.FetchedAt, "truncated": note.Truncated, "text": preview,
	}, nil
}

func vaultSearchTool(params map[string]interface{}) (interface{}, error) {
	q, _ := params["query"].(string)
	if q == "" {
		return nil, fmt.Errorf("missing 'query'")
	}
	limit := 10
	if l, ok := params["limit"].(float64); ok {
		limit = int(l)
	}
	results, err := vaultSearch(q, limit)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"query": q, "count": len(results), "results": results}, nil
}

func vaultNoteShowTool(params map[string]interface{}) (interface{}, error) {
	id, _ := params["id"].(string)
	if id == "" {
		return nil, fmt.Errorf("missing 'id'")
	}
	source, _ := params["source"].(string)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if source == "cache" {
		return vaultNoteFromCache(id)
	}
	note, err := vaultNoteFromFile(ctx, id)
	if err != nil {
		// Graceful fallback to the cache when the sandbox is asleep.
		return vaultNoteFromCache(id)
	}
	return note, nil
}

func vaultListTool(params map[string]interface{}) (interface{}, error) {
	limit := 25
	if l, ok := params["limit"].(float64); ok {
		limit = int(l)
	}
	notes, err := vaultList(limit)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"count": len(notes), "notes": notes}, nil
}

// registerVaultTools wires the vault into Kāraka (called from main.go).
func registerVaultTools() {
	karaka.RegisterTool(karaka.Tool{
		ID:          "research.fetch",
		Description: "Fetch a URL into the vault: stores a markdown note (truth) and indexes it (FTS5 cache). Returns the note id + extracted text. Use suggested_by to link provenance.",
		Category:    "research",
		Params: map[string]karaka.ParamSpec{
			"url":          {Type: "string", Required: true, Description: "Page URL to fetch and store"},
			"suggested_by": {Type: "string", Required: false, Description: "Note id that led to this fetch (provenance)"},
			"tags":         {Type: "string", Required: false, Description: "Comma-separated tags"},
		},
		Returns: "{ id, title, url, path, text, truncated }",
		Handler: vaultFetchTool,
	})
	karaka.RegisterTool(karaka.Tool{
		ID:          "research.search",
		Description: "Full-text search across all vault notes (compounds across missions). Check the vault BEFORE fetching — the answer may already be there.",
		Category:    "research",
		Params: map[string]karaka.ParamSpec{
			"query": {Type: "string", Required: true, Description: "Search words (AND-ed)"},
			"limit": {Type: "int", Required: false, Description: "Max results", Default: 10},
		},
		Returns: "{ count, results: [{id, title, url, snippet, fetched_at}] }",
		Handler: vaultSearchTool,
	})
	karaka.RegisterTool(karaka.Tool{
		ID:          "research.note_show",
		Description: "Read a full vault note by id (markdown truth from file, cache fallback).",
		Category:    "research",
		Params: map[string]karaka.ParamSpec{
			"id":     {Type: "string", Required: true, Description: "Note id (n20260717…-…)"},
			"source": {Type: "string", Required: false, Description: "file (default) or cache"},
		},
		Returns: "{ id, markdown } or { id, text }",
		Handler: vaultNoteShowTool,
	})
	karaka.RegisterTool(karaka.Tool{
		ID:          "research.list",
		Description: "List the most recent vault notes.",
		Category:    "research",
		Params: map[string]karaka.ParamSpec{
			"limit": {Type: "int", Required: false, Description: "Max notes", Default: 25},
		},
		Returns: "{ count, notes: [{id, title, url, fetched_at}] }",
		Handler: vaultListTool,
	})
}

// ─── Web handlers (session-protected) ───

func researchSearchHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	results, err := vaultSearch(q, limit)
	if err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, map[string]interface{}{"count": len(results), "results": results})
}

func researchNotesHandler(w http.ResponseWriter, r *http.Request) {
	limit := 25
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	notes, err := vaultList(limit)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, map[string]interface{}{"count": len(notes), "notes": notes})
}

func researchNoteHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
		return
	}
	note, err := vaultNoteFromCache(id)
	if err != nil {
		sendJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, note)
}
