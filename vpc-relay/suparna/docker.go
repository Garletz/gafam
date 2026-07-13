package suparna

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	dockerSock     = "/var/run/docker.sock"
	qwenContainer  = "gafam-qwen"
	dockerAPIBase  = "http://localhost"
)

func dockerHTTP() *http.Client {
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", dockerSock)
			},
		},
	}
}

func dockerSockPresent() bool {
	st, err := os.Stat(dockerSock)
	return err == nil && (st.Mode()&os.ModeSocket != 0 || st.Mode().IsRegular())
}

func containerState() (running bool, err error) {
	if !dockerSockPresent() {
		return false, fmt.Errorf("docker.sock unavailable — mount /var/run/docker.sock on gafam-api")
	}
	req, err := http.NewRequest(http.MethodGet, dockerAPIBase+"/containers/"+qwenContainer+"/json", nil)
	if err != nil {
		return false, err
	}
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return false, fmt.Errorf("container %s missing — run vpc-relay/scripts/qwen-install.sh", qwenContainer)
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

func startContainer() error {
	req, err := http.NewRequest(http.MethodPost, dockerAPIBase+"/containers/"+qwenContainer+"/start", nil)
	if err != nil {
		return err
	}
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	// 204 No Content = ok; 304 = already started
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("docker start: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func stopContainer() error {
	req, err := http.NewRequest(http.MethodPost, dockerAPIBase+"/containers/"+qwenContainer+"/stop?t=15", nil)
	if err != nil {
		return err
	}
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("docker stop: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func qwenBaseURL() string {
	if u := os.Getenv("QWEN_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://gafam-qwen:8080"
}

func waitQwenReady(ctx context.Context) error {
	client := &http.Client{Timeout: 3 * time.Second}
	url := qwenBaseURL() + "/health"
	fallback := qwenBaseURL() + "/completion"
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			// If 503, model is still loading. Wait and retry.
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("qwen not ready: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func complete(ctx context.Context, prompt string, nPredict int) (string, error) {
	payload := map[string]interface{}{
		"prompt":      prompt,
		"n_predict":   nPredict,
		"temperature": 0.3,
		"top_p":       0.9,
		"stop":        []string{"```", "\n\n\n"},
		"stream":      false,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, qwenBaseURL()+"/completion", strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("qwen completion %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var out struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("parse completion: %w", err)
	}
	return out.Content, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
