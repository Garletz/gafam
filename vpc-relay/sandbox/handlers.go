package sandbox

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var mu sync.Mutex

func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func StatusHandler(w http.ResponseWriter, r *http.Request) {
	running, err := containerState()
	if err != nil {
		if strings.Contains(err.Error(), "missing") {
			sendJSON(w, http.StatusOK, map[string]interface{}{
				"running": false,
				"error":   "",
				"message": "not installed yet — Wake will create from gafam-sandbox image",
			})
			return
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"running": false,
			"error":   err.Error(),
		})
		return
	}
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"running": running,
		"error":   "",
	})
}

func WakeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !mu.TryLock() {
		sendJSON(w, http.StatusConflict, map[string]string{"error": "sandbox_busy"})
		return
	}
	defer mu.Unlock()

	running, err := containerState()
	if err != nil && !strings.Contains(err.Error(), "missing") {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if running {
		sendJSON(w, http.StatusOK, map[string]string{"status": "already_running"})
		return
	}

	log.Println("yantrashala: starting gafam-sandbox container")
	if err := startContainer(); err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": "start: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := waitSandboxReady(ctx); err != nil {
		sendJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "ready: " + err.Error()})
		return
	}

	log.Println("yantrashala: sandbox ready")
	sendJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

func StopHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !mu.TryLock() {
		sendJSON(w, http.StatusConflict, map[string]string{"error": "sandbox_busy"})
		return
	}
	defer mu.Unlock()

	running, err := containerState()
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !running {
		sendJSON(w, http.StatusOK, map[string]string{"status": "already_stopped"})
		return
	}

	log.Println("yantrashala: stopping gafam-sandbox container")
	if err := stopContainer(); err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": "stop: " + err.Error()})
		return
	}

	log.Println("yantrashala: sandbox stopped")
	sendJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func ExecHandler(w http.ResponseWriter, r *http.Request) {
	getProxy().ServeHTTP(w, r)
}

func FilesHandler(w http.ResponseWriter, r *http.Request) {
	getProxy().ServeHTTP(w, r)
}

func VpcStorageHandler(w http.ResponseWriter, r *http.Request) {
	volumes := []map[string]interface{}{
		{"name": "sandbox_files", "used_mb": walkDirSize("/root/gafam_data/sandbox/files") / (1024 * 1024), "quota_mb": 512},
		{"name": "sandbox_downloads", "used_mb": walkDirSize("/root/gafam_data/sandbox/downloads") / (1024 * 1024), "quota_mb": 1024},
		{"name": "sandbox_screenshots", "used_mb": walkDirSize("/root/gafam_data/sandbox/screenshots") / (1024 * 1024), "quota_mb": 256},
		{"name": "gafam_data", "used_mb": walkDirSize("/root/gafam_data") / (1024 * 1024), "quota_mb": 2048},
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"volumes": volumes,
	})
}

func walkDirSize(path string) int64 {
	var total int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}
