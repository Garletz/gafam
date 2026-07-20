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
	"regexp"
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
	b.WriteString("You are Saṃyojaka, orchestrator of a sovereign VPC node.\n")
	b.WriteString("You have 3 Organic Tools + a mini-OpenCode in the sandbox.\n\n")
	b.WriteString("🌐 Vātāyana — live web (Firefox)\n")
	b.WriteString("   browser.sense  → fetch + LLM extraction in ONE call. BEST for news/headlines/current info.\n")
	b.WriteString("   browser.fetch  → raw page text + links (use with sandbox scripts for complex parsing)\n")
	b.WriteString("   browser.navigate / window\n")
	b.WriteString("🛠️ Yantraśālā — YOUR MINI-OPENCODE\n")
	b.WriteString("   sandbox.file_write → write ANY Python/bash script on-the-fly\n")
	b.WriteString("   sandbox.exec       → run it (python3, bash, curl, jq, sqlite3...)\n")
	b.WriteString("   sandbox.file_read  → read results\n")
	b.WriteString("   sandbox.shell      → persistent session\n")
	b.WriteString("   sandbox.tree       → filesystem tree\n")
	b.WriteString("📚 Vault — stored notes (research.search, notes)\n")
	b.WriteString("💬 llm.chat  → ask the LLM directly\n\n")
	b.WriteString("KEY STRATEGY: the sandbox IS your code editor.\n")
	b.WriteString("Need to parse a page? Write a Python script that uses the fetched text.\n")
	b.WriteString("Need to filter/transform data? Write a jq one-liner or Python.\n")
	b.WriteString("Need to call multiple URLs? Write a bash loop with curl.\n")
	b.WriteString("Every quest can CREATE its own tools via sandbox.file_write + sandbox.exec.\n\n")
	b.WriteString("Chaining example for news:\n")
	b.WriteString("  BEST: q1: browser.sense(url=\"https://lemonde.fr\", question=\"main headlines today\")\n")
	b.WriteString("  OR manual: q1: browser.fetch → q2: sandbox.file_write(path=\"/files/news.txt\", content=\"{{q1.result.text}}\")\n")
	b.WriteString("  Use /files/ (not /tmp/) for sandbox file paths.\n\n")
	b.WriteString("---\n")
	fmt.Fprintf(&b, "Plan %d quests max. STRICT JSON:\n", maxQuests)
	b.WriteString("{\"quests\": [{\"title\":\"..\", \"tool\":\"browser.fetch\", \"params\":{\"url\":\"https://..\"}}]}\n")
	b.WriteString("No markdown, ONLY JSON.\n")
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

	const maxIterations = 3
	for iteration := 0; iteration < maxIterations; iteration++ {
		m, ok := moksa.GetMission(missionID)
		if !ok {
			log.Printf("orchestrator: mission %s vanished", missionID)
			return
		}

		// ── PLAN (first iteration) or REPLAN (after observing results) ──
		if len(m.Quests) == 0 || iteration > 0 {
			var instruction string
			if iteration == 0 {
				instruction = m.Instruction
			} else {
				instruction = buildReplanInstruction(m)
				if instruction == "" {
					break // no replanning needed — all quests succeeded
				}
			}

			planCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			plan, raw, err := planQuests(planCtx, instruction, maxQuests)
			cancel()
			if err != nil {
				log.Printf("orchestrator: planning failed for %s (iter %d): %v", missionID, iteration, err)
				if iteration == 0 {
					_, _ = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
						miss.Status = "cancelled"
						miss.Summary = fmt.Sprintf("Planning failed: %v\n\n--- raw planner output ---\n%s", err, truncateStr(raw, 2000))
						return nil
					})
					return
				}
				// On later iterations, just break and synthesize with what we have
				break
			}

			questIDs := make([]string, 0, len(plan.Quests))
			for _, pq := range plan.Quests {
				updated, err := moksa.AddQuest(missionID, pq.Title, karakaID, pq.Tool, pq.Params, nil, 30)
				if err != nil {
					log.Printf("orchestrator: AddQuest failed: %v", err)
					continue
				}
				questIDs = append(questIDs, updated.Quests[len(updated.Quests)-1].ID)
			}

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
			log.Printf("orchestrator: iteration %d — planned %d quests for %s", iteration+1, len(questIDs), missionID)
		}

		// ── EXECUTE ──
		_ = executeQuestLevels(ctx, missionID, karakaID)

		// ── OBSERVE: check if all done or need replanning ──
		m, _ = moksa.GetMission(missionID)
		if m == nil {
			return
		}
		pending := 0
		doneAll := true
		hasNewDone := false
		for _, q := range m.Quests {
			if q.Status == "done" {
				hasNewDone = true
			} else if q.Status != "failed" && q.Status != "cancelled" && q.Status != "skipped" {
				doneAll = false
				if q.Status == "pending" {
					pending++
				}
			}
		}
		if doneAll && (hasNewDone || iteration == 0) {
			log.Printf("orchestrator: all quests resolved, synthesizing")
			break
		}
		if pending == 0 && !hasNewDone {
			// Nothing to do and nothing succeeded — abort
			log.Printf("orchestrator: no progress in iteration %d, aborting", iteration+1)
			break
		}
		log.Printf("orchestrator: iteration %d complete — replanning for remaining work", iteration+1)
	}

	// ── SYNTHESIZE ──
	_, _ = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
		miss.Status = "synthesizing"
		return nil
	})
	if _, err := moksa.Synthesize(missionID); err != nil {
		log.Printf("orchestrator: synthesize %s failed: %v", missionID, err)
	}
	if m, ok := moksa.GetMission(missionID); ok && m.Summary != "" {
		saveMissionToVault(m)
	}
	finalM, _ := moksa.GetMission(missionID)
	failedN := 0
	totalN := 0
	if finalM != nil {
		totalN = len(finalM.Quests)
		for _, q := range finalM.Quests {
			if q.Status == "failed" || q.Status == "cancelled" {
				failedN++
			}
		}
	}
	log.Printf("orchestrator: run finished for mission %s (%d/%d quests failed)", missionID, failedN, totalN)
}

// buildReplanInstruction creates a prompt describing what's been done and what's still needed.
func buildReplanInstruction(m *moksa.Mission) string {
	var b strings.Builder
	b.WriteString("Original instruction: " + m.Instruction + "\n\n")
	b.WriteString("What has been done so far:\n")
	hasWork := 0
	for _, q := range m.Quests {
		switch q.Status {
		case "done":
			fmt.Fprintf(&b, "  ✅ %s (%s) → result: %v\n", q.Title, q.Tool, summarizeResult(q.Result))
			hasWork++
		case "failed":
			fmt.Fprintf(&b, "  ❌ %s (%s) — %s\n", q.Title, q.Tool, q.Error)
		case "pending":
			fmt.Fprintf(&b, "  ⏳ %s (%s) — not yet attempted\n", q.Title, q.Tool)
		}
	}
	if hasWork == 0 {
		return "" // nothing useful was done, no point replanning
	}
	b.WriteString("\nBased on what succeeded and what failed, plan additional quests to complete the original instruction. If the instruction is already satisfied, output:\n")
	b.WriteString(`{"quests": []}` + "\n")
	b.WriteString("Otherwise, output new quests following the standard format. Focus on what's MISSING — don't repeat completed work.\n")
	return b.String()
}

func summarizeResult(r interface{}) string {
	if r == nil {
		return "(none)"
	}
	if m, ok := r.(map[string]interface{}); ok {
		// For browser.sense/llm.chat: show answer
		if a, ok := m["answer"].(string); ok && len(a) > 0 {
			return truncateStr(a, 120)
		}
		if c, ok := m["content"].(string); ok && len(c) > 0 {
			return truncateStr(c, 120)
		}
		// For sandbox.exec: show stdout
		if o, ok := m["stdout"].(string); ok && len(o) > 0 {
			return truncateStr(o, 120)
		}
		// For browser.fetch: show text snippet
		if t, ok := m["text"].(string); ok && len(t) > 0 {
			return truncateStr(t, 120)
		}
		return fmt.Sprintf("(%d fields)", len(m))
	}
	s := fmt.Sprintf("%v", r)
	return truncateStr(s, 120)
}

// executeQuestLevels runs pending quests level by level: all quests whose
// dependencies are satisfied run in parallel (bounded). A quest whose
// dependency failed is cancelled; a dependency cycle cancels the rest.

// interpolateParams resolves {{qN.field}} references in quest params
// using results from previously completed quests.
func interpolateParams(params map[string]interface{}, mission *moksa.Mission) map[string]interface{} {
	if params == nil {
		return params
	}
	out := make(map[string]interface{}, len(params))
	re := regexp.MustCompile(`\{\{(q\d+)\.(result\.\w+(?:\.\w+)*|result)\}\}`)

	// Build lookup: qID → result map
	results := map[string]interface{}{}
	for _, q := range mission.Quests {
		if q.Status == "done" && q.Result != nil {
			results[q.ID] = q.Result
		}
	}

	for k, v := range params {
		s, isStr := v.(string)
		if !isStr {
			out[k] = v
			continue
		}
		out[k] = re.ReplaceAllStringFunc(s, func(match string) string {
			parts := re.FindStringSubmatch(match)
			if len(parts) < 3 {
				return match
			}
			qid := parts[1]
			fieldPath := parts[2] // e.g. "result.text" or "result"
			result, ok := results[qid]
			if !ok {
				return match
			}
			if resultMap, ok := result.(map[string]interface{}); ok {
				if fieldPath == "result" {
					// Return whole result as JSON string
					b, _ := json.Marshal(resultMap)
					return string(b)
				}
				// Walk fieldPath like "result.text" → resultMap["text"]
				fields := strings.Split(fieldPath, ".")
				cur := interface{}(resultMap)
				for _, f := range fields[1:] { // skip "result"
					if m, ok := cur.(map[string]interface{}); ok {
						cur = m[f]
					} else {
						return match
					}
				}
				if s, ok := cur.(string); ok {
					return s
				}
				b, _ := json.Marshal(cur)
				return string(b)
			}
			return match
		})
	}
	return out
}

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
		// Interpolate {{qN.field}} references from previous quest results
		m, _ := moksa.GetMission(missionID)
		if m != nil {
			q.Params = interpolateParams(q.Params, m)
		}
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
			queueSmsReply(selfPhone, "GAFAM ❌ "+done.ID+": "+truncateStr(done.Summary, 200))
			return
		}
		total := len(done.Quests)
		failedN := 0
		var summarySentences []string
		for _, q := range done.Quests {
			if q.Status == "failed" || q.Status == "cancelled" {
				failedN++
			} else if q.Result != nil {
				// Extract useful info from quest results for the SMS summary
				if resultMap, ok := q.Result.(map[string]interface{}); ok {
					switch q.Tool {
					case "browser.fetch":
						if text, ok := resultMap["text"].(string); ok && len(text) > 0 {
							firstLines := takeFirstNLines(text, 3)
							summarySentences = append(summarySentences, firstLines)
						}
					case "browser.sense":
						if answer, ok := resultMap["answer"].(string); ok && len(answer) > 0 {
							summarySentences = append(summarySentences, answer)
						}
					case "llm.chat":
						if content, ok := resultMap["content"].(string); ok && len(content) > 0 {
							summarySentences = append(summarySentences, content)
						}
					case "sandbox.exec", "sandbox.shell":
						if stdout, ok := resultMap["stdout"].(string); ok && len(stdout) > 0 {
							summarySentences = append(summarySentences, truncateStr(stdout, 120))
						}
					}
				}
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
		// Build SMS with summary + link
		msg := fmt.Sprintf("GAFAM %s %s: %d/%d quests OK.", status, done.ID, total-failedN, total)
		if len(summarySentences) > 0 {
			msg += "\n" + strings.Join(summarySentences, "\n")
		}
		if done.Summary != "" {
			msg += "\n" + truncateStr(done.Summary, 300)
		}
		queueSmsReply(selfPhone, msg)
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

// saveMissionToVault stores the synthesis report as a research note.
func saveMissionToVault(m *moksa.Mission) {
	body := m.Summary
	if body == "" {
		return
	}
	title := m.Instruction
	if len(title) > 80 {
		title = title[:77] + "..."
	}
	id := "mission-" + m.ID
	tags := "mission saṃyojaka"

	// Write markdown to sandbox
	path := "/files/research/notes/" + id + ".md"
	md := fmt.Sprintf("---\nid: %s\ntitle: %q\ntags: [%s]\n---\n# %s\n\n%s\n", id, title, tags, title, body)
	_, writeErr := karaka.ExecuteTool("sandbox.file_write", map[string]interface{}{
		"path":    path,
		"content": md,
	})
	if writeErr != nil {
		log.Printf("orchestrator: vault write failed for %s: %v", m.ID, writeErr)
		return
	}

	// Index in FTS5 (best effort via research note insert)
	_, _ = db.Exec(
		`INSERT INTO research_notes (id, title, url, tags, body, fetched_at, path, suggested_by)
		 VALUES (?, ?, ?, ?, ?, datetime('now'), ?, ?)`,
		id, title, "", tags, body, path, "saṃyojaka",
	)
	log.Printf("orchestrator: report saved to vault for mission %s", m.ID)
}

func takeFirstNLines(text string, n int) string {
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	out := strings.Join(lines, " ")
	if len(out) > 200 {
		out = out[:197] + "..."
	}
	return out
}
