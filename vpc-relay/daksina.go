package main

// Dakṣiṇā — the self-improvement loop (Manifest 27).
//
// A dedicated sidecar (gafam-opencode) runs headless opencode on a checkout of
// the GAFAM repo. The relay wakes it, builds a context pack (relay logs, Docker
// logs, opencode conversations, Vault), polls the run and texts the owner the
// result. The runner tests every change, pushes a branch, opens a PR and merges
// automatically when the tests are green. It never pushes directly to main.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Garletz/gafam/vpc-relay/karaka"
)

const (
	daksinaContainer     = "gafam-opencode"
	daksinaDefaultImage  = "ghcr.io/garletz/gafam:opencode"
	daksinaDockerSock    = "/var/run/docker.sock"
	daksinaDockerAPIBase = "http://localhost"
	daksinaPort          = "6092"
)

const (
	settingDaksinaEnabled   = "daksina_enabled"
	settingDaksinaRepo      = "daksina_github_repo"
	settingDaksinaToken     = "daksina_github_token"
	settingDaksinaAutoMerge = "daksina_auto_merge"
	settingDaksinaProvider  = "daksina_provider_id"
	settingDaksinaModel     = "daksina_model"
)

var (
	daksinaMu           sync.Mutex // one run / container operation at a time
	daksinaActivityMu   sync.Mutex
	daksinaLastActivity = time.Now()
	daksinaRepoRE       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	daksinaRunIDRE      = regexp.MustCompile(`^[A-Za-z0-9_-]{4,80}$`)
	daksinaSessionRE    = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,120}$`)
)

// ─── Database ──────────────────────────────────────────────────────────────

type daksinaRun struct {
	ID          string `json:"id"`
	Instruction string `json:"instruction"`
	Status      string `json:"status"`
	Phase       string `json:"phase"`
	Repo        string `json:"repo"`
	Branch      string `json:"branch"`
	PRURL       string `json:"pr_url"`
	Summary     string `json:"summary"`
	Error       string `json:"error"`
	Log         string `json:"log,omitempty"`
	AutoMerge   bool   `json:"auto_merge"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func initDaksina() {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS daksina_runs (
		id TEXT PRIMARY KEY,
		instruction TEXT,
		status TEXT DEFAULT 'queued',
		phase TEXT DEFAULT 'queued',
		repo TEXT,
		branch TEXT,
		pr_url TEXT,
		summary TEXT,
		error TEXT,
		log TEXT,
		auto_merge INTEGER DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		log.Printf("daksina: create table: %v", err)
		return
	}
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_daksina_runs_updated ON daksina_runs(updated_at DESC)`)

	// A relay restart interrupts in-flight runs. The sidecar may still be
	// working; the operator can inspect it, but the board must not lie.
	if _, err := db.Exec(`UPDATE daksina_runs
		SET status='interrupted', phase='interrupted', updated_at=CURRENT_TIMESTAMP
		WHERE status IN ('queued','running','testing','pushing')`); err != nil {
		log.Printf("daksina: resume cleanup: %v", err)
	}

	ensureDaksinaDirs()
	log.Println("daksina: self-improvement runner ready")
}

func ensureDaksinaDirs() {
	dirs := []string{
		"daksina", "daksina/context", "daksina/runs", "daksina/sessions/imported",
		"daksina/workspace", "opencode", "opencode-config",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(dataDir(), d), 0o755); err != nil {
			log.Printf("daksina: mkdir %s: %v", d, err)
		}
	}
}

func daksinaDataDir() string {
	return filepath.Join(dataDir(), "daksina")
}

func daksinaHostPath(sub string) string {
	base := os.Getenv("GAFAM_HOST_DATA")
	if base == "" {
		base = "/root/gafam_data"
	}
	return filepath.Join(base, sub)
}

func createDaksinaRun(instruction string, autoMerge bool) (*daksinaRun, error) {
	id := "dk_" + strconv.FormatInt(time.Now().Unix(), 36) + "_" + generateToken(6)
	id = strings.ToLower(id)
	if !daksinaRunIDRE.MatchString(id) {
		return nil, fmt.Errorf("invalid run id")
	}
	_, err := db.Exec(
		`INSERT INTO daksina_runs (id, instruction, status, phase, repo, auto_merge)
		 VALUES (?, ?, 'queued', 'queued', ?, ?)`,
		id, instruction, daksinaRepo(), boolToInt(autoMerge),
	)
	if err != nil {
		return nil, err
	}
	return getDaksinaRun(id)
}

func getDaksinaRun(id string) (*daksinaRun, error) {
	var r daksinaRun
	var auto int
	err := db.QueryRow(
		`SELECT id, instruction, status, phase, COALESCE(repo,''), COALESCE(branch,''),
		        COALESCE(pr_url,''), COALESCE(summary,''), COALESCE(error,''),
		        COALESCE(log,''), auto_merge, created_at, updated_at
		 FROM daksina_runs WHERE id = ?`, id,
	).Scan(&r.ID, &r.Instruction, &r.Status, &r.Phase, &r.Repo, &r.Branch,
		&r.PRURL, &r.Summary, &r.Error, &r.Log, &auto, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	r.AutoMerge = auto != 0
	return &r, nil
}

func updateDaksinaRun(r *daksinaRun) {
	if r.ID == "" {
		return
	}
	_, err := db.Exec(
		`UPDATE daksina_runs SET status=?, phase=?, repo=?, branch=?, pr_url=?,
		        summary=?, error=?, log=?, auto_merge=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		r.Status, r.Phase, r.Repo, r.Branch, r.PRURL, r.Summary, r.Error,
		truncateStr(r.Log, 30000), boolToInt(r.AutoMerge), r.ID,
	)
	if err != nil {
		log.Printf("daksina: update %s: %v", r.ID, err)
	}
}

func listDaksinaRuns(limit int) []daksinaRun {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := db.Query(
		`SELECT id, instruction, status, phase, COALESCE(repo,''), COALESCE(branch,''),
		        COALESCE(pr_url,''), COALESCE(summary,''), COALESCE(error,''),
		        auto_merge, created_at, updated_at
		 FROM daksina_runs ORDER BY updated_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return []daksinaRun{}
	}
	defer rows.Close()
	out := []daksinaRun{}
	for rows.Next() {
		var r daksinaRun
		var auto int
		if err := rows.Scan(&r.ID, &r.Instruction, &r.Status, &r.Phase, &r.Repo,
			&r.Branch, &r.PRURL, &r.Summary, &r.Error, &auto,
			&r.CreatedAt, &r.UpdatedAt); err == nil {
			r.AutoMerge = auto != 0
			out = append(out, r)
		}
	}
	return out
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ─── Settings ─────────────────────────────────────────────────────────────

func daksinaRepo() string {
	repo := strings.TrimSpace(getSetting(settingDaksinaRepo))
	if repo == "" {
		return "Garletz/gafam"
	}
	return repo
}

func daksinaEnabled() bool {
	return getSetting(settingDaksinaEnabled) != "0"
}

func daksinaAutoMerge() bool {
	return getSetting(settingDaksinaAutoMerge) != "0"
}

func daksinaGithubToken() string {
	raw := getSetting(settingDaksinaToken)
	if raw == "" {
		return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	}
	if dec, err := unsealSettingsValue(raw); err == nil {
		return string(dec)
	}
	return raw // legacy / manually set plaintext
}

func daksinaTokenHint(token string) string {
	if len(token) <= 4 {
		if token == "" {
			return ""
		}
		return "…"
	}
	return "…" + token[len(token)-4:]
}

func daksinaPickProvider() (LLMProvider, error) {
	wanted := strings.TrimSpace(getSetting(settingDaksinaProvider))
	if wanted != "" {
		if p, ok := findProvider(wanted); ok {
			if !p.Enabled {
				return p, fmt.Errorf("provider %q is disabled", p.Name)
			}
			if p.APIKey == "" {
				return p, fmt.Errorf("provider %q has no API key", p.Name)
			}
			return p, nil
		}
	}
	// Prefer the engine the user actually selected (or the orchestrator scope
	// primary) so a suspended/empty provider is not picked just because it is
	// first in the list.
	candidates := []string{activeEngine(), getScopeRouting("orchestrator").Primary}
	for _, c := range candidates {
		if !strings.HasPrefix(c, "provider:") {
			continue
		}
		if p, ok := findProvider(strings.TrimPrefix(c, "provider:")); ok &&
			p.Enabled && p.APIKey != "" && p.BaseURL != "" {
			return p, nil
		}
	}
	for _, p := range loadLLMProviders() {
		if p.Enabled && p.APIKey != "" && p.BaseURL != "" {
			return p, nil
		}
	}
	return LLMProvider{}, fmt.Errorf("no enabled LLM provider with an API key (Settings → Suparna → Providers)")
}

func daksinaSharedSecret() string {
	h := sha256.New()
	h.Write(jwtSecret)
	h.Write([]byte("daksina-sidecar"))
	return hex.EncodeToString(h.Sum(nil))[:40]
}

func daksinaTouch() {
	daksinaActivityMu.Lock()
	daksinaLastActivity = time.Now()
	daksinaActivityMu.Unlock()
}

// ─── Docker (unix socket, no docker CLI needed) ───────────────────────────

func daksinaDockerHTTP() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Minute,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", daksinaDockerSock)
			},
		},
	}
}

func daksinaDockerSockPresent() bool {
	st, err := os.Stat(daksinaDockerSock)
	return err == nil && st.Mode()&os.ModeSocket != 0
}

func daksinaImage() string {
	if v := strings.TrimSpace(os.Getenv("OPENCODE_IMAGE")); v != "" {
		return v
	}
	return daksinaDefaultImage
}

func daksinaContainerState() (bool, error) {
	if !daksinaDockerSockPresent() {
		return false, fmt.Errorf("docker.sock unavailable")
	}
	resp, err := daksinaDockerHTTP().Get(daksinaDockerAPIBase + "/containers/" + daksinaContainer + "/json")
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound {
		return false, fmt.Errorf("container %s missing", daksinaContainer)
	}
	if resp.StatusCode >= 400 {
		return false, fmt.Errorf("docker inspect: %s", strings.TrimSpace(string(body)))
	}
	var info struct {
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return false, err
	}
	return info.State.Running, nil
}

func daksinaImageExists(image string) bool {
	resp, err := daksinaDockerHTTP().Get(daksinaDockerAPIBase + "/images/" + image + "/json")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return resp.StatusCode == http.StatusOK
}

func daksinaPullImage(image string) error {
	from, tag := image, "latest"
	if i := strings.LastIndex(image, ":"); i > 0 && !strings.Contains(image[i+1:], "/") {
		from, tag = image[:i], image[i+1:]
	}
	q := url.Values{}
	q.Set("fromImage", from)
	q.Set("tag", tag)
	req, err := http.NewRequest(http.MethodPost, daksinaDockerAPIBase+"/images/create?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := daksinaDockerHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 || bytes.Contains(body, []byte(`"error"`)) {
		return fmt.Errorf("pull %s: %s", image, strings.TrimSpace(string(body)))
	}
	return nil
}

func daksinaCreateContainer(image string) error {
	memMB := int64(1536)
	if v := os.Getenv("OPENCODE_MEMORY_MB"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 512 {
			memMB = n
		}
	}
	cfg := map[string]interface{}{
		"Image": image,
		"Env": []string{
			"OPENCODE_SECRET=" + daksinaSharedSecret(),
			"OPENCODE_PORT=" + daksinaPort,
			"OPENCODE_DISABLE_AUTOUPDATE=1",
		},
		"ExposedPorts": map[string]interface{}{daksinaPort + "/tcp": map[string]interface{}{}},
		"HostConfig": map[string]interface{}{
			"Memory":     memMB * 1024 * 1024,
			"MemorySwap": memMB * 2 * 1024 * 1024,
			"RestartPolicy": map[string]interface{}{
				"Name": "no",
			},
			"NetworkMode": "gafam-net",
			"Binds": []string{
				daksinaHostPath("daksina") + ":/daksina",
				daksinaHostPath("") + ":/daksina/gafam-data:ro",
				daksinaHostPath("opencode") + ":/root/.local/share/opencode",
				daksinaHostPath("opencode-config") + ":/root/.config/opencode",
				daksinaHostPath("logs") + ":/daksina/logs:ro",
				daksinaHostPath("sandbox/files") + ":/daksina/vault:ro",
			},
		},
		"NetworkingConfig": map[string]interface{}{
			"EndpointsConfig": map[string]interface{}{
				"gafam-net": map[string]interface{}{},
			},
		},
	}
	payload, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost,
		daksinaDockerAPIBase+"/containers/create?name="+url.QueryEscape(daksinaContainer),
		bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := daksinaDockerHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusConflict {
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("create: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func daksinaRemoveContainer() error {
	req, err := http.NewRequest(http.MethodDelete,
		daksinaDockerAPIBase+"/containers/"+daksinaContainer+"?force=true", nil)
	if err != nil {
		return err
	}
	resp, err := daksinaDockerHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return nil
}

func daksinaStartContainer() error {
	req, err := http.NewRequest(http.MethodPost,
		daksinaDockerAPIBase+"/containers/"+daksinaContainer+"/start", nil)
	if err != nil {
		return err
	}
	resp, err := daksinaDockerHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("start: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func daksinaStopContainer() error {
	req, err := http.NewRequest(http.MethodPost,
		daksinaDockerAPIBase+"/containers/"+daksinaContainer+"/stop?t=15", nil)
	if err != nil {
		return err
	}
	resp, err := daksinaDockerHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return nil
}

func daksinaBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("OPENCODE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://" + daksinaContainer + ":" + daksinaPort
}

// daksinaExternalRunner is true when the sidecar is not managed by this relay
// (dev machine, external runner). Docker management is then skipped and the
// relay only probes OPENCODE_URL.
func daksinaExternalRunner() bool {
	return strings.TrimSpace(os.Getenv("OPENCODE_URL")) != ""
}

// daksinaRunnerAvailable reports whether the sidecar can be reached right now.
func daksinaRunnerAvailable() bool {
	if daksinaExternalRunner() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := daksinaHealth(ctx)
		return err == nil
	}
	running, err := daksinaContainerState()
	return err == nil && running
}

func daksinaHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

func daksinaHealth(ctx context.Context) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, daksinaBaseURL()+"/health", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-GAFAM-DAKSINA", daksinaSharedSecret())
	resp, err := daksinaHTTPClient(6 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("health HTTP %d", resp.StatusCode)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func daksinaWaitReady(ctx context.Context) error {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		probe, cancel := context.WithTimeout(ctx, 8*time.Second)
		_, err := daksinaHealth(probe)
		cancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("opencode sidecar not ready: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// daksinaEnsureReady starts the sidecar, pulling/creating the container when
// needed. Caller must hold daksinaMu (or be the idle reaper).
func daksinaEnsureReady(ctx context.Context) error {
	ensureDaksinaDirs()
	if daksinaExternalRunner() {
		if err := daksinaWaitReady(ctx); err != nil {
			return err
		}
		daksinaTouch()
		return nil
	}
	running, err := daksinaContainerState()
	if err != nil && !strings.Contains(err.Error(), "missing") {
		return err
	}
	image := daksinaImage()

	if err != nil && strings.Contains(err.Error(), "missing") {
		if !daksinaImageExists(image) {
			if err := daksinaPullImage(image); err != nil {
				return err
			}
		}
		if err := daksinaCreateContainer(image); err != nil {
			return err
		}
	}
	if !running {
		if err := daksinaStartContainer(); err != nil {
			log.Printf("daksina: start failed (%v) — recreating from %s", err, image)
			if pullErr := daksinaPullImage(image); pullErr != nil {
				return fmt.Errorf("%v; pull: %w", err, pullErr)
			}
			_ = daksinaRemoveContainer()
			if createErr := daksinaCreateContainer(image); createErr != nil {
				return createErr
			}
			if startErr := daksinaStartContainer(); startErr != nil {
				return startErr
			}
		}
	}
	if err := daksinaWaitReady(ctx); err != nil {
		return err
	}
	daksinaTouch()
	return nil
}

func startDaksinaIdleReaper() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if daksinaExternalRunner() {
				continue // never stop a runner we do not manage
			}
			daksinaActivityMu.Lock()
			idle := time.Since(daksinaLastActivity)
			daksinaActivityMu.Unlock()
			if idle < 15*time.Minute {
				continue
			}
			if !daksinaMu.TryLock() {
				continue // run in progress
			}
			running, err := daksinaContainerState()
			if err == nil && running {
				log.Println("daksina: idle — stopping gafam-opencode")
				_ = daksinaStopContainer()
			}
			daksinaMu.Unlock()
		}
	}()
}

// ─── Sidecar API ──────────────────────────────────────────────────────────

type daksinaTaskRequest struct {
	ID          string            `json:"id"`
	Instruction string            `json:"instruction"`
	Repo        string            `json:"repo"`
	GithubToken string            `json:"github_token"`
	AutoMerge   bool              `json:"auto_merge"`
	Model       map[string]string `json:"model"`
	Title       string            `json:"title,omitempty"`
}

type daksinaSidecarStatus struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Phase        string `json:"phase"`
	Instruction  string `json:"instruction"`
	Title        string `json:"title"`
	Repo         string `json:"repo"`
	Branch       string `json:"branch"`
	PRURL        string `json:"pr_url"`
	Summary      string `json:"summary"`
	Error        string `json:"error"`
	TestSummary  string `json:"test_summary"`
	AgentSummary string `json:"agent_summary"`
	Merged       bool   `json:"merged"`
	ManualReview bool   `json:"manual_review"`
	LogTail      string `json:"log_tail"`
}

func daksinaPostJSON(ctx context.Context, path string, payload interface{}) (int, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, daksinaBaseURL()+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GAFAM-DAKSINA", daksinaSharedSecret())
	resp, err := daksinaHTTPClient(30 * time.Second).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, out, nil
}

func daksinaFetchTask(ctx context.Context, runID string) (*daksinaSidecarStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, daksinaBaseURL()+"/task/"+url.PathEscape(runID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-GAFAM-DAKSINA", daksinaSharedSecret())
	resp, err := daksinaHTTPClient(20 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("sidecar HTTP %d: %s", resp.StatusCode, truncateStr(strings.TrimSpace(string(body)), 200))
	}
	var st daksinaSidecarStatus
	if err := json.Unmarshal(body, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func daksinaCancelTask(ctx context.Context, runID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		daksinaBaseURL()+"/task/"+url.PathEscape(runID)+"/cancel", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-GAFAM-DAKSINA", daksinaSharedSecret())
	resp, err := daksinaHTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return nil
}

func daksinaSidecarJSON(ctx context.Context, method, path string, body []byte, limit int64) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, daksinaBaseURL()+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("X-GAFAM-DAKSINA", daksinaSharedSecret())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := daksinaHTTPClient(30 * time.Second).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, limit))
	return resp.StatusCode, out, nil
}

// ─── Context pack + Docker logs ───────────────────────────────────────────

func daksinaDockerLogs(container string, tail int) (string, error) {
	if !daksinaDockerSockPresent() {
		return "", fmt.Errorf("docker.sock unavailable")
	}
	u := fmt.Sprintf("%s/containers/%s/logs?stdout=1&stderr=1&timestamps=1&tail=%d",
		daksinaDockerAPIBase, url.PathEscape(container), tail)
	resp, err := daksinaDockerHTTP().Get(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("docker logs: %s", truncateStr(strings.TrimSpace(string(raw)), 200))
	}
	return demuxDockerStream(raw), nil
}

// demuxDockerStream decodes the multiplexed stdout/stderr framing used by the
// Docker logs API for non-TTY containers.
func demuxDockerStream(b []byte) string {
	var out bytes.Buffer
	for len(b) >= 8 {
		n := int(binary.BigEndian.Uint32(b[4:8]))
		if n < 0 || 8+n > len(b) {
			break
		}
		out.Write(b[8 : 8+n])
		b = b[8+n:]
	}
	if out.Len() == 0 {
		return string(b)
	}
	return out.String()
}

func daksinaBuildContext(run *daksinaRun) (string, error) {
	dir := filepath.Join(daksinaDataDir(), "context", run.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dockerLog := func(name string) string {
		out, err := daksinaDockerLogs(name, 400)
		if err != nil {
			return fmt.Sprintf("(%s logs unavailable: %v)\n", name, err)
		}
		return out
	}
	content := "### gafam-api\n" + dockerLog("gafam-api") +
		"\n\n### gafam-opencode\n" + dockerLog(daksinaContainer)
	if tok := daksinaGithubToken(); tok != "" {
		content = strings.ReplaceAll(content, tok, "***")
	}
	_ = os.WriteFile(filepath.Join(dir, "docker-logs.txt"), []byte(content), 0o644)

	daksinaWriteHistorySnapshot(dir)

	meta, _ := json.MarshalIndent(map[string]interface{}{
		"id": run.ID, "repo": run.Repo, "instruction": run.Instruction,
		"auto_merge": run.AutoMerge, "created_at": run.CreatedAt,
	}, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "meta.json"), meta, 0o644)
	return dir, nil
}

// daksinaWriteHistorySnapshot gives the coding agent a consistent read-only
// copy of the node's own history (SMS, contacts, missions, runs, outbox) even
// while the relay keeps writing to the live WAL database.
func daksinaWriteHistorySnapshot(dir string) {
	snap := filepath.Join(dir, "gafam-history.sqlite")
	_ = os.Remove(snap)
	stmt := "VACUUM INTO '" + strings.ReplaceAll(snap, "'", "''") + "'"
	if _, err := db.Exec(stmt); err != nil {
		log.Printf("daksina: history snapshot failed: %v", err)
		return
	}

	var b strings.Builder
	b.WriteString("# Historique GAFAM — snapshot SQLite\n\n")
	b.WriteString("Base cohérente : `gafam-history.sqlite` (lecture seule, dans ce dossier).\n\n")
	b.WriteString("Tables utiles : `gafam_sms`, `gafam_outbox`, `gafam_contacts`, `moksa_missions`, `daksina_runs`, `log_days`, `gafam_envelopes`.\n")
	b.WriteString("Exemples :\n```sql\n")
	b.WriteString("SELECT sender, body, datetime(created_at) FROM gafam_sms ORDER BY id DESC LIMIT 50;\n")
	b.WriteString("SELECT id, status, instruction, summary, updated_at FROM moksa_missions ORDER BY updated_at DESC LIMIT 20;\n")
	b.WriteString("SELECT id, status, phase, instruction, summary, updated_at FROM daksina_runs ORDER BY updated_at DESC LIMIT 20;\n")
	b.WriteString("```\n\n")

	b.WriteString("## Derniers SMS\n")
	rows, err := db.Query(`SELECT COALESCE(sender,''), COALESCE(body,''), COALESCE(created_at,'')
		FROM gafam_sms ORDER BY id DESC LIMIT 25`)
	if err == nil {
		for rows.Next() {
			var sender, body, created string
			if rows.Scan(&sender, &body, &created) == nil {
				fmt.Fprintf(&b, "- %s · %s · %s\n", created, sender, truncateStr(strings.ReplaceAll(body, "\n", " "), 220))
			}
		}
		rows.Close()
	}

	b.WriteString("\n## Dernières missions Saṃyojaka\n")
	rows, err = db.Query(`SELECT id, COALESCE(status,''), COALESCE(instruction,''), COALESCE(updated_at,'')
		FROM moksa_missions ORDER BY updated_at DESC LIMIT 12`)
	if err == nil {
		for rows.Next() {
			var id, status, instruction, updated string
			if rows.Scan(&id, &status, &instruction, &updated) == nil {
				fmt.Fprintf(&b, "- %s · %s · %s\n  %s\n", updated, id, status, truncateStr(strings.ReplaceAll(instruction, "\n", " "), 180))
			}
		}
		rows.Close()
	}
	_ = os.WriteFile(filepath.Join(dir, "history.md"), []byte(b.String()), 0o644)
}

// registerDaksinaTool exposes the self-improvement runner to Saṃyojaka so
// « /q peux-tu t'auto-améliorer ? » can actually launch Dakṣiṇā.
func registerDaksinaTool() {
	karaka.RegisterTool(karaka.Tool{
		ID: "daksina.run",
		Description: "Launch Dakṣiṇā, the self-improvement coding agent, on the GAFAM repository. " +
			"It can read logs, SMS/mission history, Vault and opencode conversations, modify the code, " +
			"run the tests, push a branch and open/merge a PR. Use it whenever the user asks to improve, " +
			"fix, inspect or modify GAFAM itself (auto-amélioration, opencode, GitHub, ton propre code).",
		Category: "dev",
		Params: map[string]karaka.ParamSpec{
			"instruction": {Type: "string", Required: true, Description: "What to change or investigate in the GAFAM codebase"},
			"auto_merge":  {Type: "bool", Required: false, Description: "Merge automatically when tests pass (default from settings)", Default: true},
		},
		Returns: "{ run_id: string, status: string }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			instruction, _ := params["instruction"].(string)
			if strings.TrimSpace(instruction) == "" {
				return nil, fmt.Errorf("missing 'instruction'")
			}
			if !daksinaEnabled() {
				return nil, fmt.Errorf("Dakṣiṇā is disabled in settings")
			}
			if daksinaGithubToken() == "" {
				return nil, fmt.Errorf("GitHub token not configured (Settings → Dakṣiṇā)")
			}
			if _, err := daksinaPickProvider(); err != nil {
				return nil, err
			}
			auto := daksinaAutoMerge()
			if b, ok := params["auto_merge"].(bool); ok {
				auto = b
			}
			run, err := startDaksinaRun(instruction, auto, getSelfPhone())
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"run_id": run.ID, "status": run.Status,
				"message": "Dakṣiṇā started; the final report (tests + PR) will arrive by SMS.",
			}, nil
		},
	})
}

// ─── Runner ───────────────────────────────────────────────────────────────

func triggerDaksina(selfPhone, instruction string) {
	if !daksinaEnabled() {
		queueSmsReply(selfPhone, "GAFAM ⚙️ Dakṣiṇā désactivée (Settings → Dakṣiṇā).")
		return
	}
	if daksinaGithubToken() == "" {
		queueSmsReply(selfPhone, "GAFAM ⚙️ Dakṣiṇā: configure un token GitHub (Settings → Dakṣiṇā) avant de lancer une mission.")
		return
	}
	if _, err := daksinaPickProvider(); err != nil {
		queueSmsReply(selfPhone, "GAFAM ⚙️ Dakṣiṇā: "+truncateStr(err.Error(), 300))
		return
	}
	run, err := startDaksinaRun(instruction, daksinaAutoMerge(), selfPhone)
	if err != nil {
		queueSmsReply(selfPhone, "GAFAM ⏳ "+truncateStr(err.Error(), 300))
		return
	}
	queueSmsReply(selfPhone, fmt.Sprintf(
		"GAFAM 🛠️ Dakṣiṇā %s démarrée. Je te texte le résultat (PR + tests) à la fin.", run.ID))
}

func startDaksinaRun(instruction string, autoMerge bool, notifyPhone string) (*daksinaRun, error) {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return nil, fmt.Errorf("instruction vide")
	}
	if len(instruction) > 12000 {
		return nil, fmt.Errorf("instruction trop longue (max 12000 caractères)")
	}
	if !daksinaMu.TryLock() {
		return nil, fmt.Errorf("une mission Dakṣiṇā tourne déjà")
	}
	run, err := createDaksinaRun(instruction, autoMerge)
	if err != nil {
		daksinaMu.Unlock()
		return nil, err
	}
	go func() {
		defer daksinaMu.Unlock()
		runDaksina(run, notifyPhone)
	}()
	return run, nil
}

func daksinaPollInterval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("DAKSINA_POLL_INTERVAL_MS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 10 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return 5 * time.Second
}

func runDaksina(run *daksinaRun, notifyPhone string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	daksinaTouch()

	fail := func(msg string) {
		run.Status = "failed"
		run.Phase = "failed"
		run.Error = msg
		run.Summary = "Échec : " + msg
		updateDaksinaRun(run)
		if notifyPhone != "" {
			queueSmsReply(notifyPhone, fmt.Sprintf(
				"GAFAM ❌ Dakṣiṇā %s: %s", run.ID, truncateStr(msg, 500)))
		}
	}

	run.Status = "running"
	run.Phase = "wake"
	updateDaksinaRun(run)

	if err := daksinaEnsureReady(ctx); err != nil {
		fail("sidecar opencode indisponible: " + err.Error())
		return
	}

	provider, err := daksinaPickProvider()
	if err != nil {
		fail(err.Error())
		return
	}
	model := provider.Model
	if v := strings.TrimSpace(getSetting(settingDaksinaModel)); v != "" {
		model = v
	}

	run.Phase = "context"
	updateDaksinaRun(run)
	if _, err := daksinaBuildContext(run); err != nil {
		fail("contexte: " + err.Error())
		return
	}

	req := daksinaTaskRequest{
		ID:          run.ID,
		Instruction: run.Instruction,
		Repo:        run.Repo,
		GithubToken: daksinaGithubToken(),
		AutoMerge:   run.AutoMerge,
		Model: map[string]string{
			"base_url": provider.BaseURL,
			"api_key":  provider.APIKey,
			"model":    model,
		},
		Title: daksinaTitle(run.Instruction),
	}

	code, body, err := daksinaPostJSON(ctx, "/task", req)
	if err != nil {
		fail("lancement opencode: " + err.Error())
		return
	}
	if code >= 400 {
		fail(fmt.Sprintf("sidecar a refusé la tâche (HTTP %d): %s", code, truncateStr(string(body), 300)))
		return
	}

	run.Status = "running"
	run.Phase = "agent"
	updateDaksinaRun(run)

	ticker := time.NewTicker(daksinaPollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = daksinaCancelTask(context.Background(), run.ID)
			fail("timeout global (60 min)")
			return
		case <-ticker.C:
		}
		st, err := daksinaFetchTask(ctx, run.ID)
		if err != nil {
			log.Printf("daksina: poll %s: %v", run.ID, err)
			run.Log = truncateStr(run.Log+"\npoll error: "+err.Error()+"\n", 30000)
			updateDaksinaRun(run)
			continue
		}
		daksinaTouch()
		daksinaMergeSidecarStatus(run, st)
		updateDaksinaRun(run)
		if daksinaTerminal(st.Status) {
			daksinaFinish(run, st, notifyPhone)
			return
		}
	}
}

func daksinaMergeSidecarStatus(run *daksinaRun, st *daksinaSidecarStatus) {
	if st.Status != "" {
		run.Status = st.Status
	}
	if st.Phase != "" {
		run.Phase = st.Phase
	}
	if st.Branch != "" {
		run.Branch = st.Branch
	}
	if st.PRURL != "" {
		run.PRURL = st.PRURL
	}
	if st.Summary != "" {
		run.Summary = st.Summary
	}
	if st.Error != "" {
		run.Error = st.Error
	}
	if st.LogTail != "" {
		run.Log = truncateStr(st.LogTail, 30000)
	}
}

func daksinaTerminal(status string) bool {
	switch status {
	case "done", "failed", "cancelled", "pr_open":
		return true
	}
	return false
}

func daksinaFinish(run *daksinaRun, st *daksinaSidecarStatus, notifyPhone string) {
	updateDaksinaRun(run)
	if notifyPhone == "" {
		return
	}
	switch run.Status {
	case "done":
		msg := fmt.Sprintf("GAFAM ✅ Dakṣiṇā %s terminée.", run.ID)
		if st.Merged {
			msg += " PR mergée automatiquement."
		}
		if run.PRURL != "" {
			msg += " " + run.PRURL
		}
		if run.Summary != "" {
			msg += "\n" + truncateStr(run.Summary, 500)
		}
		if st.TestSummary != "" {
			msg += "\n🧪 " + truncateStr(st.TestSummary, 200)
		}
		queueSmsReply(notifyPhone, msg)
	case "pr_open":
		msg := fmt.Sprintf("GAFAM 🔀 Dakṣiṇā %s: PR ouverte mais pas encore mergée.", run.ID)
		if run.PRURL != "" {
			msg += "\n" + run.PRURL
		}
		queueSmsReply(notifyPhone, msg)
	case "cancelled":
		queueSmsReply(notifyPhone, fmt.Sprintf("GAFAM ⏹️ Dakṣiṇā %s annulée.", run.ID))
	default:
		msg := fmt.Sprintf("GAFAM ❌ Dakṣiṇā %s: %s", run.ID, truncateStr(run.Error, 600))
		if st.TestSummary != "" {
			msg += "\n🧪 " + truncateStr(st.TestSummary, 200)
		}
		queueSmsReply(notifyPhone, msg)
	}
}

func daksinaTitle(instruction string) string {
	line := strings.TrimSpace(strings.Split(strings.TrimSpace(instruction), "\n")[0])
	if len(line) > 80 {
		line = truncateBytes(line, 80)
	}
	if line == "" {
		line = "self-improvement"
	}
	return line
}

// ─── HTTP handlers ────────────────────────────────────────────────────────

func daksinaStatusHandler(w http.ResponseWriter, r *http.Request) {
	running := false
	installed := true
	errMsg := ""
	var health map[string]interface{}
	if daksinaExternalRunner() {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		var err error
		health, err = daksinaHealth(ctx)
		cancel()
		running = err == nil
		if err != nil {
			errMsg = err.Error()
		}
	} else {
		var err error
		running, err = daksinaContainerState()
		if err != nil {
			installed = !strings.Contains(err.Error(), "missing") && !strings.Contains(err.Error(), "unavailable")
			errMsg = err.Error()
		}
		if running {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			health, _ = daksinaHealth(ctx)
			cancel()
		}
	}
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"installed":   installed,
		"running":     running,
		"healthy":     health != nil,
		"health":      health,
		"error":       errMsg,
		"enabled":     daksinaEnabled(),
		"repo":        daksinaRepo(),
		"auto_merge":  daksinaAutoMerge(),
		"has_token":   daksinaGithubToken() != "",
		"token_hint":  daksinaTokenHint(daksinaGithubToken()),
		"provider_id": getSetting(settingDaksinaProvider),
		"model":       getSetting(settingDaksinaModel),
	})
}

func daksinaWakeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !daksinaMu.TryLock() {
		sendJSON(w, http.StatusConflict, map[string]string{"error": "daksina_busy"})
		return
	}
	defer daksinaMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	if err := daksinaEnsureReady(ctx); err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

func daksinaStopHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if daksinaExternalRunner() {
		sendJSON(w, http.StatusConflict, map[string]string{
			"error": "external runner (OPENCODE_URL) — stop it outside the relay",
		})
		return
	}
	if !daksinaMu.TryLock() {
		sendJSON(w, http.StatusConflict, map[string]string{"error": "a run is active — cancel it first"})
		return
	}
	defer daksinaMu.Unlock()
	if err := daksinaStopContainer(); err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func daksinaRunsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sendJSON(w, http.StatusOK, map[string]interface{}{"runs": listDaksinaRuns(50)})
	case http.MethodPost:
		if !daksinaEnabled() {
			sendJSON(w, http.StatusForbidden, map[string]string{"error": "Dakṣiṇā is disabled in settings"})
			return
		}
		var in struct {
			Instruction string `json:"instruction"`
			AutoMerge   *bool  `json:"auto_merge"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		auto := daksinaAutoMerge()
		if in.AutoMerge != nil {
			auto = *in.AutoMerge
		}
		if daksinaGithubToken() == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "GitHub token not configured"})
			return
		}
		if _, err := daksinaPickProvider(); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		run, err := startDaksinaRun(in.Instruction, auto, "")
		if err != nil {
			sendJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		sendJSON(w, http.StatusAccepted, run)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func daksinaRunHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !daksinaRunIDRE.MatchString(id) {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		run, err := getDaksinaRun(id)
		if err != nil {
			sendJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
			return
		}
		// Opportunistic refresh from the sidecar when it is awake.
		if daksinaRunnerAvailable() {
			ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
			if st, err := daksinaFetchTask(ctx, id); err == nil {
				daksinaMergeSidecarStatus(run, st)
				updateDaksinaRun(run)
			}
			cancel()
		}
		sendJSON(w, http.StatusOK, run)
	case http.MethodDelete:
		if daksinaRunnerAvailable() {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			_ = daksinaCancelTask(ctx, id)
			cancel()
		}
		if run, err := getDaksinaRun(id); err == nil {
			if !daksinaTerminal(run.Status) {
				run.Status = "cancelled"
				run.Phase = "cancelled"
				run.Summary = "Annulé par l'utilisateur."
				updateDaksinaRun(run)
			}
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{"cancelled": id})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func daksinaSettingsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"enabled":     daksinaEnabled(),
			"repo":        daksinaRepo(),
			"auto_merge":  daksinaAutoMerge(),
			"has_token":   daksinaGithubToken() != "",
			"token_hint":  daksinaTokenHint(daksinaGithubToken()),
			"provider_id": getSetting(settingDaksinaProvider),
			"model":       getSetting(settingDaksinaModel),
		})
	case http.MethodPost:
		var in struct {
			Enabled     *bool  `json:"enabled"`
			Repo        string `json:"repo"`
			AutoMerge   *bool  `json:"auto_merge"`
			GithubToken string `json:"github_token"`
			ClearToken  bool   `json:"clear_token"`
			ProviderID  string `json:"provider_id"`
			Model       string `json:"model"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		repo := strings.TrimSpace(in.Repo)
		if repo != "" && !daksinaRepoRE.MatchString(repo) {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "repo must be owner/name"})
			return
		}
		if in.Enabled != nil {
			v := "0"
			if *in.Enabled {
				v = "1"
			}
			_ = setSetting(settingDaksinaEnabled, v)
		}
		if in.AutoMerge != nil {
			v := "0"
			if *in.AutoMerge {
				v = "1"
			}
			_ = setSetting(settingDaksinaAutoMerge, v)
		}
		if repo != "" {
			_ = setSetting(settingDaksinaRepo, repo)
		}
		if strings.TrimSpace(in.ProviderID) != "" {
			_ = setSetting(settingDaksinaProvider, strings.TrimSpace(in.ProviderID))
		}
		if strings.TrimSpace(in.Model) != "" {
			_ = setSetting(settingDaksinaModel, strings.TrimSpace(in.Model))
		}
		if in.ClearToken {
			_, _ = db.Exec(`DELETE FROM gafam_settings WHERE key = ?`, settingDaksinaToken)
		} else if tok := strings.TrimSpace(in.GithubToken); tok != "" {
			sealed, err := sealSettingsValue([]byte(tok))
			if err != nil {
				sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			_ = setSetting(settingDaksinaToken, sealed)
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"ok":         true,
			"has_token":  daksinaGithubToken() != "",
			"token_hint": daksinaTokenHint(daksinaGithubToken()),
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func daksinaSessionsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		imported := daksinaListImportedSessions()
		sidecar := []map[string]interface{}{}
		if daksinaRunnerAvailable() {
			ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
			code, body, err := daksinaSidecarJSON(ctx, http.MethodGet, "/sessions", nil, 4<<20)
			cancel()
			if err == nil && code < 400 {
				var payload struct {
					Sessions []map[string]interface{} `json:"sessions"`
				}
				if json.Unmarshal(body, &payload) == nil {
					sidecar = payload.Sessions
				}
			}
		}
		seen := map[string]bool{}
		out := []map[string]interface{}{}
		for _, s := range sidecar {
			if id, _ := s["id"].(string); id != "" && !seen[id] {
				seen[id] = true
				out = append(out, s)
			}
		}
		for _, s := range imported {
			if id, _ := s["id"].(string); id != "" && !seen[id] {
				seen[id] = true
				out = append(out, s)
			}
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{"sessions": out})
	case http.MethodPost:
		// Import one or more `opencode export` JSON payloads.
		raw, err := io.ReadAll(io.LimitReader(r.Body, 25<<20))
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "read failed"})
			return
		}
		var single map[string]interface{}
		items := []map[string]interface{}{}
		if err := json.Unmarshal(raw, &single); err == nil {
			items = append(items, single)
		} else {
			var many []map[string]interface{}
			if err := json.Unmarshal(raw, &many); err != nil {
				sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON export"})
				return
			}
			items = many
		}
		dir := filepath.Join(daksinaDataDir(), "sessions", "imported")
		_ = os.MkdirAll(dir, 0o755)
		imported := []map[string]interface{}{}
		for _, item := range items {
			info, _ := item["info"].(map[string]interface{})
			id, _ := info["id"].(string)
			if !daksinaSessionRE.MatchString(id) {
				continue
			}
			data, err := json.MarshalIndent(item, "", "  ")
			if err != nil {
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, id+".json"), data, 0o644); err != nil {
				continue
			}
			title, _ := info["title"].(string)
			imported = append(imported, map[string]interface{}{
				"id": id, "title": title, "messages": len(asSlice(item["messages"])),
			})
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{"imported": imported})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func daksinaSessionHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !daksinaSessionRE.MatchString(id) {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	path := filepath.Join(daksinaDataDir(), "sessions", "imported", id+".json")
	if raw, err := os.ReadFile(path); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(raw)
		return
	}
	if daksinaRunnerAvailable() {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		code, body, err := daksinaSidecarJSON(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id), nil, 8<<20)
		cancel()
		if err == nil && code < 400 {
			w.Header().Set("Content-Type", "application/json")
			w.Write(body)
			return
		}
	}
	sendJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
}

func daksinaListImportedSessions() []map[string]interface{} {
	dir := filepath.Join(daksinaDataDir(), "sessions", "imported")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []map[string]interface{}{}
	}
	out := []map[string]interface{}{}
	for _, e := range entries {
		if len(out) >= 100 {
			break
		}
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var data struct {
			Info map[string]interface{} `json:"info"`
		}
		if json.Unmarshal(raw, &data) != nil || data.Info == nil {
			continue
		}
		id, _ := data.Info["id"].(string)
		title, _ := data.Info["title"].(string)
		out = append(out, map[string]interface{}{
			"id": id, "title": title, "source": "imported",
			"directory": data.Info["directory"],
		})
	}
	return out
}

func asSlice(v interface{}) []interface{} {
	if s, ok := v.([]interface{}); ok {
		return s
	}
	return nil
}
