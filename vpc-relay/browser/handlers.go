package browser

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
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
	running := false
	var dockerErr string
	if dockerSockPresent() {
		r, err := containerState()
		running = r
		if err != nil {
			dockerErr = err.Error()
		}
	} else {
		dockerErr = "docker.sock not mounted"
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"running":      running,
		"docker_error": dockerErr,
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
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if running {
		sendJSON(w, http.StatusOK, map[string]string{"status": "already_running"})
		return
	}

	log.Println("vatayana: starting gafam-browser container")
	if err := startContainer(); err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": "start: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := waitBrowserReady(ctx); err != nil {
		sendJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "ready: " + err.Error()})
		return
	}

	log.Println("vatayana: browser ready")
	sendJSON(w, http.StatusOK, map[string]string{"status": "started"})
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
