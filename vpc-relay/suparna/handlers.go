package suparna

import (
	"encoding/json"
	"net/http"
	"strings"
)

func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func StatusHandler(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, http.StatusOK, Status())
}

// ReadDayHandler expects query: day=, refresh=0|1
func ReadDayHandler(
	w http.ResponseWriter,
	r *http.Request,
	readDay func(day string, offset, limit int) ([]LogLine, int, error),
	heavyBusy HeavyBusyFunc,
) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	day := r.URL.Query().Get("day")
	if day == "" {
		http.Error(w, "Missing day", http.StatusBadRequest)
		return
	}
	force := r.URL.Query().Get("refresh") == "1"

	lines, _, err := readDay(day, 0, 2000)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(lines) == 0 {
		sendJSON(w, http.StatusOK, &Reading{
			Day:        day,
			Summary:    "Aucun log pour ce jour.",
			Engine:     "none",
			ModelReady: ModelOnDisk(),
			Confidence: "high",
		})
		return
	}

	reading, err := AnalyzeDay(day, lines, force, heavyBusy)
	if err != nil {
		code := http.StatusBadGateway
		msg := err.Error()
		switch {
		case msg == "analysis_in_progress":
			code = http.StatusConflict
		case strings.Contains(msg, "heavy_job_busy"):
			code = http.StatusConflict
		case strings.Contains(msg, "model_missing"):
			code = http.StatusPreconditionFailed
		}
		sendJSON(w, code, map[string]string{"error": msg})
		return
	}
	sendJSON(w, http.StatusOK, reading)
}
