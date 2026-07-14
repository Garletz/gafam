package browser

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	mu sync.Mutex
)

func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func StatusHandler(w http.ResponseWriter, r *http.Request) {
	running, err := containerState()
	if err != nil {
		// Not an error if missing — Wake will pull+create from GHCR.
		if strings.Contains(err.Error(), "missing") {
			sendJSON(w, http.StatusOK, map[string]interface{}{
				"running":      false,
				"docker_error": "",
				"browser_url":  browserBaseURL(),
				"message":      "not installed yet — Wake pulls ghcr.io/garletz/gafam:browser",
			})
			return
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"running":      false,
			"docker_error": err.Error(),
			"browser_url":  browserBaseURL(),
		})
		return
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"running":      running,
		"docker_error": "",
		"browser_url":  browserBaseURL(),
	})
}

func WakeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !mu.TryLock() {
		sendJSON(w, http.StatusConflict, map[string]string{"error": "browser_busy: another operation in progress"})
		return
	}
	defer mu.Unlock()

	running, err := containerState()
	if err != nil && !strings.Contains(err.Error(), "missing") {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Old noVNC containers report Running but have no /status — force recreate.
	if running {
		if streamBackendReady() {
			sendJSON(w, http.StatusOK, map[string]string{"status": "already_running"})
			return
		}
		log.Println("vatayana: running container missing /status (stale image) — recreating from GHCR")
	} else {
		log.Println("vatayana: ensuring/pulling gafam-browser from GHCR then starting")
	}

	if err := startContainer(); err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": "start: " + err.Error()})
		return
	}

	// First pull of Firefox image can take several minutes on a small VPS.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if err := waitBrowserReady(ctx); err != nil {
		sendJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "ready: " + err.Error()})
		return
	}

	log.Println("vatayana: browser ready")
	sendJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

func streamBackendReady() bool {
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequest(http.MethodGet, browserBaseURL()+"/status", nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func StopHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !mu.TryLock() {
		sendJSON(w, http.StatusConflict, map[string]string{"error": "browser_busy: another operation in progress"})
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

	log.Println("vatayana: stopping gafam-browser container")
	if err := stopContainer(); err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": "stop: " + err.Error()})
		return
	}

	log.Println("vatayana: browser stopped")
	sendJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}
