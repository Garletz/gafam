package main

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Garletz/gafam/vpc-relay/karaka"
)

func TestDaksinaDefaults(t *testing.T) {
	oldDB := db
	db = nil
	defer func() { db = oldDB }()
	t.Setenv("GITHUB_TOKEN", "")

	if got := daksinaRepo(); got != "Garletz/gafam" {
		t.Fatalf("default repo = %q", got)
	}
	if !daksinaEnabled() {
		t.Fatal("daksina should be enabled by default")
	}
	if !daksinaAutoMerge() {
		t.Fatal("auto-merge should be on by default")
	}
	if got := daksinaGithubToken(); got != "" {
		t.Fatalf("expected empty token, got %q", got)
	}
}

func TestDaksinaRepoValidation(t *testing.T) {
	valid := []string{"Garletz/gafam", "foo-bar/baz_qux", "a.b/c.d"}
	for _, repo := range valid {
		if !daksinaRepoRE.MatchString(repo) {
			t.Fatalf("expected valid repo: %q", repo)
		}
	}
	invalid := []string{"", "gafam", "a/b/c", "../x", "a b/c"}
	for _, repo := range invalid {
		if daksinaRepoRE.MatchString(repo) {
			t.Fatalf("expected invalid repo: %q", repo)
		}
	}
}

func TestDaksinaTitle(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"corrige le bug des logs\nplus de détail", "corrige le bug des logs"},
		{"   ", "self-improvement"},
	}
	for _, c := range cases {
		if got := daksinaTitle(c.in); got != c.want {
			t.Fatalf("daksinaTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	long := "a very long line that should be truncated because it is definitely longer than eighty characters"
	got := daksinaTitle(long)
	if len(got) != 80 {
		t.Fatalf("long title len = %d, want 80 (%q)", len(got), got)
	}
	if got != long[:80] {
		t.Fatalf("long title = %q, want %q", got, long[:80])
	}
}

func TestDaksinaTokenHint(t *testing.T) {
	if got := daksinaTokenHint(""); got != "" {
		t.Fatalf("empty hint = %q", got)
	}
	if got := daksinaTokenHint("ghp_1234567890abcd"); got != "…abcd" {
		t.Fatalf("hint = %q", got)
	}
	if got := daksinaTokenHint("abc"); got != "…" {
		t.Fatalf("short hint = %q", got)
	}
}

func TestDemuxDockerStream(t *testing.T) {
	var buf bytes.Buffer
	writeFrame := func(stream byte, payload string) {
		var hdr [8]byte
		hdr[0] = stream
		binary.BigEndian.PutUint32(hdr[4:], uint32(len(payload)))
		buf.Write(hdr[:])
		buf.WriteString(payload)
	}
	writeFrame(1, "hello ")
	writeFrame(2, "world")
	if got := demuxDockerStream(buf.Bytes()); got != "hello world" {
		t.Fatalf("demux = %q", got)
	}
	if got := demuxDockerStream([]byte("plain text")); got != "plain text" {
		t.Fatalf("plain fallback = %q", got)
	}
}

func TestDaksinaRunIDValidation(t *testing.T) {
	for _, id := range []string{"dk_abc123", "dk_m3x_9f2a", "A1_b2-c3"} {
		if !daksinaRunIDRE.MatchString(id) {
			t.Fatalf("expected valid run id: %q", id)
		}
	}
	for _, id := range []string{"", "dk/../../etc", "dk abc", "x"} {
		if daksinaRunIDRE.MatchString(id) {
			t.Fatalf("expected invalid run id: %q", id)
		}
	}
}

func TestFallbackPlanRoutesSelfImprovementToDaksina(t *testing.T) {
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
	if _, err := db.Exec(`CREATE TABLE gafam_settings (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}

	// Without a token, never pretend to self-improve: answer directly.
	plan := fallbackPlan("peux tu t'auto améliorer ?")
	if len(plan.Quests) != 1 || plan.Quests[0].Tool != "llm.chat" {
		t.Fatalf("without token: %#v", plan.Quests)
	}

	if _, err := db.Exec(`INSERT INTO gafam_settings (key,value) VALUES (?,?)`,
		settingDaksinaToken, "test-token"); err != nil {
		t.Fatal(err)
	}
	plan = fallbackPlan("peux tu t'auto améliorer ?")
	if len(plan.Quests) != 1 || plan.Quests[0].Tool != "daksina.run" {
		t.Fatalf("with token: %#v", plan.Quests)
	}

	plan = fallbackPlan("quelle heure est-il ?")
	if len(plan.Quests) != 1 || plan.Quests[0].Tool != "llm.chat" {
		t.Fatalf("normal question: %#v", plan.Quests)
	}
}

func TestDaksinaToolRegistered(t *testing.T) {
	registerDaksinaTool()
	tool, ok := karaka.GetTool("daksina.run")
	if !ok {
		t.Fatal("daksina.run tool not registered")
	}
	if !strings.Contains(tool.Description, "self-improvement") {
		t.Fatalf("unexpected description: %q", tool.Description)
	}
}

func TestDaksinaHistorySnapshot(t *testing.T) {
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
		`CREATE TABLE gafam_sms (id INTEGER PRIMARY KEY AUTOINCREMENT, sender TEXT, body TEXT,
			timestamp INTEGER, status TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE moksa_missions (id TEXT PRIMARY KEY, status TEXT, mode TEXT,
			instruction TEXT, data TEXT, updated_at DATETIME)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO gafam_sms (sender, body, timestamp, status) VALUES (?, ?, ?, ?)`,
		"+33628782725", "/q peux tu t'auto améliorer ?", 1, "inbox"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO moksa_missions (id,status,mode,instruction,data,updated_at)
		VALUES (?,?,?,?,?,?)`, "m1", "done", "", "tout va bien ?", "{}", "2026-11-11 00:22:07"); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	daksinaWriteHistorySnapshot(dir)
	if _, err := os.Stat(filepath.Join(dir, "gafam-history.sqlite")); err != nil {
		t.Fatalf("snapshot missing: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "history.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Historique GAFAM", "/q peux tu t'auto améliorer", "tout va bien"} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Fatalf("history.md missing %q:\n%s", want, raw)
		}
	}
}

func TestDaksinaPickProviderPrefersActiveEngine(t *testing.T) {
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
	if _, err := db.Exec(`CREATE TABLE gafam_settings (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	providers, _ := json.Marshal([]LLMProvider{
		{ID: "p1", Name: "Suspended", BaseURL: "https://kimi.test/v1", APIKey: "k1", Model: "kimi-k3", Enabled: true},
		{ID: "p2", Name: "Active", BaseURL: "https://deepseek.test/v1", APIKey: "k2", Model: "deepseek-v4-pro", Enabled: true},
	})
	for _, kv := range [][2]string{
		{settingLLMProviders, string(providers)},
		{settingLLMEngine, "provider:p2"},
	} {
		if _, err := db.Exec(`INSERT INTO gafam_settings (key,value) VALUES (?,?)`, kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	p, err := daksinaPickProvider()
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "p2" {
		t.Fatalf("picked %q, want the active engine provider p2", p.ID)
	}
}

func TestSelfQuestDaksinaPrefix(t *testing.T) {
	oldDB := db
	var err error
	db, err = sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.Close()
		db = oldDB
	}()
	if _, err := db.Exec(`CREATE TABLE gafam_settings (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gafam_settings (key, value) VALUES ('self_phone', '+33612345678')`); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		body        string
		wantInstr   string
		wantMode    string
		wantMatched bool
	}{
		{"/d corrige le bug", "corrige le bug", "daksina", true},
		{"/DEV ajoute un test", "ajoute un test", "daksina", true},
		{"/dak refactor logs", "refactor logs", "daksina", true},
		{"/code documente l'API", "documente l'API", "daksina", true},
		{"/q envoie un SMS", "envoie un SMS", "action", true},
		{"/r cherche la météo", "cherche la météo", "research", true},
	}
	for _, c := range cases {
		instr, mode, ok := selfQuestInstruction("+33 6 12 34 56 78", c.body)
		if ok != c.wantMatched || instr != c.wantInstr || mode != c.wantMode {
			t.Fatalf("body=%q got (%q,%q,%v), want (%q,%q,%v)",
				c.body, instr, mode, ok, c.wantInstr, c.wantMode, c.wantMatched)
		}
	}
	if _, _, ok := selfQuestInstruction("+33600000000", "/d hacked"); ok {
		t.Fatal("a non-self phone must not trigger a Dakṣiṇā run")
	}
}
