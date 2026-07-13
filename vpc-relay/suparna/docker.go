package suparna

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	if resp.StatusCode != http.StatusOK {
		return false
	}
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
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	if lastErr != nil && !isQwenLoadingErr(lastErr) {
		log.Printf("suparna: first attempt failed (%v), retrying without grammar", lastErr)
		content, err := completeOnceNoGrammar(ctx, prompt, nPredict)
		if err == nil {
			return content, nil
		}
		lastErr = err
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

const suparnaGBNF = `root ::= "{" ws members ws "}"
members ::= pair ("," ws pair)*
pair ::= ws string ws ":" ws value
string ::= "\"" chars "\""
chars ::= char*
char ::= [^"\\] | "\\" escape
escape ::= ["\\nrt/]
value ::= string | number | object | array | "true" | "false" | "null"
object ::= "{" ws (pair ("," ws pair)*)? ws "}"
array ::= "[" ws (value ("," ws value)*)? ws "]"
number ::= "-"? [0-9]+ ("." [0-9]+)?
ws ::= [ \t\n]*
`

func completeOnce(ctx context.Context, prompt string, nPredict int) (string, error) {
	payload := map[string]interface{}{
		"prompt":            prompt,
		"n_predict":         nPredict,
		"temperature":       0.5,
		"top_p":             0.9,
		"top_k":             40,
		"repeat_penalty":    1.3,
		"frequency_penalty": 0.5,
		"presence_penalty":  0.3,
		"stop":              []string{"```", "\n\n\n", "\nLOGS:", "\nLangue"},
		"stream":            false,
		"grammar":           suparnaGBNF,
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
	if isDegenerate(out.Content) {
		return "", fmt.Errorf("degenerate output detected (repetition loop)")
	}
	return out.Content, nil
}

func isDegenerate(s string) bool {
	words := strings.Fields(s)
	if len(words) < 20 {
		return false
	}
	ngrams := map[string]int{}
	for i := 0; i+2 < len(words); i++ {
		key := words[i] + " " + words[i+1] + " " + words[i+2]
		ngrams[key]++
		if ngrams[key] >= 5 {
			return true
		}
	}
	return false
}

func completeOnceNoGrammar(ctx context.Context, prompt string, nPredict int) (string, error) {
	payload := map[string]interface{}{
		"prompt":            prompt,
		"n_predict":         nPredict,
		"temperature":       0.7,
		"top_p":             0.85,
		"top_k":             30,
		"repeat_penalty":    1.5,
		"frequency_penalty": 0.7,
		"presence_penalty":  0.5,
		"stop":              []string{"```", "\n\n\n", "\nLOGS:", "\nLangue"},
		"stream":            false,
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
		return "", fmt.Errorf("qwen completion (no-grammar) %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var out struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("parse completion (no-grammar): %w", err)
	}
	if isDegenerate(out.Content) {
		return "", fmt.Errorf("degenerate output on retry (model too weak for this input)")
	}
	return out.Content, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
