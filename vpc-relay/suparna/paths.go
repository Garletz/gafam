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
	b.WriteString("Tu es Suparna. Analyse ces logs Android GAFAM du jour ")
	b.WriteString(day)
	b.WriteString(".\nRéponds UNIQUEMENT avec un objet JSON valide (pas de markdown), schéma:\n")
	b.WriteString(`{"summary":"…","timeline":[{"time":"HH:MM","app":"…","event":"…","severity":"info|warn|error"}],"alerts":[{"type":"…","detail":"…"}],"stats":{"errors":0},"confidence":"low|medium|high","log_citations":["…"]}`)
	b.WriteString("\nLangue du summary: français. N'invente rien.\n\nLOGS:\n")
	for _, ln := range lines {
		fmt.Fprintf(&b, "%s [%s/%s] %s: %s\n",
			formatTS(ln.Ts), ln.Source, ln.Level, ln.Tag, truncate(ln.Message, 180))
	}
	b.WriteString("\nJSON:")
	return b.String()
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
