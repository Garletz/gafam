package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"
)

// Set at build time via -ldflags
var (
	Version   = "dev"
	GitSHA    = "unknown"
	BuildTime = "unknown"
)

var serverStartedAt = time.Now()

func vpcInfoHandler(w http.ResponseWriter, r *http.Request) {
	uptime := int64(time.Since(serverStartedAt).Seconds())
	shortSHA := GitSHA
	if len(shortSHA) > 7 && shortSHA != "unknown" {
		shortSHA = shortSHA[:7]
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"status":          "online",
		"version":         Version,
		"git_sha":         GitSHA,
		"git_sha_short":   shortSHA,
		"build_time":      BuildTime,
		"image":           "ghcr.io/garletz/gafam:latest",
		"uptime_seconds":  uptime,
		"started_at":      serverStartedAt.UTC().Format(time.RFC3339),
		"watchtower":      checkWatchtowerReachable(),
	})
}

func checkWatchtowerReachable() bool {
	token := os.Getenv("WATCHTOWER_TOKEN")
	if token == "" {
		return false
	}
	watchtowerURL := os.Getenv("WATCHTOWER_URL")
	if watchtowerURL == "" {
		watchtowerURL = "http://host.docker.internal:8080/v1/update"
	}
	// HEAD/GET may not be supported; a short POST with auth validates reachability.
	req, err := http.NewRequest(http.MethodPost, watchtowerURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	// Any HTTP response means Watchtower is reachable (401/403 still counts).
	return resp.StatusCode > 0
}

func triggerUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := os.Getenv("WATCHTOWER_TOKEN")
	if token == "" {
		sendJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":   "watchtower_not_configured",
			"message": "Manual update requires Watchtower HTTP API on this node.",
		})
		return
	}

	watchtowerURL := os.Getenv("WATCHTOWER_URL")
	if watchtowerURL == "" {
		watchtowerURL = "http://watchtower:8080/v1/update"
	}

	req, err := http.NewRequest(http.MethodPost, watchtowerURL, nil)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{
			"error":   "watchtower_unreachable",
			"message": err.Error(),
		})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		sendJSON(w, resp.StatusCode, map[string]string{
			"error":   "watchtower_error",
			"message": string(body),
		})
		return
	}

	var wtResp map[string]interface{}
	if err := json.Unmarshal(body, &wtResp); err != nil {
		wtResp = map[string]interface{}{"raw": string(body)}
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "update_triggered",
		"message": "Watchtower is pulling the latest image. The VPC will restart shortly.",
		"details": wtResp,
	})
}
