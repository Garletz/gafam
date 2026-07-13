package suparna

import (
	"encoding/json"
	"net/http"
)

func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func StatusHandler(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, http.StatusOK, Status())
}

// ReadingHandler GET ?day= — poll async analysis result (fast, Cloudflare-safe).
func ReadingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	day := r.URL.Query().Get("day")
	if day == "" {
		http.Error(w, "Missing day", http.StatusBadRequest)
		return
	}
	sendJSON(w, http.StatusOK, ReadingJob(day))
}

// ReadDayHandler POST ?day=, refresh=0|1 — starts analysis, returns immediately.
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

	if heavyBusy != nil && heavyBusy() {
		sendJSON(w, http.StatusConflict, map[string]string{"error": "heavy_job_busy: stop scrcpy/remote session before analyzing"})
		return
	}
	if !ModelOnDisk() {
		sendJSON(w, http.StatusPreconditionFailed, map[string]string{"error": "model_missing: download GGUF via vpc-relay/scripts/qwen-install.sh"})
		return
	}

	reading, status, err := StartAnalyzeDay(day, lines, force, heavyBusy)
	if err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if reading != nil {
		sendJSON(w, http.StatusOK, reading)
		return
	}
	sendJSON(w, http.StatusAccepted, map[string]string{"status": status})
}
