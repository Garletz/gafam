package suparna

import (
	"sync"
	"time"
)

type jobState struct {
	Running   bool
	Done      bool
	Error     string
	Reading   *Reading
	StartedAt time.Time
}

var (
	jobsMu sync.Mutex
	jobs   = map[string]*jobState{}
)

// StartAnalyzeDay kicks off analysis in the background (for long-running Qwen on 1 Go VPS).
// Returns cached reading, or nil + status "started"|"running".
func StartAnalyzeDay(day string, lines []LogLine, force bool, heavyBusy HeavyBusyFunc) (*Reading, string, error) {
	if !force {
		if cached := loadReading(day, true); cached != nil {
			return cached, "cached", nil
		}
	}

	jobsMu.Lock()
	if j, ok := jobs[day]; ok && j.Running {
		jobsMu.Unlock()
		return nil, "running", nil
	}
	jobs[day] = &jobState{Running: true, StartedAt: time.Now()}
	jobsMu.Unlock()

	go func() {
		reading, err := analyzeDaySync(day, lines, force, heavyBusy)
		jobsMu.Lock()
		defer jobsMu.Unlock()
		j := jobs[day]
		if j == nil {
			return
		}
		j.Running = false
		j.Done = true
		if err != nil {
			j.Error = err.Error()
		} else {
			j.Reading = reading
		}
	}()

	return nil, "started", nil
}

func ReadingJob(day string) map[string]interface{} {
	jobsMu.Lock()
	j := jobs[day]
	jobsMu.Unlock()

	if j == nil {
		if cached := loadReading(day, true); cached != nil {
			return map[string]interface{}{
				"status":  "done",
				"reading": cached,
			}
		}
		return map[string]interface{}{"status": "idle"}
	}

	if j.Running {
		return map[string]interface{}{
			"status":     "running",
			"started_at": j.StartedAt.UTC().Format(time.RFC3339),
		}
	}
	if j.Error != "" {
		return map[string]interface{}{
			"status": "error",
			"error":  j.Error,
		}
	}
	return map[string]interface{}{
		"status":  "done",
		"reading": j.Reading,
	}
}
