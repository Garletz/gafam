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

	orchestratorStateMu   sync.RWMutex
	orchestratorRunning   = false
	orchestratorMission   = ""
	orchestratorSince     time.Time
	orchestratorPublishTo string // recipient phone for feed publish ("*" = broadcast)
)

func setOrchestratorState(running bool, missionID string) {
	orchestratorStateMu.Lock()
	defer orchestratorStateMu.Unlock()
	orchestratorRunning = running
	orchestratorMission = missionID
	if running {
		orchestratorSince = time.Now()
	} else {
		orchestratorPublishTo = ""
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
	res, err := chatWithEngine(ctx, "orchestrator", system, user, 4096)
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

	// ── EXECUTE (topological levels — independent quests run in parallel) ──
	failed := executeQuestLevels(ctx, missionID, karakaID)

	// ── SYNTHESIZE ──
	_, _ = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
		miss.Status = "synthesizing"
		return nil
	})
	if _, err := moksa.Synthesize(missionID); err != nil {
		log.Printf("orchestrator: synthesize %s failed: %v", missionID, err)
	}
	m, _ = moksa.GetMission(missionID)
	questCount := 0
	if m != nil {
		questCount = len(m.Quests)
	}
	log.Printf("orchestrator: run finished for mission %s (%d/%d quests failed)", missionID, len(failed), questCount)
}

// executeQuestLevels runs pending quests level by level: all quests whose
// dependencies are satisfied run in parallel (bounded). A quest whose
// dependency failed is cancelled; a dependency cycle cancels the rest.
func executeQuestLevels(ctx context.Context, missionID, karakaID string) map[string]bool {
	const maxParallel = 4

	m, _ := moksa.GetMission(missionID)
	if m == nil {
		return map[string]bool{}
	}

	pending := map[string]moksa.Quest{}
	done := map[string]bool{}
	failed := map[string]bool{}
	for _, q := range m.Quests {
		switch q.Status {
		case "pending":
			pending[q.ID] = q
		case "done":
			done[q.ID] = true
		case "failed", "cancelled":
			failed[q.ID] = true
		}
	}
	var stateMu sync.Mutex

	cancelQuest := func(qid, reason string) {
		_, _ = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
			if q := miss.FindQuest(qid); q != nil {
				q.Status = "cancelled"
				q.Error = reason
			}
			return nil
		})
		stateMu.Lock()
		failed[qid] = true
		stateMu.Unlock()
	}

	runOne := func(q moksa.Quest) {
		if _, err := moksa.ClaimQuest(missionID, q.ID, karakaID); err != nil {
			log.Printf("orchestrator: claim %s failed: %v", q.ID, err)
			stateMu.Lock()
			failed[q.ID] = true
			stateMu.Unlock()
			return
		}
		log.Printf("orchestrator: running quest %s (%s)", q.ID, q.Tool)
		updated, err := moksa.RunQuest(missionID, q.ID)
		stateMu.Lock()
		defer stateMu.Unlock()
		if err != nil {
			log.Printf("orchestrator: run %s failed: %v", q.ID, err)
			failed[q.ID] = true
			return
		}
		if rq := updated.FindQuest(q.ID); rq != nil && rq.Status == "failed" {
			log.Printf("orchestrator: quest %s failed: %s", q.ID, rq.Error)
			failed[q.ID] = true
			return
		}
		done[q.ID] = true
	}

	for len(pending) > 0 {
		if ctx.Err() != nil {
			break
		}
		// Collect the runnable level + cancel quests whose deps failed.
		var level []moksa.Quest
		for _, q := range pending {
			depFailed := false
			depOK := true
			for _, dep := range q.DependsOn {
				stateMu.Lock()
				f, d := failed[dep], done[dep]
				stateMu.Unlock()
				if f {
					depFailed = true
					break
				}
				if !d {
					depOK = false
					break
				}
			}
			if depFailed {
				cancelQuest(q.ID, "dependency failed")
				delete(pending, q.ID)
				continue
			}
			if depOK {
				level = append(level, q)
			}
		}

		if len(level) == 0 {
			// Nothing runnable but quests remain: dependency cycle.
			for id := range pending {
				cancelQuest(id, "dependency cycle — unsatisfiable")
				delete(pending, id)
			}
			break
		}

		// Run the level in parallel (bounded).
		sem := make(chan struct{}, maxParallel)
		var wg sync.WaitGroup
		for _, q := range level {
			delete(pending, q.ID)
			wg.Add(1)
			go func(quest moksa.Quest) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				runOne(quest)
			}(q)
		}
		wg.Wait()
	}

	return failed
}

// ─── HTTP handlers ───

// orchestratorRunHandler — POST /api/web/orchestrator/run
// Body: { "instruction": "...", "karaka_id"?: "suparna_vpc", "max_quests"?: 6 }
//    or: { "mission_id": "m…", "karaka_id"?: … } to execute an existing board.
func orchestratorRunHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Instruction    string `json:"instruction"`
		MissionID      string `json:"mission_id"`
		KarakaID       string `json:"karaka_id"`
		MaxQuests      int    `json:"max_quests"`
		Mode           string `json:"mode"`           // "" | action | research
		PublishFeed    bool   `json:"publish_feed"`   // publish synthesis to /feed when done
		RecipientPhone string `json:"recipient_phone"` // target for feed publish (default: *)
		RequireApproval bool  `json:"require_approval"` // pause before each quest for human approval
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
	if req.Mode != "" && req.Mode != "action" && req.Mode != "research" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid mode (action|research)"})
		return
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

	if !launchOrchestration(missionID, req.KarakaID, req.MaxQuests, req.Mode, nil, req.PublishFeed, req.RecipientPhone) {
		sendJSON(w, http.StatusConflict, map[string]string{"error": "orchestrator_busy", "mission_id": orchestratorMission})
		return
	}

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
		"running":     orchestratorRunning,
		"mission_id":  orchestratorMission,
		"since":       orchestratorSince,
		"publish_to":  orchestratorPublishTo,
	})
}

// launchOrchestration starts the loop in the background (one run at a time).
// mode: "" / "action" (free planner) | "research" (fixed pipeline).
// onDone, if set, receives the final mission state — used by the self-phone
// SMS trigger to text the result back.
func launchOrchestration(missionID, karakaID string, maxQuests int, mode string, onDone func(*moksa.Mission), publishFeed bool, recipientPhone string) bool {
	if !orchestratorMu.TryLock() {
		return false
	}
	setOrchestratorState(true, missionID)
	if publishFeed {
		orchestratorStateMu.Lock()
		orchestratorPublishTo = recipientPhone
		if orchestratorPublishTo == "" {
			orchestratorPublishTo = "*"
		}
		orchestratorStateMu.Unlock()
	}
	go func() {
		defer orchestratorMu.Unlock()
		defer setOrchestratorState(false, "")
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
		defer cancel()
		if mode == "research" {
			runResearchPipeline(ctx, missionID)
		} else {
			runOrchestration(ctx, missionID, karakaID, maxQuests)
		}
		if onDone != nil {
			if m, ok := moksa.GetMission(missionID); ok {
				onDone(m)
			}
		}
		// Publish synthesis to feed if requested
		if publishFeed && recipientPhone != "" {
			publishMissionResult(missionID, recipientPhone)
		} else if publishFeed {
			publishMissionResult(missionID, "*")
		}
	}()
	return true
}

// ─── Self-phone remote trigger (SMS → quest) ───

// selfQuestInstruction checks whether an incoming SMS is a remote quest
// command from the owner's self phone (Settings → self_phone).
// Triggers: "/q " = action mission, "/r " = research mission (case-insensitive).
func selfQuestInstruction(sender, body string) (instruction, mode string, ok bool) {
	self := getSetting("self_phone")
	if self == "" || !phonesMatch(sender, self) {
		return "", "", false
	}
	trimmed := strings.TrimSpace(body)
	lower := strings.ToLower(trimmed)
	prefixes := []struct {
		p    string
		mode string
	}{
		{"/r ", "research"},
		{"/research ", "research"},
		{"/q ", "action"},
		{"/quest ", "action"},
	}
	for _, pre := range prefixes {
		if strings.HasPrefix(lower, pre.p) {
			if instr := strings.TrimSpace(trimmed[len(pre.p):]); instr != "" {
				return instr, pre.mode, true
			}
		}
	}
	return "", "", false
}

// phonesMatch compares two phone numbers on their last 9 digits
// (same convention as the guardian recovery check).
func phonesMatch(a, b string) bool {
	digits := func(s string) string {
		out := make([]byte, 0, len(s))
		for i := 0; i < len(s); i++ {
			if s[i] >= '0' && s[i] <= '9' {
				out = append(out, s[i])
			}
		}
		if len(out) >= 9 {
			return string(out[len(out)-9:])
		}
		return string(out)
	}
	ca, cb := digits(a), digits(b)
	return ca != "" && cb != "" && ca == cb
}

// triggerSelfQuest creates a mission from an SMS instruction and runs
// Saṃyojaka on it, texting confirmations back to the self phone.
func triggerSelfQuest(selfPhone, instruction, mode string) {
	m := moksa.CreateEmptyMission(instruction)
	log.Printf("self-quest: mission %s created from self phone SMS (mode=%s)", m.ID, mode)

	ok := launchOrchestration(m.ID, "suparna_vpc", 6, mode, func(done *moksa.Mission) {
		if done.Status == "cancelled" {
			queueSmsReply(selfPhone, "GAFAM ❌ "+done.ID+": failed — see dashboard for details")
			return
		}
		total := len(done.Quests)
		failedN := 0
		for _, q := range done.Quests {
			if q.Status == "failed" || q.Status == "cancelled" {
				failedN++
			}
		}
		status := "✅"
		if failedN > 0 {
			status = "⚠️"
		}
		if mode == "research" {
			queueSmsReply(selfPhone, fmt.Sprintf(
				"GAFAM %s research %s done. Report: /files/research/missions/%s/report.md",
				status, done.ID, done.ID,
			))
			return
		}
		queueSmsReply(selfPhone, fmt.Sprintf(
			"GAFAM %s %s: %d/%d quests OK. Report: /files/missions/%s/report.md (sandbox)",
			status, done.ID, total-failedN, total, done.ID,
		))
	}, false, "")
	if !ok {
		// Busy — drop the placeholder mission so the board stays clean.
		moksa.DeleteMission(m.ID)
		queueSmsReply(selfPhone, "GAFAM ⏳ saṃyojaka busy on another mission — retry in a few minutes")
		return
	}
	if mode == "research" {
		queueSmsReply(selfPhone, fmt.Sprintf("GAFAM 🔬 research %s started — vault checked first, then web sweep. I'll text you the report path", m.ID))
		return
	}
	queueSmsReply(selfPhone, fmt.Sprintf("GAFAM ⚡ quest %s started — I'll text you when done", m.ID))
}

// queueSmsReply appends an SMS to the outbox (the relay phone sends it)
// and mirrors it in the chat history.
func queueSmsReply(recipient, body string) {
	if _, err := db.Exec(`INSERT INTO gafam_outbox (recipient, body) VALUES (?, ?)`, recipient, body); err != nil {
		log.Printf("self-quest: outbox insert failed: %v", err)
		return
	}
	ts := time.Now().UnixMilli()
	_, _ = db.Exec(
		`INSERT INTO gafam_sms (sender, body, timestamp, status) VALUES (?, ?, ?, ?)`,
		recipient, body, ts, "outbound",
	)
}

// publishMissionResult publishes a mission's summary to the VPC feed
// so federated nodes can scan it via their /links/{phone}/scan endpoint.
func publishMissionResult(missionID, recipientPhone string) {
	m, ok := moksa.GetMission(missionID)
	if !ok {
		log.Printf("feed-publish: mission %s not found", missionID)
		return
	}
	selfPhone := getSelfPhone()
	if selfPhone == "" {
		log.Printf("feed-publish: self_phone not set — skipping")
		return
	}
	content := fmt.Sprintf(
		"Saṃyojaka mission %s: %s\nStatus: %s — %d/%d quests completed\nSummary: %s",
		missionID, m.Instruction, m.Status,
		countDoneQuests(m), len(m.Quests),
		m.Summary,
	)
	if len(content) > 2000 {
		content = content[:1996] + "..."
	}
	if _, err := db.Exec(
		`INSERT INTO gafam_envelopes (author_phone, recipient_phone, content, created_at) 
		 VALUES (?, ?, ?, datetime('now'))`,
		selfPhone, recipientPhone, content,
	); err != nil {
		log.Printf("feed-publish: insert envelope failed: %v", err)
		return
	}
	log.Printf("feed-publish: mission %s published to feed for %s", missionID, recipientPhone)
}

func countDoneQuests(m *moksa.Mission) int {
	n := 0
	for _, q := range m.Quests {
		if q.Status == "done" {
			n++
		}
	}
	return n
}
