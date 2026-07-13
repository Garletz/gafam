package suparna

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func dataRoot() string {
	if os.Getenv("ENV") == "development" {
		return "."
	}
	return "/app/data"
}

func ModelPath() string {
	if p := os.Getenv("QWEN_MODEL_PATH"); p != "" {
		return p
	}
	return filepath.Join(dataRoot(), "qwen", "Qwen3-0.6B-Q4_K_M.gguf")
}

func ModelOnDisk() bool {
	st, err := os.Stat(ModelPath())
	return err == nil && st.Size() > 1<<20
}

func readingsDir() string {
	return filepath.Join(dataRoot(), "suparna", "readings")
}

func readingPath(day string, llm bool) string {
	name := day + ".json"
	if llm {
		name = day + ".qwen.json"
	}
	return filepath.Join(readingsDir(), name)
}

// llama.cpp -c 2048 on 1 Go VPS: ~512 reserved for output, rest for prompt.
const (
	qwenContextTokens = 2048
	qwenPredictTokens = 512
	promptOverheadTok = 320 // instructions + JSON schema
	maxLogLineChars   = 96
)

func maxPromptTokens() int {
	return qwenContextTokens - qwenPredictTokens - promptOverheadTok
}

func estimateTokens(s string) int {
	// Conservative for Qwen tokenizer on mixed log text.
	return (len(s)*2 + 5) / 6
}

func sampleLines(lines []LogLine, max int) []LogLine {
	if len(lines) <= max {
		return lines
	}
	// Prefer errors/warnings + tail of day
	var hot []LogLine
	for _, ln := range lines {
		if ln.Level == "E" || ln.Level == "W" || strings.EqualFold(ln.Tag, "sms") {
			hot = append(hot, ln)
		}
	}
	if len(hot) > max/2 {
		hot = hot[len(hot)-max/2:]
	}
	tailN := max - len(hot)
	if tailN < 0 {
		tailN = 0
	}
	start := len(lines) - tailN
	if start < 0 {
		start = 0
	}
	tail := lines[start:]
	out := append(hot, tail...)
	if len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

func formatPrompt(day string, lines []LogLine) string {
	var b strings.Builder
	b.WriteString("Tu es Suparna, analyste de logs Android. Jour: ")
	b.WriteString(day)
	b.WriteString(`.

Consignes:
- Lis les lignes LOGS ci-dessous (faits réels uniquement).
- Réponds avec UN seul objet JSON, sans markdown, sans texte avant/après.
- Champs requis: summary (français, 2-3 phrases), timeline (max 5 entrées), alerts (max 3), stats.errors (nombre), confidence (low|medium|high).
- N'utilise PAS de placeholders comme "…" ou "HH:MM". Utilise les vraies heures et messages des logs.
- Si tu ne sais pas, dis-le dans summary; n'invente pas.

LOGS:
`)
	for _, ln := range lines {
		fmt.Fprintf(&b, "%s [%s/%s] %s: %s\n",
			formatTS(ln.Ts), ln.Source, ln.Level, ln.Tag, truncate(ln.Message, maxLogLineChars))
	}
	b.WriteString("\n{")
	return b.String()
}

// buildPrompt fits log sample into Qwen context (2048 on 1 Go VPS).
func buildPrompt(day string, lines []LogLine) string {
	budget := maxPromptTokens()
	for max := 80; max >= 8; max -= 4 {
		sample := sampleLines(lines, max)
		prompt := formatPrompt(day, sample)
		if estimateTokens(prompt) <= budget {
			return prompt
		}
	}
	return formatPrompt(day, sampleLines(lines, 8))
}

func formatTS(ts int64) string {
	if ts <= 0 {
		return "--:--"
	}
	if ts > 1e12 {
		ts = ts / 1000
	}
	t := time.Unix(ts, 0).UTC()
	return t.Format("15:04")
}
