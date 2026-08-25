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
	"strconv"
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
	orchestratorApproval  bool   // require human approval before quests run
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
		orchestratorApproval = false
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
	b.WriteString("You have 3 Organic Tools + CDP browser control.\n\n")

	// Inject vault memory (recent notes + FTS-relevant past missions)
	vaultCtx := getVaultContext()
	if vaultCtx != "" {
		b.WriteString(vaultCtx)
	}
	if memCtx := getMissionMemoryContext(instruction); memCtx != "" {
		b.WriteString(memCtx)
	}

	b.WriteString("🌐 Vātāyana — live web (Firefox + Chromium CDP) — ONLY for JS-heavy or interactive pages:\n")
	b.WriteString("   browser.sense  → fetch + LLM extraction in ONE call (semantic understanding of a page)\n")
	b.WriteString("   browser.fetch  → raw page text + links\n")
	b.WriteString("   browser.navigate / window\n")
	b.WriteString("🛠️ Yantraśālā — YOUR MINI-OPENCODE (has internet: curl, wget, python3, node, jq)\n")
	b.WriteString("   sandbox.file_write → write ANY Python/Node/bash script on-the-fly\n")
	b.WriteString("   sandbox.exec       → run it (python3, node, bash, curl, jq, sqlite3...)\n")
	b.WriteString("   Missing a library? INSTALL IT yourself, it persists across runs:\n")
	b.WriteString("     pip install --user <pkg>     (lands in /sandbox/files/.pylibs)\n")
	b.WriteString("     npm install -g <pkg>         (lands in /sandbox/files/.npm-global)\n")
	b.WriteString("   e.g. need a PDF report? pip install --user fpdf2, write a script, done.\n")
	b.WriteString("   sandbox.file_read  → read results\n")
	b.WriteString("   sandbox.shell      → persistent session\n")
	b.WriteString("   sandbox.tree       → filesystem tree\n")
	b.WriteString("🔮 CDP (Chrome DevTools) — REAL browser automation:\n")
	b.WriteString("   browser.cdp_nav   → navigate to URL (faster than Firefox)\n")
	b.WriteString("   browser.cdp_click  → click elements by CSS selector\n")
	b.WriteString("   browser.cdp_type   → type into input fields by selector\n")
	b.WriteString("   browser.cdp_text   → get text content of element\n")
	b.WriteString("   browser.cdp_eval   → execute arbitrary JavaScript\n")
	b.WriteString("📚 Vault — stored notes (research.search, notes)\n")
	b.WriteString("   vault.remember → persist a long-term memory (findings, decisions, preferences)\n")
	b.WriteString("🧠 Memory — semantic search over the node's own knowledge:\n")
	b.WriteString("   memory.semantic_search(query, entity_type?) → find things by MEANING\n")
	b.WriteString("     (e.g. \"qui peut m'aider à réparer un vélo\" → contact profiles)\n")
	b.WriteString("   memory.build_contact_profiles() → (re)build the contact profiles first\n")
	b.WriteString("📱 SMS & Contacts — the node's voice:\n")
	b.WriteString("   sms.send(to, body)   → send an SMS via the relay phone\n")
	b.WriteString("   sms.history(phone?)  → read recent SMS for context\n")
	b.WriteString("   contacts.search(q)   → find a phone number by name\n")
	b.WriteString("   feed.publish(content)→ publish to the federated feed\n")
	b.WriteString("💬 llm.chat  → ask the LLM directly\n")
	b.WriteString("🤖 karaka.delegate → spawn a sub-agent for a self-contained subtask\n\n")

	// Agent-written tools (custom.*) — discovered from the sandbox, so the
	// planner always sees the node's latest self-made capabilities.
	customTools := []karaka.Tool{}
	for _, t := range karaka.ListTools() {
		if t.Category == "custom" {
			customTools = append(customTools, t)
		}
	}
	if len(customTools) > 0 {
		b.WriteString("🧰 Custom tools (built by previous missions — reusable):\n")
		for _, t := range customTools {
			fmt.Fprintf(&b, "   %s → %s\n", t.ID, t.Description)
		}
		b.WriteString("\n")
	}
	b.WriteString("If a recurring sub-task has no tool, CREATE one: sandbox.file_write a .sh or .py\n")
	b.WriteString("script into /files/tools/ (with a '# desc: ...' header), then call it as custom.<name>.\n\n")
	b.WriteString("KEY STRATEGY: the sandbox IS your code editor.\n")
	b.WriteString("Need to parse a page? Write a Python script that uses the fetched text.\n")
	b.WriteString("Need to filter/transform data? Write a jq one-liner or Python.\n")
	b.WriteString("Need to call multiple URLs? Write a bash loop with curl.\n")
	b.WriteString("Every quest can CREATE its own tools via sandbox.file_write + sandbox.exec.\n\n")
	b.WriteString("WEB STRATEGY — PREFER sandbox.exec + curl over the browser:\n")
	b.WriteString("  - API / JSON / structured data / a simple page?  curl in the sandbox.\n")
	b.WriteString("    q1: sandbox.exec command=\"curl -s 'https://api.example.com/...?x=1' | jq .\"\n")
	b.WriteString("    → exact raw data, no sidecar to wake, no paraphrase drift. FASTEST & MOST RELIABLE.\n")
	b.WriteString("  - JS-heavy SPA, login, real interaction?  THEN use browser (cdp_nav/click/type) or browser.sense.\n")
	b.WriteString("  - Numbers MUST be copied EXACTLY: never round, never approximate, never invent.\n")
	b.WriteString("  - When a task needs gathered data turned into a summary/report, ALWAYS chain it:\n")
	b.WriteString("    q1 sandbox.exec (curl) → q2 llm.chat with {{q1.result.stdout}} in the prompt.\n\n")
	b.WriteString("Chaining example for news:\n")
	b.WriteString("  PREFERRED: q1: sandbox.exec command=\"curl -s https://lemonde.fr | head -c 8000\"\n")
	b.WriteString("             q2: llm.chat prompt=\"headlines from: {{q1.result.stdout}}\"\n")
	b.WriteString("  OR browser: q1: browser.sense(url=\"https://lemonde.fr\", question=\"main headlines today\")\n")
	b.WriteString("  RULE: any quest whose params use {{qN...}} MUST declare \"depends_on\": [N] — otherwise it runs BEFORE qN.\n")
	b.WriteString("  If the instruction asks for analysis/summary/report, plan a quest that PRODUCES it (llm.chat on gathered data).\n")
	b.WriteString("  CRITICAL: if the instruction is a QUESTION or asks for an answer/recommendation, ALWAYS\n")
	b.WriteString("  plan a FINAL quest llm.chat whose prompt includes the previous results ({{qN.result...}})\n")
	b.WriteString("  and which ANSWERS the question in natural language. Never end a mission with raw tool\n")
	b.WriteString("  output and no answer — the judge will fail it.\n")
	b.WriteString("  Use /files/ (not /tmp/) for sandbox file paths.\n\n")
	b.WriteString("---\n")
	fmt.Fprintf(&b, "Plan %d quests max. STRICT JSON:\n", maxQuests)
	b.WriteString("{\"quests\": [{\"title\":\"..\", \"tool\":\"browser.fetch\", \"params\":{\"url\":\"https://..\"}}]}\n")
	b.WriteString("No markdown, ONLY JSON.\n")
	return b.String(), "Instruction: " + instruction
}

// autoWireDeps forces dependencies implied by cross-quest references:
// a quest whose params contain {{qN...}} depends on qN, whether the planner
// declared it or not — otherwise the quest would run in the same parallel
// level as its source and the reference would interpolate to nothing.
var depRefRe = regexp.MustCompile(`\{\{q(\d+)\.`)

func autoWireDeps(quests []plannedQuest) {
	for qi := range quests {
		q := &quests[qi]
		for _, v := range q.Params {
			s, isStr := v.(string)
			if !isStr {
				continue
			}
			for _, m := range depRefRe.FindAllStringSubmatch(s, -1) {
				n, err := strconv.Atoi(m[1])
				if err != nil || n < 1 || n > len(quests) || n == qi+1 {
					continue
				}
				seen := false
				for _, d := range q.DependsOn {
					if d == n {
						seen = true
						break
					}
				}
				if !seen {
					q.DependsOn = append(q.DependsOn, n)
					log.Printf("orchestrator: auto-wired dependency q%d → q%d (found {{q%d. reference)", qi+1, n, n)
				}
			}
		}
	}
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
	// Refresh agent-written tools (sandbox /files/tools/) so a tool created
	// during a previous quest or replan round is immediately plannable.
	rescanCustomTools()

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
	autoWireDeps(valid)
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
		// Approval mode is per-tool (OpenCode-style): quests whose tool is
		// "ask" for this kāraka pause as waiting_approval; "allow" quests run.
		_ = executeQuestLevels(ctx, missionID, karakaID, orchestratorApproval)

		// ── OBSERVE: check if all done, waiting on human, or need replanning ──
		m, _ = moksa.GetMission(missionID)
		if m == nil {
			return
		}
		waiting := 0
		for _, q := range m.Quests {
			if q.Status == "waiting_approval" {
				waiting++
			}
		}
		if waiting > 0 {
			_, _ = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
				miss.Status = "waiting_approval"
				return nil
			})
			log.Printf("orchestrator: %d quest(s) waiting for human approval — pausing mission %s", waiting, missionID)
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

	// ── JUDGE (AET/GRM pattern): independent verification of the outcome
	// against the instruction — reward grounded in the result, not in the
	// agent's self-reported completion.
	judgeMission(ctx, missionID)

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

// ─── Judge (verify-in-the-loop) ───

// judgeMission evaluates the finished mission against its own instruction,
// following the generative-reward-model protocol: read the outcome → write a
// rubric → score against the rubric → record the verdict on the board.
func judgeMission(ctx context.Context, missionID string) {
	m, ok := moksa.GetMission(missionID)
	if !ok || m == nil {
		return
	}
	if m.Status == "cancelled" || len(m.Quests) == 0 {
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Original instruction: %s\n\n", m.Instruction)
	b.WriteString("Quest outcomes:\n")
	for _, q := range m.Quests {
		fmt.Fprintf(&b, "  [%s] %s (%s)", q.Status, q.Title, q.Tool)
		if q.Status == "done" {
			fmt.Fprintf(&b, " → %s", summarizeResult(q.Result))
		} else if q.Error != "" {
			fmt.Fprintf(&b, " — %s", truncateStr(q.Error, 120))
		}
		b.WriteString("\n")
	}
	if m.Summary != "" {
		fmt.Fprintf(&b, "\nFinal report (excerpt):\n%s\n", truncateStr(m.Summary, 1500))
	}

	system := `You are an independent verifier. You did NOT execute the mission — you judge it.
Protocol: (1) read the outcome, (2) write a short rubric of what the instruction required,
(3) score the outcome against your rubric, (4) record the verdict.
Be strict: a quest that merely ran is not a goal achieved. Penalize verbosity without substance.
Output STRICT JSON: {"rubric": "...", "verdict": "success|partial|failed", "score": 0.0, "reason": "one sentence"}`

	judgeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	res, err := chatWithEngine(judgeCtx, "orchestrator", system, b.String(), 1500)
	if err != nil {
		log.Printf("orchestrator: judge %s failed: %v", missionID, err)
		return
	}
	raw := extractJSON(res.Content)
	if raw == "" {
		return
	}
	var v struct {
		Rubric  string  `json:"rubric"`
		Verdict string  `json:"verdict"`
		Score   float64 `json:"score"`
		Reason  string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return
	}
	switch v.Verdict {
	case "success", "partial", "failed":
	default:
		v.Verdict = "partial"
	}
	if v.Score < 0 {
		v.Score = 0
	}
	if v.Score > 1 {
		v.Score = 1
	}
	_, _ = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
		miss.Judge = &moksa.Judge{
			Verdict: v.Verdict,
			Score:   v.Score,
			Rubric:  truncateStr(v.Rubric, 600),
			Reason:  truncateStr(v.Reason, 300),
		}
		return nil
	})
	log.Printf("orchestrator: judge %s → %s (%.2f) — %s", missionID, v.Verdict, v.Score, truncateStr(v.Reason, 100))
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

// ─── Agentic repair (ReAct at quest level) ───

// repairQuestParams asks the LLM to fix a failed quest's parameters given the
// observed error, then writes the corrected params back to the board.
// Returns nil on success (quest ready to re-run), error if unrepairable.
func repairQuestParams(ctx context.Context, missionID, questID, lastErr string) error {
	m, ok := moksa.GetMission(missionID)
	if !ok {
		return fmt.Errorf("mission vanished")
	}
	q := m.FindQuest(questID)
	if q == nil {
		return fmt.Errorf("quest vanished")
	}
	tool, ok := karaka.GetTool(q.Tool)
	if !ok {
		return fmt.Errorf("unknown tool %s", q.Tool)
	}

	spec, _ := json.Marshal(tool.Params)
	current, _ := json.Marshal(q.Params)
	system := "You repair failed tool calls. Output STRICT JSON: only the corrected params object, no markdown, no commentary."
	user := fmt.Sprintf(
		"Tool: %s — %s\nParam schema: %s\nParams that failed: %s\nObserved error: %s\n\nMission instruction: %s\nQuest goal: %s\n\nOutput the corrected params JSON object.",
		tool.ID, tool.Description, spec, current, truncateStr(lastErr, 400),
		truncateStr(m.Instruction, 200), truncateStr(q.Title, 120),
	)
	repairCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	res, err := chatWithEngine(repairCtx, "light_task", system, user, 1024)
	if err != nil {
		return err
	}
	raw := extractJSON(res.Content)
	if raw == "" {
		return fmt.Errorf("repair: no JSON in LLM reply")
	}
	var fixed map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &fixed); err != nil {
		return fmt.Errorf("repair: bad JSON: %w", err)
	}
	if err := karaka.ValidateParams(tool, fixed); err != nil {
		return fmt.Errorf("repair: %w", err)
	}
	_, err = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
		if qq := miss.FindQuest(questID); qq != nil {
			qq.Params = fixed
			qq.Error = ""
		}
		return nil
	})
	if err == nil {
		log.Printf("orchestrator: quest %s params repaired by LLM", questID)
	}
	return err
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

func executeQuestLevels(ctx context.Context, missionID, karakaID string, approvalMode bool) map[string]bool {
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
		case "pending", "failed":
			// "failed" quests get one fresh chance on a re-run (human may have
			// edited params, or a sidecar was simply asleep).
			pending[q.ID] = q
		case "done":
			done[q.ID] = true
		case "cancelled":
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
		// Permission gate (Manifest 25): in approval mode, an "ask" tool
		// pauses the quest until a human approves it from the dashboard.
		if perm := karaka.CheckPermission(karakaID, q.Tool); perm == "deny" {
			cancelQuest(q.ID, fmt.Sprintf("permission deny: %s cannot use %s", karakaID, q.Tool))
			return
		} else if perm == "ask" && approvalMode {
			_, _ = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
				if qq := miss.FindQuest(q.ID); qq != nil {
					qq.Status = "waiting_approval"
				}
				return nil
			})
			log.Printf("orchestrator: quest %s (%s) parked — permission ask", q.ID, q.Tool)
			return
		}

		// Interpolate {{qN.field}} references from previous quest results
		m, _ := moksa.GetMission(missionID)
		if m != nil {
			q.Params = interpolateParams(q.Params, m)
			_, _ = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
				if qq := miss.FindQuest(q.ID); qq != nil {
					qq.Params = q.Params
				}
				return nil
			})
		}
		if q.Status == "failed" {
			// Fresh chance for a previously failed quest: reset before claiming.
			_, _ = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
				if qq := miss.FindQuest(q.ID); qq != nil && qq.Status == "failed" {
					qq.Status = "pending"
					qq.Error = ""
				}
				return nil
			})
		}
		if _, err := moksa.ClaimQuest(missionID, q.ID, karakaID); err != nil {
			log.Printf("orchestrator: claim %s failed: %v", q.ID, err)
			stateMu.Lock()
			failed[q.ID] = true
			stateMu.Unlock()
			return
		}
		log.Printf("orchestrator: running quest %s (%s)", q.ID, q.Tool)

		// Agentic core: run → observe → on failure, ask the LLM to repair
		// the parameters and retry (max 2 repairs). This is the ReAct loop
		// at quest level from Manifest 26.
		const maxRepairs = 2
		var lastErr string
		for attempt := 0; attempt <= maxRepairs; attempt++ {
			updated, err := moksa.RunQuest(missionID, q.ID)
			if err == nil {
				if rq := updated.FindQuest(q.ID); rq != nil && rq.Status == "done" {
					// Auto-reward: trajectory filter (Mokṣa) — cheap, no LLM judge.
					_, _ = moksa.ApplyReward(missionID, q.ID, "done", 1.0, "auto: tool executed", false)
					stateMu.Lock()
					done[q.ID] = true
					stateMu.Unlock()
					return
				}
			}
			// Observe the failure
			lastErr = "unknown error"
			if err != nil {
				lastErr = err.Error()
			} else if rq := updated.FindQuest(q.ID); rq != nil && rq.Error != "" {
				lastErr = rq.Error
			}
			log.Printf("orchestrator: quest %s attempt %d failed: %s", q.ID, attempt+1, lastErr)
			if attempt == maxRepairs {
				break
			}
			// Repair: LLM proposes corrected params given the error
			if repairQuestParams(ctx, missionID, q.ID, lastErr) != nil {
				break // cannot repair — stop wasting attempts
			}
		}
		_, _ = moksa.ApplyReward(missionID, q.ID, "failed", 0, "auto: "+truncateStr(lastErr, 120), false)
		stateMu.Lock()
		failed[q.ID] = true
		stateMu.Unlock()
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
			// Nothing runnable but quests remain. Either a dependency cycle,
			// or quests blocked behind a parked (waiting_approval) ancestor —
			// in that case they must survive until the human decides.
			if cur, ok := moksa.GetMission(missionID); ok {
				hasWaiting := false
				for _, q := range cur.Quests {
					if q.Status == "waiting_approval" {
						hasWaiting = true
						break
					}
				}
				if hasWaiting {
					log.Printf("orchestrator: %d quest(s) blocked behind human approval — pausing levels", len(pending))
					break
				}
			}
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
//
//	or: { "mission_id": "m…", "karaka_id"?: … } to execute an existing board.
func orchestratorRunHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Instruction     string `json:"instruction"`
		MissionID       string `json:"mission_id"`
		KarakaID        string `json:"karaka_id"`
		MaxQuests       int    `json:"max_quests"`
		Mode            string `json:"mode"`             // "" | action | research
		PublishFeed     bool   `json:"publish_feed"`     // publish synthesis to /feed when done
		RecipientPhone  string `json:"recipient_phone"`  // target for feed publish (default: *)
		RequireApproval bool   `json:"require_approval"` // pause before each quest for human approval
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

	if !launchOrchestration(missionID, req.KarakaID, req.MaxQuests, req.Mode, nil, req.PublishFeed, req.RecipientPhone, req.RequireApproval) {
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
		"running":    orchestratorRunning,
		"mission_id": orchestratorMission,
		"since":      orchestratorSince,
		"publish_to": orchestratorPublishTo,
		"approval":   orchestratorApproval,
	})
}

// launchOrchestration starts the loop in the background (one run at a time).
// mode: "" / "action" (free planner) | "research" (fixed pipeline).
// onDone, if set, receives the final mission state — used by the self-phone
// SMS trigger to text the result back.
func launchOrchestration(missionID, karakaID string, maxQuests int, mode string, onDone func(*moksa.Mission), publishFeed bool, recipientPhone string, requireApproval bool) bool {
	if !orchestratorMu.TryLock() {
		return false
	}
	setOrchestratorState(true, missionID)
	orchestratorStateMu.Lock()
	orchestratorApproval = requireApproval
	if publishFeed {
		orchestratorPublishTo = recipientPhone
		if orchestratorPublishTo == "" {
			orchestratorPublishTo = "*"
		}
	}
	orchestratorStateMu.Unlock()
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
		if done.Judge != nil {
			msg += fmt.Sprintf("\n⚖️ Juge: %s (%.0f%%) — %s", done.Judge.Verdict, done.Judge.Score*100, truncateStr(done.Judge.Reason, 120))
		}
		if len(summarySentences) > 0 {
			msg += "\n" + strings.Join(summarySentences, "\n")
		}
		if done.Summary != "" {
			msg += "\n" + truncateStr(done.Summary, 300)
		}
		msg += "\n📄 Rapport: onglet Vault, note mission-" + done.ID
		queueSmsReply(selfPhone, msg)
	}, false, "", false)
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
	_, _, _ = insertSmsDeduped(recipient, body, ts, "outbound")
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
	sig, ts, err := signEnvelope(selfPhone, recipientPhone, content)
	if err != nil {
		log.Printf("feed-publish: signing failed: %v", err)
		return
	}
	if _, err := db.Exec(
		`INSERT INTO gafam_envelopes (author_phone, recipient_phone, content, signature, signed_ts, created_at) 
		 VALUES (?, ?, ?, ?, ?, datetime('now'))`,
		selfPhone, recipientPhone, content, sig, ts,
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

// getMissionMemoryContext searches the vault (FTS5) for past missions and
// research notes relevant to the instruction — episodic memory for the planner.
func getMissionMemoryContext(instruction string) string {
	// Keep only meaningful words (>= 4 chars) to avoid a trivial MATCH.
	words := strings.Fields(instruction)
	keep := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"'()[]{}")
		if len([]rune(w)) >= 4 {
			keep = append(keep, w)
		}
		if len(keep) >= 6 {
			break
		}
	}
	if len(keep) == 0 {
		return ""
	}
	hits, err := vaultSearch(strings.Join(keep, " "), 3)
	if err != nil || len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("🧠 Relevant past memory (vault FTS):\n")
	for _, h := range hits {
		title, _ := h["title"].(string)
		snip, _ := h["snippet"].(string)
		snip = strings.ReplaceAll(snip, "<b>", "")
		snip = strings.ReplaceAll(snip, "</b>", "")
		if title == "" {
			continue
		}
		fmt.Fprintf(&b, "  - %s: %s\n", truncateStr(title, 70), truncateStr(snip, 160))
	}
	b.WriteString("\n")
	return b.String()
}

// getVaultContext returns a summary of recent vault notes for the planner prompt.
func getVaultContext() string {
	notes, err := vaultList(5)
	if err != nil || len(notes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("📚 Recent vault memory:\n")
	for _, n := range notes {
		title, _ := n["title"].(string)
		tags, _ := n["tags"].(string)
		if title == "" {
			continue
		}
		if len(title) > 80 {
			title = title[:77] + "..."
		}
		fmt.Fprintf(&b, "  - %s", title)
		if tags != "" {
			fmt.Fprintf(&b, " [%s]", tags)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
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

	// Write markdown to sandbox (body already starts with "# instruction")
	path := "/files/research/notes/" + id + ".md"
	md := fmt.Sprintf("---\nid: %s\ntitle: %q\ntags: [%s]\n---\n%s\n", id, title, tags, body)
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
