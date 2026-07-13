package edge

import (
	"encoding/json"
	"net/http"
)

// ApkSyncHandler POST /api/auth/edge/sync — APK reports state, receives pending command.
func ApkSyncHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req SyncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		if req.EdgeService == "" {
			req.EdgeService = "idle"
		}
		UpdateApkReport(req)
		sendJSON(w, http.StatusOK, TakeSyncResponse())
	}
}
