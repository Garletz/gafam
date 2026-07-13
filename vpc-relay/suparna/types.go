package suparna

import (
	"encoding/json"
	"time"
)

type LogLine struct {
	Ts      int64  `json:"ts"`
	Source  string `json:"source"`
	Level   string `json:"level"`
	Tag     string `json:"tag"`
	Message string `json:"message"`
}

type TimelineItem struct {
	Time     string `json:"time"`
	App      string `json:"app"`
	Event    string `json:"event"`
	Severity string `json:"severity"`
}

type Alert struct {
	Type   string   `json:"type"`
	Detail string   `json:"detail"`
	Codes  []string `json:"codes,omitempty"`
}

type Reading struct {
	Day          string                 `json:"day"`
	Summary      string                 `json:"summary"`
	Timeline     []TimelineItem         `json:"timeline,omitempty"`
	Alerts       []Alert                `json:"alerts,omitempty"`
	Stats        map[string]interface{} `json:"stats,omitempty"`
	Confidence   string                 `json:"confidence,omitempty"`
	LogCitations []string               `json:"log_citations,omitempty"`
	Engine       string                 `json:"engine"`
	ModelReady   bool                   `json:"model_ready"`
	GeneratedAt  string                 `json:"generated_at,omitempty"`
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func mustJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
