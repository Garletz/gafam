package suparna

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const llmTimeout = 90 * time.Second

// InterpretDay returns a reading. LLM runs only when useLLM is true (explicit user action).
func InterpretDay(day string, lines []LogLine, useLLM bool) (Reading, error) {
	if useLLM {
		if !ModelDirReady() {
			return Reading{}, fmt.Errorf("model not ready")
		}
		r, err := runLLM(day, lines)
		if err != nil {
			return Reading{}, err
		}
		r.ModelReady = true
		return r, nil
	}
	r := HeuristicReading(day, lines)
	r.ModelReady = ModelDirReady()
	return r, nil
}

func runLLM(day string, lines []LogLine) (Reading, error) {
	script := RunnerScript()
	if _, err := os.Stat(script); err != nil {
		return Reading{}, err
	}
	if !ModelDirReady() {
		return Reading{}, fmt.Errorf("model not ready")
	}

	prompt := buildPrompt(day, lines)
	ctx, cancel := context.WithTimeout(context.Background(), llmTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "python3", script, "--model", ModelDir())
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return Reading{}, fmt.Errorf("llm: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	var reading Reading
	if err := json.Unmarshal(stdout.Bytes(), &reading); err != nil {
		return Reading{}, fmt.Errorf("parse llm json: %w", err)
	}
	reading.Day = day
	reading.Engine = "qwen-onnx"
	if reading.Confidence == "" {
		reading.Confidence = "medium"
	}
	return reading, nil
}

func buildPrompt(day string, lines []LogLine) string {
	var b strings.Builder
	b.WriteString("Analyse these Android relay logs for day ")
	b.WriteString(day)
	b.WriteString(". Output JSON only with keys: summary (French, 3-5 sentences), timeline (array of {time,app,event,severity}), alerts (array), stats ({sms_in,sms_out,errors,sources}), confidence (low|medium|high), log_citations (array of strings).\n\n")
	max := 400
	if len(lines) < max {
		max = len(lines)
	}
	for i := 0; i < max; i++ {
		ln := lines[i]
		b.WriteString(fmt.Sprintf("[%s] %s/%s %s: %s\n", formatTs(ln.Ts), ln.Source, ln.Level, ln.Tag, ln.Message))
	}
	return b.String()
}
