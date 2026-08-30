package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// ─── Playwright MCP sidecar ───────────────────────────────────────────────
// gafam-mcp runs the @playwright/mcp server attached (CDP) to the shared
// Chrome instance. The Go orchestrator calls it over streamable HTTP at
// http://gafam-mcp:8931/mcp on the internal gafam-net. Because the sidecar
// attaches to the SAME Chrome the human sees, the agent works in the same
// session — cookies, logins and tabs included. No navigator.webdriver flag:
// no automation switches are ever passed to Chrome.

// EnsureMcpContainer starts the MCP sidecar, creating it from GHCR if missing.
func EnsureMcpContainer(ctx context.Context) error {
	if !dockerSockPresent() {
		return fmt.Errorf("docker.sock unavailable — mount /var/run/docker.sock on gafam-api")
	}
	exists, err := containerNamedExists(mcpContainer)
	if err != nil {
		return err
	}
	if exists {
		return dockerStartIfStopped(mcpContainer)
	}
	if err := pullImage(defaultMcpImage); err != nil {
		return fmt.Errorf("pull %s: %w", defaultMcpImage, err)
	}
	if err := createMcpContainer(); err != nil {
		return err
	}
	return dockerStartIfStopped(mcpContainer)
}

func createMcpContainer() error {
	cfg := map[string]interface{}{
		"Image":      defaultMcpImage,
		"Entrypoint": []string{"playwright-mcp"},
		"Cmd": []string{
			"--port", "8931",
			"--host", "0.0.0.0",
			"--allowed-hosts", "*",
			"--cdp-endpoint", "http://gafam-browser:9222",
			"--image-responses", "omit",
			"--output-dir", "/data/output",
		},
		"HostConfig": map[string]interface{}{
			"Memory":     int64(512) * 1024 * 1024,
			"MemorySwap": int64(768) * 1024 * 1024,
			"RestartPolicy": map[string]interface{}{
				"Name": "unless-stopped",
			},
			"NetworkMode": "gafam-net",
			"Binds": []string{
				"/root/gafam_data/browser-mcp:/data",
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
	req, err := http.NewRequest(http.MethodPost, dockerAPIBase+"/containers/create?name="+mcpContainer, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusConflict {
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("create mcp: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func containerNamedExists(name string) (bool, error) {
	req, err := http.NewRequest(http.MethodGet, dockerAPIBase+"/containers/"+url.QueryEscape(name)+"/json", nil)
	if err != nil {
		return false, err
	}
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("docker inspect %s: %s", name, strings.TrimSpace(string(body)))
	}
	return true, nil
}

func containerNamedRunning(name string) (bool, error) {
	req, err := http.NewRequest(http.MethodGet, dockerAPIBase+"/containers/"+url.QueryEscape(name)+"/json", nil)
	if err != nil {
		return false, err
	}
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode >= 400 {
		return false, fmt.Errorf("docker inspect: %s", name)
	}
	var info struct {
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return false, err
	}
	return info.State.Running, nil
}

func dockerStartIfStopped(name string) error {
	running, err := containerNamedRunning(name)
	if err != nil {
		return err
	}
	if running {
		return nil
	}
	log.Printf("mcp: starting %s", name)
	return dockerStart(name)
}

// MCPURL is the streamable-HTTP endpoint of the MCP sidecar.
func MCPURL() string {
	return "http://" + mcpContainer + ":8931/mcp"
}

// EnsureDataDirs prepares the host volumes for the browser and MCP sidecar:
// both containers run as uid 1000 (browser / node) and must be able to write
// their profile/output dirs. The API runs as root, so it can fix ownership.
func EnsureDataDirs() {
	dirs := []string{browserProfileHost, "/app/data/browser-mcp"}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Println("browser: mkdir", d, ":", err)
			continue
		}
		if err := os.Chown(d, 1000, 1000); err != nil {
			log.Println("browser: chown", d, ":", err)
			continue
		}
		// Fix ownership of existing contents (previously root-created files).
		if entries, err := os.ReadDir(d); err == nil {
			for _, e := range entries {
				_ = os.Chown(d+"/"+e.Name(), 1000, 1000)
			}
		}
	}
}

// ─── Human / agent handoff ────────────────────────────────────────────────
// When the human sends input through the dashboard (MJPEG view), the agent's
// browser tools briefly yield so the two don't fight over the same window.

var humanActiveUntil atomic.Int64

// TouchHuman records human interaction on the shared browser.
func TouchHuman() {
	humanActiveUntil.Store(time.Now().Add(15 * time.Second).UnixMilli())
}

// WaitHumanIdle blocks until the human interaction window expires (bounded).
func WaitHumanIdle(ctx context.Context) {
	until := humanActiveUntil.Load()
	for {
		remaining := until - time.Now().UnixMilli()
		if remaining <= 0 {
			return
		}
		if remaining > 20000 {
			remaining = 20000
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(remaining) * time.Millisecond):
			until = humanActiveUntil.Load()
		}
	}
}
