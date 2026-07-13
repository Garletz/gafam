package suparna

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	analyzeMu sync.Mutex
)

// HeavyBusy reports whether a remote-control session is active (caller supplies).
type HeavyBusyFunc func() bool

func analyzeDaySync(day string, lines []LogLine, force bool, heavyBusy HeavyBusyFunc) (*Reading, error) {
	if !analyzeMu.TryLock() {
		return nil, fmt.Errorf("analysis_in_progress")
	}
	defer analyzeMu.Unlock()

	if !force {
		if cached := loadReading(day, true); cached != nil {
			return cached, nil
		}
	}

	if heavyBusy != nil && heavyBusy() {
		return nil, fmt.Errorf("heavy_job_busy: stop scrcpy/remote session before analyzing")
	}
	if !ModelOnDisk() {
		return nil, fmt.Errorf("model_missing: download GGUF via vpc-relay/scripts/qwen-install.sh")
	}

	// Wake sidecar → RAM load → analyze → always stop (disk-only again)
	startedByUs := false
	running, err := containerState()
	if err != nil {
		return nil, err
	}
	if !running {
		log.Println("suparna: starting gafam-qwen (load model into RAM)")
		if err := startContainer(); err != nil {
			return nil, fmt.Errorf("wake qwen: %w", err)
		}
		startedByUs = true
	}
	defer func() {
		log.Println("suparna: stopping gafam-qwen (unload from RAM)")
		if err := stopContainer(); err != nil {
			log.Println("suparna: stop:", err)
		}
		_ = startedByUs
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := waitQwenReady(ctx); err != nil {
		return nil, err
	}

	prompt := buildPrompt(day, lines)
	raw, err := complete(ctx, prompt, 256)
	if err != nil {
		return nil, err
	}

	reading, err := parseReading(day, raw)
	if err != nil {
		return nil, err
	}
	reading.Engine = "qwen-gguf"
	reading.ModelReady = true
	reading.GeneratedAt = nowISO()
	_ = saveReading(day, true, reading)
	return reading, nil
}

func parseReading(day, content string) (*Reading, error) {
	raw := strings.TrimSpace(content)
	// Strip optional markdown fence
	if strings.HasPrefix(raw, "```") {
		if i := strings.Index(raw[3:], "```"); i >= 0 {
			inner := raw[3 : 3+i]
			if j := strings.Index(inner, "\n"); j >= 0 {
				inner = inner[j+1:]
			}
			raw = strings.TrimSpace(inner)
		}
	}

	m := extractJSONObject(raw)
	if m == "" {
		return nil, fmt.Errorf("no json in model output: %s", truncate(content, 200))
	}
	var r Reading
	if err := json.Unmarshal([]byte(m), &r); err != nil {
		return nil, fmt.Errorf("invalid json from model: %w (snippet: %s)", err, truncate(m, 120))
	}
	r.Day = day
	if r.Summary == "" {
		r.Summary = "(empty summary)"
	}
	return &r, nil
}

// extractJSONObject returns the first balanced {...} object (stops before trailing LOGS:/Langue/etc.).
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if inString {
			if c == '\\' {
				escape = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func loadReading(day string, llm bool) *Reading {
	b, err := os.ReadFile(readingPath(day, llm))
	if err != nil {
		return nil
	}
	var r Reading
	if json.Unmarshal(b, &r) != nil {
		return nil
	}
	return &r
}

func saveReading(day string, llm bool, r *Reading) error {
	if err := os.MkdirAll(readingsDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(readingPath(day, llm), mustJSON(r), 0o644)
}

func Status() map[string]interface{} {
	running := false
	var dockerErr string
	if dockerSockPresent() {
		r, err := containerState()
		running = r
		if err != nil {
			dockerErr = err.Error()
		}
	} else {
		dockerErr = "docker.sock not mounted"
	}

	jobsMu.Lock()
	analyzeRunning := false
	for _, j := range jobs {
		if j.Running {
			analyzeRunning = true
			break
		}
	}
	jobsMu.Unlock()

	return map[string]interface{}{
		"model_on_disk":    ModelOnDisk(),
		"model_path":       ModelPath(),
		"model_ready":      ModelOnDisk(),
		"qwen_running":     running,
		"qwen_url":         qwenBaseURL(),
		"docker_error":     dockerErr,
		"passive":          true,
		"auto_stop":        true,
		"analyze_running":  analyzeRunning,
	}
}
