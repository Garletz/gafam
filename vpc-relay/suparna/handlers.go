package suparna

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

var (
	downloadMu     sync.Mutex
	downloadStatus = map[string]interface{}{
		"status": "idle",
	}
)

func StatusHandler(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"model_ready":    ModelDirReady(),
		"model_dir":      ModelDir(),
		"runner":         RunnerScript(),
		"download":       downloadStatus,
	})
}

func DownloadModelHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !downloadMu.TryLock() {
		sendJSON(w, http.StatusConflict, map[string]interface{}{
			"error":    "download_in_progress",
			"download": downloadStatus,
		})
		return
	}

	downloadStatus = map[string]interface{}{
		"status": "downloading",
		"step":   "starting",
	}
	go func() {
		defer downloadMu.Unlock()
		err := downloadModel()
		if err != nil {
			log.Println("suparna download:", err)
			downloadStatus = map[string]interface{}{
				"status": "error",
				"error":  err.Error(),
			}
			return
		}
		downloadStatus = map[string]interface{}{
			"status": "ready",
		}
	}()

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "started",
		"message": "Suparna model download started. This may take several minutes.",
	})
}

func downloadModel() error {
	dir := ModelDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	downloadStatus = map[string]interface{}{
		"status": "downloading",
		"step":   "onnxruntime_genai builder",
	}

	// Build int4 CPU model from HuggingFace (requires network on VPS)
	cmd := exec.Command(
		"python3", "-m", "onnxruntime_genai.models.builder",
		"-m", "Qwen/Qwen3-0.6B",
		"-o", dir,
		"-e", "cpu",
		"-p", "int4",
	)
	cmd.Env = append(os.Environ(), "HF_HUB_ENABLE_HF_TRANSFER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: try huggingface snapshot with pre-exported genai bundle if exists
		log.Printf("builder failed: %v\n%s", err, string(out))
		return fmt.Errorf("model build failed: %v", err)
	}
	return nil
}

func ReadDayHandler(w http.ResponseWriter, r *http.Request, readDay func(day string, offset, limit int) ([]LogLine, int, error)) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	day := r.URL.Query().Get("day")
	if day == "" {
		http.Error(w, "Missing day", http.StatusBadRequest)
		return
	}
	force := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("llm") == "1"

	// Cached reading unless refresh
	if !force {
		if cached, ok := loadCachedReading(day); ok {
			sendJSON(w, http.StatusOK, cached)
			return
		}
	}

	lines, _, err := readDay(day, 0, 2000)
	if err != nil {
		http.Error(w, "Failed to read logs", http.StatusBadRequest)
		return
	}

	useLLM := force && ModelDirReady()
	reading := InterpretDay(day, lines, useLLM)
	_ = saveCachedReading(day, reading)

	sendJSON(w, http.StatusOK, reading)
}

func ScanSmsHandler(w http.ResponseWriter, r *http.Request, querySms func(limit int) ([]map[string]interface{}, error)) {
	limit := 80
	rows, err := querySms(limit)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	type hit struct {
		ID        interface{} `json:"id"`
		Sender    string      `json:"sender"`
		Body      string      `json:"body"`
		Timestamp int64       `json:"timestamp"`
		Codes     []string    `json:"codes"`
	}

	var results []hit
	for _, row := range rows {
		dir, _ := row["direction"].(string)
		if dir == "outbound" {
			continue
		}
		body, _ := row["body"].(string)
		codes := DetectCodes(body)
		if len(codes) == 0 {
			continue
		}
		ts, _ := row["timestamp"].(int64)
		if ts == 0 {
			if f, ok := row["timestamp"].(float64); ok {
				ts = int64(f)
			}
		}
		results = append(results, hit{
			ID:        row["id"],
			Sender:    fmt.Sprint(row["sender"]),
			Body:      body,
			Timestamp: ts,
			Codes:     codes,
		})
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"hits":  results,
		"count": len(results),
	})
}

func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		http.Error(w, "Failed to marshal JSON", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(jsonData)
}

func loadCachedReading(day string) (Reading, bool) {
	path := filepath.Join(dataRoot(), "suparna", "readings", day+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		return Reading{}, false
	}
	var r Reading
	if err := json.Unmarshal(b, &r); err != nil {
		return Reading{}, false
	}
	return r, true
}

func saveCachedReading(day string, r Reading) error {
	dir := filepath.Join(dataRoot(), "suparna", "readings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, day+".json"), b, 0o644)
}

// Unused import guard
var _ = io.Discard
