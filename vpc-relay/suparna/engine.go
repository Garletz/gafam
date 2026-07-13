package suparna

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	analyzeMu sync.Mutex
	jsonObjRe = regexp.MustCompile(`\{[\s\S]*\}`)
)

// HeavyBusy reports whether a remote-control session is active (caller supplies).
type HeavyBusyFunc func() bool

func AnalyzeDay(day string, lines []LogLine, force bool, heavyBusy HeavyBusyFunc) (*Reading, error) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := waitQwenReady(ctx); err != nil {
		return nil, err
	}

	sample := sampleLines(lines, 400)
	prompt := formatPrompt(day, sample)
	raw, err := complete(ctx, prompt, 512)
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
	m := jsonObjRe.FindString(content)
	if m == "" {
		return nil, fmt.Errorf("no json in model output: %s", truncate(content, 200))
	}
	var r Reading
	if err := json.Unmarshal([]byte(m), &r); err != nil {
		// try to fix trailing commas lightly
		clean := strings.TrimSpace(m)
		if err2 := json.Unmarshal([]byte(clean), &r); err2 != nil {
			return nil, fmt.Errorf("invalid json from model: %w", err)
		}
	}
	r.Day = day
	if r.Summary == "" {
		r.Summary = "(empty summary)"
	}
	return &r, nil
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
	return map[string]interface{}{
		"model_on_disk":    ModelOnDisk(),
		"model_path":       ModelPath(),
		"model_ready":      ModelOnDisk(),
		"qwen_running":     running,
		"qwen_url":         qwenBaseURL(),
		"docker_error":     dockerErr,
		"passive":          true,
		"auto_stop":        true,
	}
}
