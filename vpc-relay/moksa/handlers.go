package moksa

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

// CreateHandler — POST /api/web/mission
// Body: { "instruction": "..." } or { "instruction": "...", "quests": [...] } for L1 later
func CreateHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Instruction string  `json:"instruction"`
		Quests      []Quest `json:"quests"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.Instruction) == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing instruction"})
		return
	}

	m := CreateMissionFromInstruction(req.Instruction)
	if len(req.Quests) > 0 {
		updated, err := UpdateMission(m.ID, func(miss *Mission) error {
			for i := range req.Quests {
				if req.Quests[i].ID == "" {
					req.Quests[i].ID = "q" + itoa(i+1)
				}
				if req.Quests[i].Status == "" {
					req.Quests[i].Status = "pending"
				}
			}
			miss.Quests = req.Quests
			miss.Status = "active"
			return nil
		})
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		m = updated
	}

	sendJSON(w, http.StatusOK, m)
}

// ListHandler — GET /api/web/mission
func ListHandler(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"missions":  ListMissions(),
		"world_card": WorldCard(),
	})
}

// GetHandler — GET /api/web/mission/{id}
func GetHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m, ok := GetMission(id)
	if !ok {
		sendJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	sendJSON(w, http.StatusOK, m)
}

// DeleteHandler — DELETE /api/web/mission/{id}
func DeleteHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !DeleteMission(id) {
		sendJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	sendJSON(w, http.StatusOK, map[string]string{"ok": "deleted"})
}

// ClaimHandler — POST /api/web/mission/{id}/quest/{qid}/claim
func ClaimHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		KarakaID string `json:"karaka_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.KarakaID == "" {
		// default to organ hint
		m, ok := GetMission(r.PathValue("id"))
		if ok {
			if q := m.FindQuest(r.PathValue("qid")); q != nil && q.OrganHint != "" {
				req.KarakaID = q.OrganHint
			}
		}
	}
	m, err := ClaimQuest(r.PathValue("id"), r.PathValue("qid"), req.KarakaID)
	if err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, m)
}

// RunHandler — POST /api/web/mission/{id}/quest/{qid}/run
func RunHandler(w http.ResponseWriter, r *http.Request) {
	m, err := RunQuest(r.PathValue("id"), r.PathValue("qid"))
	if err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, m)
}

// RewardHandler — POST /api/web/mission/{id}/quest/{qid}/reward
func RewardHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Verdict string  `json:"verdict"`
		Score   float64 `json:"score"`
		Reason  string  `json:"reason"`
		AutoAdd *bool   `json:"auto_add"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	autoAdd := true
	if req.AutoAdd != nil {
		autoAdd = *req.AutoAdd
	}
	m, err := ApplyReward(r.PathValue("id"), r.PathValue("qid"), req.Verdict, req.Score, req.Reason, autoAdd)
	if err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, m)
}

// AddQuestHandler — POST /api/web/mission/{id}/quest
func AddQuestHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title     string                 `json:"title"`
		OrganHint string                 `json:"organ_hint"`
		Tool      string                 `json:"tool"`
		Params    map[string]interface{} `json:"params"`
		DependsOn []string               `json:"depends_on"`
		ETA       int                    `json:"eta"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	m, err := AddQuest(r.PathValue("id"), req.Title, req.OrganHint, req.Tool, req.Params, req.DependsOn, req.ETA)
	if err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, m)
}

// SynthesizeHandler — POST /api/web/mission/{id}/synthesize
func SynthesizeHandler(w http.ResponseWriter, r *http.Request) {
	m, err := Synthesize(r.PathValue("id"))
	if err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, m)
}

// WorldCardHandler — GET /api/web/mission/world-card
func WorldCardHandler(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, http.StatusOK, map[string]string{"world_card": WorldCard()})
}
