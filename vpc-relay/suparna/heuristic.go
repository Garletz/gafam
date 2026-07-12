package suparna

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type LogLine struct {
	Ts      int64
	Source  string
	Level   string
	Tag     string
	Message string
}

type Reading struct {
	Day           string                   `json:"day"`
	Summary       string                   `json:"summary"`
	Timeline      []map[string]interface{} `json:"timeline"`
	Alerts        []map[string]interface{} `json:"alerts"`
	Stats         map[string]interface{}   `json:"stats"`
	Confidence    string                   `json:"confidence"`
	LogCitations  []string                 `json:"log_citations"`
	Engine        string                   `json:"engine"`
	ModelReady    bool                     `json:"model_ready"`
}

func HeuristicReading(day string, lines []LogLine) Reading {
	stats := map[string]interface{}{
		"sms_in": 0, "sms_out": 0, "errors": 0, "warnings": 0,
		"sources": []string{},
	}
	srcSet := map[string]struct{}{}
	var timeline []map[string]interface{}
	var alerts []map[string]interface{}
	var citations []string
	var parts []string

	for _, ln := range lines {
		srcSet[ln.Source] = struct{}{}
		msg := strings.TrimSpace(ln.Message)
		lower := strings.ToLower(msg)

		switch ln.Level {
		case "E":
			stats["errors"] = stats["errors"].(int) + 1
		case "W":
			stats["warnings"] = stats["warnings"].(int) + 1
		}

		if ln.Tag == "sms" && strings.Contains(lower, "received") {
			stats["sms_in"] = stats["sms_in"].(int) + 1
		}
		if ln.Tag == "outbox" && strings.Contains(lower, "sent sms") {
			stats["sms_out"] = stats["sms_out"].(int) + 1
		}

		if ln.Level == "E" || ln.Level == "W" || ln.Tag == "sms" || ln.Tag == "challenge" || ln.Tag == "pair" {
			t := formatTs(ln.Ts)
			timeline = append(timeline, map[string]interface{}{
				"time":     t,
				"app":      ln.Tag,
				"event":    truncate(msg, 120),
				"severity": levelSeverity(ln.Level),
			})
			if len(citations) < 8 {
				citations = append(citations, fmt.Sprintf("%s %s %s", t, ln.Tag, truncate(msg, 80)))
			}
		}

		if strings.Contains(lower, "urgence_gafam") || strings.Contains(lower, "recovery") {
			alerts = append(alerts, map[string]interface{}{
				"type":   "recovery_keyword",
				"detail": truncate(msg, 100),
			})
		}
		if ln.Level == "E" {
			alerts = append(alerts, map[string]interface{}{
				"type":   "error",
				"detail": fmt.Sprintf("[%s] %s", ln.Tag, truncate(msg, 80)),
			})
		}
		if codes := DetectCodes(msg); len(codes) > 0 && (ln.Tag == "sms" || strings.Contains(lower, "code")) {
			alerts = append(alerts, map[string]interface{}{
				"type":   "verification_code",
				"detail": "Code detected: " + strings.Join(codes, ", "),
				"codes":  codes,
			})
		}
	}

	var sources []string
	for s := range srcSet {
		sources = append(sources, s)
	}
	sort.Strings(sources)
	stats["sources"] = sources

	if len(lines) == 0 {
		parts = append(parts, fmt.Sprintf("No log entries for %s.", day))
	} else {
		parts = append(parts, fmt.Sprintf("%d log lines on %s.", len(lines), day))
		if stats["sms_in"].(int) > 0 {
			parts = append(parts, fmt.Sprintf("%d incoming SMS events.", stats["sms_in"].(int)))
		}
		if stats["sms_out"].(int) > 0 {
			parts = append(parts, fmt.Sprintf("%d outbound SMS events.", stats["sms_out"].(int)))
		}
		if stats["errors"].(int) > 0 {
			parts = append(parts, fmt.Sprintf("%d errors — check alerts.", stats["errors"].(int)))
		} else if stats["warnings"].(int) > 0 {
			parts = append(parts, fmt.Sprintf("%d warnings.", stats["warnings"].(int)))
		}
	}

	if len(timeline) > 12 {
		timeline = timeline[:12]
	}
	if len(alerts) > 6 {
		alerts = alerts[:6]
	}

	conf := "medium"
	if len(lines) < 5 {
		conf = "low"
	}
	if stats["errors"].(int) == 0 && len(lines) > 20 {
		conf = "high"
	}

	return Reading{
		Day:          day,
		Summary:      strings.Join(parts, " "),
		Timeline:     timeline,
		Alerts:       alerts,
		Stats:        stats,
		Confidence:   conf,
		LogCitations: citations,
		Engine:       "heuristic",
		ModelReady:   ModelDirReady(),
	}
}

func formatTs(ts int64) string {
	if ts <= 0 {
		return "??:??"
	}
	return time.UnixMilli(ts).Format("15:04")
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func levelSeverity(level string) string {
	switch level {
	case "E":
		return "error"
	case "W":
		return "warning"
	default:
		return "info"
	}
}
