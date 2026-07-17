package main

// Saṃyojaka — the orchestrator loop (Manifest 25).
// Given an instruction: PLAN (active LLM engine → quest plan) → EXECUTE
// (claim + run each quest via Kāraka, respecting dependencies) → SYNTHESIZE
// (Mokṣa report written to the sandbox). One run at a time.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Garletz/gafam/vpc-relay/karaka"
	"github.com/Garletz/gafam/vpc-relay/moksa"
)

var (
	orchestratorMu sync.Mutex

	orchestratorStateMu sync.RWMutex
	orchestratorRunning = false
	orchestratorMission = ""
	orchestratorSince   time.Time
)

func setOrchestratorState(running bool, missionID string) {
	orchestratorStateMu.Lock()
	defer orchestratorStateMu.Unlock()
	orchestratorRunning = running
	orchestratorMission = missionID
	if running {
		orchestratorSince = time.Now()
	}
}

// ─── Planner ───

type plannedQuest struct {
	Title     string                 `json:"title"`
	Tool      string                 `json:"tool"`
	Params    map[string]interface{} `json:"params"`
	DependsOn []int                  `json:"depends_on"` // 1-based indices of previous quests
}

type planResult struct {
	Quests []plannedQuest `json:"quests"`
}

func buildPlannerPrompt(instruction string, maxQuests int) (system, user string) {
	var b strings.Builder
	b.WriteString("You are Saṃyojaka, the orchestrator of a personal sovereign node (GAFAM).\n")
	b.WriteString("You receive a user instruction and output a quest plan as STRICT JSON.\n\n")
	b.WriteString("Available Kāraka tools:\n")
	for _, t := range karaka.ListTools() {
		params, _ := json.Marshal(t.Params)
		fmt.Fprintf(&b, "- %s — %s\n  params: %s\n  returns: %s\n", t.ID, t.Description, string(params), t.Returns)
	}
	b.WriteString("\nRules:\n")
	fmt.Fprintf(&b, "- Output between 1 and %d quests, ordered for sequential execution.\n", maxQuests)
	b.WriteString("- Each quest: {\"title\": \"short action title\", \"tool\": \"<tool id>\", \"params\": {...}, \"depends_on\": [1-based indices of earlier quests]}\n")
	b.WriteString("- depends_on is optional; omit it or use [] when the quest is independent.\n")
	b.WriteString("- Fill params with concrete values (real paths, real URLs from the instruction). No placeholders.\n")
	b.WriteString("- Read before you act: e.g. sandbox.tree or browser.fetch before file/browser mutations.\n")
	b.WriteString("- The final quest should gather what is needed to answer the user (e.g. read the result file, fetch the page).\n")
	b.WriteString("- Output ONLY the JSON object {\"quests\": [...]} — no markdown fences, no commentary.\n")
	return b.String(), "Instruction: " + instruction
}

// extractJSON pulls the first balanced {...} object out of an LLM reply.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func planQuests(ctx context.Context, instruction string, maxQuests int) (*planResult, string, error) {
	system, user := buildPlannerPrompt(instruction, maxQuests)
	res, err := chatWithActiveEngine(ctx, system, user, "", 1024)
	if err != nil {
		return nil, "", fmt.Errorf("planner LLM: %w", err)
	}
	raw := extractJSON(res.Content)
	if raw == "" {
		return nil, res.Content, fmt.Errorf("planner returned no JSON")
	}
	var plan planResult
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return nil, res.Content, fmt.Errorf("planner JSON invalid: %w", err)
	}
	// Validate: tool must exist, title required, deps in range.
	valid := make([]plannedQuest, 0, len(plan.Quests))
	for _, q := range plan.Quests {
		if strings.TrimSpace(q.Title) == "" {
			continue
		}
		if _, ok := karaka.GetTool(q.Tool); !ok {
			log.Printf("orchestrator: dropping quest %q — unknown tool %q", q.Title, q.Tool)
			continue
		}
		if q.Params == nil {
			q.Params = map[string]interface{}{}
		}
		cleanDeps := make([]int, 0, len(q.DependsOn))
		for _, d := range q.DependsOn {
			if d >= 1 && d <= len(plan.Quests) {
				cleanDeps = append(cleanDeps, d)
			}
		}
		q.DependsOn = cleanDeps
		valid = append(valid, q)
		if len(valid) >= maxQuests {
			break
		}
	}
	if len(valid) == 0 {
		return nil, res.Content, fmt.Errorf("planner produced zero valid quests")
	}
	plan.Quests = valid
	return &plan, res.Content, nil
}

// ─── Execution ───

func runOrchestration(ctx context.Context, missionID, karakaID string, maxQuests int) {
	log.Printf("orchestrator: run started for mission %s (karaka=%s)", missionID, karakaID)

	m, ok := moksa.GetMission(missionID)
	if !ok {
		log.Printf("orchestrator: mission %s vanished", missionID)
		return
	}

	// ── PLAN ──
	if len(m.Quests) == 0 {
		planCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		plan, raw, err := planQuests(planCtx, m.Instruction, maxQuests)
		cancel()
		if err != nil {
			log.Printf("orchestrator: planning failed for %s: %v", missionID, err)
			_, _ = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
				miss.Status = "cancelled"
				miss.Summary = fmt.Sprintf("Planning failed: %v\n\n--- raw planner output ---\n%s", err, truncateStr(raw, 2000))
				return nil
			})
			return
		}

		// Create quests (AddQuest assigns q1..qn in order).
		questIDs := make([]string, 0, len(plan.Quests))
		for _, pq := range plan.Quests {
			updated, err := moksa.AddQuest(missionID, pq.Title, karakaID, pq.Tool, pq.Params, nil, 30)
			if err != nil {
				log.Printf("orchestrator: AddQuest failed: %v", err)
				continue
			}
			questIDs = append(questIDs, updated.Quests[len(updated.Quests)-1].ID)
		}

		// Wire dependencies: planned index (1-based) → assigned quest ID.
		for i, pq := range plan.Quests {
			if i >= len(questIDs) || len(pq.DependsOn) == 0 {
				continue
			}
			deps := make([]string, 0, len(pq.DependsOn))
			for _, d := range pq.DependsOn {
				if d-1 < len(questIDs) {
					deps = append(deps, questIDs[d-1])
				}
			}
			qid := questIDs[i]
			_, _ = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
				if q := miss.FindQuest(qid); q != nil {
					q.DependsOn = deps
				}
				return nil
			})
		}

		_, _ = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
			miss.Status = "active"
			return nil
		})
		log.Printf("orchestrator: planned %d quests for %s", len(questIDs), missionID)
	}

	// ── EXECUTE ──
	m, _ = moksa.GetMission(missionID)
	if m == nil {
		return
	}
	failed := map[string]bool{}
	for _, quest := range m.Quests {
		if ctx.Err() != nil {
			break
		}
		if quest.Status != "pending" {
			continue
		}
		// Skip if any dependency failed/cancelled.
		depFailed := false
		for _, dep := range quest.DependsOn {
			if failed[dep] {
				depFailed = true
				break
			}
		}
		if depFailed {
			_, _ = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
				if q := miss.FindQuest(quest.ID); q != nil {
					q.Status = "cancelled"
					q.Error = "dependency failed"
				}
				return nil
			})
			failed[quest.ID] = true
			continue
		}

		if _, err := moksa.ClaimQuest(missionID, quest.ID, karakaID); err != nil {
			log.Printf("orchestrator: claim %s failed: %v", quest.ID, err)
			failed[quest.ID] = true
			continue
		}
		log.Printf("orchestrator: running quest %s (%s)", quest.ID, quest.Tool)
		updated, err := moksa.RunQuest(missionID, quest.ID)
		if err != nil {
			log.Printf("orchestrator: run %s failed: %v", quest.ID, err)
			failed[quest.ID] = true
			continue
		}
		if q := updated.FindQuest(quest.ID); q != nil && q.Status == "failed" {
			log.Printf("orchestrator: quest %s failed: %s", quest.ID, q.Error)
			failed[quest.ID] = true
		}
	}

	// ── SYNTHESIZE ──
	_, _ = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
		miss.Status = "synthesizing"
		return nil
	})
	if _, err := moksa.Synthesize(missionID); err != nil {
		log.Printf("orchestrator: synthesize %s failed: %v", missionID, err)
	}
	log.Printf("orchestrator: run finished for mission %s (%d/%d quests failed)", missionID, len(failed), len(m.Quests))
}

// ─── HTTP handlers ───

// orchestratorRunHandler — POST /api/web/orchestrator/run
// Body: { "instruction": "...", "karaka_id"?: "suparna_vpc", "max_quests"?: 6 }
//    or: { "mission_id": "m…", "karaka_id"?: … } to execute an existing board.
func orchestratorRunHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Instruction string `json:"instruction"`
		MissionID   string `json:"mission_id"`
		KarakaID    string `json:"karaka_id"`
		MaxQuests   int    `json:"max_quests"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.KarakaID == "" {
		req.KarakaID = "suparna_vpc"
	}
	if req.MaxQuests <= 0 {
		req.MaxQuests = 6
	}
	if req.MaxQuests > 12 {
		req.MaxQuests = 12
	}

	var missionID string
	if req.MissionID != "" {
		if _, ok := moksa.GetMission(req.MissionID); !ok {
			sendJSON(w, http.StatusNotFound, map[string]string{"error": "mission not found"})
			return
		}
		missionID = req.MissionID
	} else if strings.TrimSpace(req.Instruction) != "" {
		// Empty board — PoseBoard heuristic must NOT pre-fill, or the LLM planner is skipped.
		m := moksa.CreateEmptyMission(strings.TrimSpace(req.Instruction))
		missionID = m.ID
	} else {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing instruction or mission_id"})
		return
	}

	if !orchestratorMu.TryLock() {
		sendJSON(w, http.StatusConflict, map[string]string{"error": "orchestrator_busy", "mission_id": orchestratorMission})
		return
	}

	setOrchestratorState(true, missionID)
	go func() {
		defer orchestratorMu.Unlock()
		defer setOrchestratorState(false, "")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		runOrchestration(ctx, missionID, req.KarakaID, req.MaxQuests)
	}()

	sendJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":     "planning",
		"mission_id": missionID,
		"karaka_id":  req.KarakaID,
	})
}

// orchestratorStatusHandler — GET /api/web/orchestrator/status
func orchestratorStatusHandler(w http.ResponseWriter, r *http.Request) {
	orchestratorStateMu.RLock()
	defer orchestratorStateMu.RUnlock()
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"running":    orchestratorRunning,
		"mission_id": orchestratorMission,
		"since":      orchestratorSince,
	})
}
