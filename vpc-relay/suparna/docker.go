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
	dockerSock    = "/var/run/docker.sock"
	qwenContainer = "gafam-qwen"
	dockerAPIBase = "http://localhost"
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
	return err == nil && st.Mode()&os.ModeSocket != 0
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
		return false, fmt.Errorf("container %s missing — run deploy-vpc.sh or qwen-install.sh", qwenContainer)
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

// waitQwenReady blocks until llama.cpp reports the model is loaded (not just the HTTP server up).
func waitQwenReady(ctx context.Context) error {
	client := &http.Client{Timeout: 8 * time.Second}
	healthURL := qwenBaseURL() + "/health"
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		if qwenHealthOK(ctx, client, healthURL) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("qwen model load timeout (1 Go VPS can take 2–3 min): %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func qwenHealthOK(ctx context.Context, client *http.Client, healthURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	// llama.cpp returns 503 {"error":{"message":"Loading model"...}} while weights load into RAM
	if resp.StatusCode != http.StatusOK {
		return false
	}
	// Optional JSON body: {"status":"ok"} or similar
	var probe struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(body, &probe) == nil && probe.Status != "" {
		return strings.EqualFold(probe.Status, "ok")
	}
	return true
}

func complete(ctx context.Context, prompt string, nPredict int) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 40; attempt++ {
		content, err := completeOnce(ctx, prompt, nPredict)
		if err == nil {
			return content, nil
		}
		lastErr = err
		if !isQwenLoadingErr(err) {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return "", lastErr
}

func isQwenLoadingErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "Loading model") ||
		strings.Contains(s, "unavailable_error") ||
		strings.Contains(s, "qwen completion 503")
}

func completeOnce(ctx context.Context, prompt string, nPredict int) (string, error) {
	payload := map[string]interface{}{
		"prompt":      prompt,
		"n_predict":   nPredict,
		"temperature": 0.3,
		"top_p":       0.9,
		"stop":        []string{"```", "\n\n\n", "\nLOGS:", "\nLangue"},
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
