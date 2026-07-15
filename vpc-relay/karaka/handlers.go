package karaka

import (
	"encoding/json"
	"net/http"
)

// ToolsListHandler — GET /api/web/karaka/tools
// Retourne la liste de tous les outils enregistrés (sans les handlers).
func ToolsListHandler(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"tools": ListTools(),
	})
}

// ExecuteHandler — POST /api/web/karaka/execute
// Body: { "tool": "sandbox.exec", "params": { "command": "ls -la" } }
// Exécute un outil et retourne le résultat.
func ExecuteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Tool   string                 `json:"tool"`
		Params map[string]interface{} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Tool == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing 'tool' field"})
		return
	}

	result, err := ExecuteTool(req.Tool, req.Params)
	if err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"tool":   req.Tool,
		"result": result,
	})
}

// KarakasListHandler — GET /api/web/karaka/status
// Retourne la liste des kāraka enregistrés.
func KarakasListHandler(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"karakas": ListKarakas(),
	})
}

func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
