package main

// Research mission mode — the fixed methodology pipeline (absorbed from
// hyperresearch's method, rewritten our way, at GAFAM scale).
//
// Unlike the free planner (action missions), a research mission follows a
// canonical path: decompose → sweep (vault FIRST, then web) → digest →
// draft → adversarial critic → patch → archive. The canonical instruction
// is gospel: it is re-read verbatim at every step. Artifacts land in
// /files/research/missions/<id>/ and the final report joins the vault.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Garletz/gafam/vpc-relay/moksa"
)

const (
	researchMaxAngles  = 4
	researchMaxFetch   = 8
	researchFetchPar   = 4
	researchNoteExcerpt = 3000
	researchDigestCap   = 24000
	researchMaxClaims   = 12
	researchMaxFindings = 8
)

// ─── Step artifacts ───

type decomposeResult struct {
	Questions []string `json:"questions"`
	Sweep     []struct {
		Angle   string   `json:"angle"`
		Queries []string `json:"queries"`
	} `json:"sweep"`
}

type sweepSource struct {
	NoteID    string `json:"note_id"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	FromVault bool   `json:"from_vault"`
	Text      string `json:"text"`
}

type digestResult struct {
	Claims []struct {
		Claim     string   `json:"claim"`
		SourceIDs []string `json:"source_ids"`
	} `json:"claims"`
}

type criticResult struct {
	Findings []struct {
		Kind        string `json:"kind"`
		Detail      string `json:"detail"`
		SectionHint string `json:"section_hint"`
	} `json:"findings"`
}

// ─── Quest progress (QuestBoard live view) ───

func researchSetQuest(missionID, qid, status string, result interface{}, errStr string) {
	_, _ = moksa.UpdateMission(missionID, func(m *moksa.Mission) error {
		if q := m.FindQuest(qid); q != nil {
			q.Status = status
			q.Result = result
			q.Error = errStr
			if status == "running" || status == "claimed" {
				q.Claim = "suparna_vpc"
			}
		}
		return nil
	})
}

func researchFailMission(missionID, step string, err error) {
	log.Printf("research: mission %s failed at %s: %v", missionID, step, err)
	_, _ = moksa.UpdateMission(missionID, func(m *moksa.Mission) error {
		m.Status = "cancelled"
		m.Summary = fmt.Sprintf("Research failed at step **%s**: %v", step, err)
		for i := range m.Quests {
			if m.Quests[i].Status == "running" || m.Quests[i].Status == "claimed" {
				m.Quests[i].Status = "failed"
				m.Quests[i].Error = err.Error()
			}
		}
		return nil
	})
}

func researchChat(ctx context.Context, system, user string, maxTokens int) (string, error) {
	res, err := chatWithActiveEngine(ctx, system, user, "", maxTokens)
	if err != nil {
		return "", err
	}
	return res.Content, nil
}

// ─── Steps ───

func researchDecompose(ctx context.Context, instruction string) (*decomposeResult, error) {
	system := `You are the decompose step of a sovereign research pipeline.
Given a research instruction, output STRICT JSON:
{"questions": ["..."], "sweep": [{"angle": "...", "queries": ["..."]}]}
Rules:
- 2-4 atomic questions the research must answer.
- 2-4 sweep angles; each has 1-2 short concrete web search queries.
- Include at least one adversarial angle ("criticism of X", "limitations of X") when relevant.
- Output ONLY the JSON object.`
	raw, err := researchChat(ctx, system, "Canonical instruction:\n"+instruction, 1500)
	if err != nil {
		return nil, err
	}
	block := extractJSON(raw)
	if block == "" {
		return nil, fmt.Errorf("decompose returned no JSON")
	}
	var out decomposeResult
	if err := json.Unmarshal([]byte(block), &out); err != nil {
		return nil, fmt.Errorf("decompose JSON invalid: %w", err)
	}
	if len(out.Sweep) == 0 {
		return nil, fmt.Errorf("decompose produced no sweep angles")
	}
	if len(out.Sweep) > researchMaxAngles {
		out.Sweep = out.Sweep[:researchMaxAngles]
	}
	return &out, nil
}

// researchSweep checks the vault FIRST for each query, then web-searches and
// fetches new sources in parallel (bounded). This is where the memory pays.
func researchSweep(ctx context.Context, plan *decomposeResult) ([]sweepSource, error) {
	sources := make([]sweepSource, 0, researchMaxFetch+8)
	seenURL := map[string]bool{}
	var mu sync.Mutex

	// ── 1. Vault pass (free, instant) ──
	vaultHits := 0
	for _, angle := range plan.Sweep {
		for _, query := range angle.Queries {
			hits, err := vaultSearch(query, 3)
			if err != nil {
				continue
			}
			for _, h := range hits {
				id, _ := h["id"].(string)
				urlStr, _ := h["url"].(string)
				if id == "" || seenURL[urlStr] {
					continue
				}
				seenURL[urlStr] = true
				cached, err := vaultNoteFromCache(id)
				if err != nil {
					continue
				}
				text, _ := cached["text"].(string)
				title, _ := cached["title"].(string)
				sources = append(sources, sweepSource{
					NoteID: id, Title: title, URL: urlStr, FromVault: true,
					Text: capRunes(text, researchNoteExcerpt),
				})
				vaultHits++
			}
		}
	}
	log.Printf("research: sweep vault pass — %d sources reused from memory", vaultHits)

	// ── 2. Web pass: search → fetch new sources in parallel ──
	type fetchJob struct{ url, title string }
	jobs := make(chan fetchJob, researchMaxFetch*2)
	var wg sync.WaitGroup
	sem := make(chan struct{}, researchFetchPar)
	fetchCount := 0
	var countMu sync.Mutex

	for _, angle := range plan.Sweep {
		for _, query := range angle.Queries {
			countMu.Lock()
			if fetchCount >= researchMaxFetch {
				countMu.Unlock()
				break
			}
			countMu.Unlock()

			results, err := vaultWebSearch(ctx, query, 4)
			if err != nil {
				log.Printf("research: web search %q failed: %v", query, err)
				continue
			}
			for _, r := range results {
				countMu.Lock()
				if fetchCount >= researchMaxFetch {
					countMu.Unlock()
					break
				}
				if seenURL[r["url"]] || vaultURLKnown(r["url"]) {
					countMu.Unlock()
					continue
				}
				seenURL[r["url"]] = true
				fetchCount++
				countMu.Unlock()

				wg.Add(1)
				go func(u, t string) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					note, err := vaultFetchNote(ctx, u, "", []string{"research-sweep"})
					if err != nil {
						log.Printf("research: fetch %s failed: %v", u, err)
						return
					}
					mu.Lock()
					sources = append(sources, sweepSource{
						NoteID: note.ID, Title: note.Title, URL: note.URL,
						Text: capRunes(note.Text, researchNoteExcerpt),
					})
					mu.Unlock()
				}(r["url"], r["title"])
			}
		}
	}
	wg.Wait()
	close(jobs)

	if len(sources) == 0 {
		return nil, fmt.Errorf("sweep found zero sources (vault empty + web search/fetch failed)")
	}
	log.Printf("research: sweep collected %d sources (%d from vault)", len(sources), vaultHits)
	return sources, nil
}

func researchDigest(ctx context.Context, instruction string, sources []sweepSource) (*digestResult, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Canonical instruction:\n%s\n\nSources:\n", instruction)
	total := 0
	for _, s := range sources {
		chunk := fmt.Sprintf("\n--- [%s] %s (%s)\n%s\n", s.NoteID, s.Title, s.URL, s.Text)
		if total+len(chunk) > researchDigestCap {
			break
		}
		b.WriteString(chunk)
		total += len(chunk)
	}

	system := `You are the digest step of a sovereign research pipeline.
Given the canonical instruction and the sources, extract key claims as STRICT JSON:
{"claims": [{"claim": "...", "source_ids": ["n..."]}]}
Rules:
- Max ` + fmt.Sprint(researchMaxClaims) + ` claims, only claims supported by the sources.
- Each claim cites its source note ids.
- Output ONLY the JSON object.`

	raw, err := researchChat(ctx, system, b.String(), 2500)
	if err != nil {
		return nil, err
	}
	block := extractJSON(raw)
	if block == "" {
		return nil, fmt.Errorf("digest returned no JSON")
	}
	var out digestResult
	if err := json.Unmarshal([]byte(block), &out); err != nil {
		return nil, fmt.Errorf("digest JSON invalid: %w", err)
	}
	if len(out.Claims) == 0 {
		return nil, fmt.Errorf("digest produced zero claims")
	}
	return &out, nil
}

func researchDraft(ctx context.Context, instruction string, digest *digestResult) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Canonical instruction:\n%s\n\nEvidence digest:\n", instruction)
	for i, c := range digest.Claims {
		fmt.Fprintf(&b, "%d. %s [%s]\n", i+1, c.Claim, strings.Join(c.SourceIDs, ", "))
	}

	system := `You are the drafting step of a sovereign research pipeline.
Write a research report in markdown answering the canonical instruction EXACTLY — it is gospel.
Rules:
- Use only the digest claims; cite sources inline as [n<id>].
- Structure: ## Findings (direct answers), ## Details, ## Open questions.
- Max 1200 words. No filler.`
	return researchChat(ctx, system, b.String(), 4000)
}

func researchCritic(ctx context.Context, instruction, draft string, digest *digestResult) (*criticResult, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Canonical instruction:\n%s\n\nDraft:\n%s\n\nEvidence digest:\n", instruction, draft)
	for i, c := range digest.Claims {
		fmt.Fprintf(&b, "%d. %s [%s]\n", i+1, c.Claim, strings.Join(c.SourceIDs, ", "))
	}

	system := `You are the adversarial critic of a sovereign research pipeline.
Review the draft against the canonical instruction and the digest. Output STRICT JSON:
{"findings": [{"kind": "missing|wrong|unsupported|off_prompt", "detail": "...", "section_hint": "..."}]}
Rules:
- Max ` + fmt.Sprint(researchMaxFindings) + ` findings; be ruthless but concrete.
- missing = an aspect of the instruction the draft ignores.
- wrong = contradicted by the digest. unsupported = no claim backs it. off_prompt = drifts from the instruction.
- If the draft is sound, output {"findings": []}.
- Output ONLY the JSON object.`

	raw, err := researchChat(ctx, system, b.String(), 1500)
	if err != nil {
		return nil, err
	}
	block := extractJSON(raw)
	if block == "" {
		return &criticResult{}, nil // unparseable critic = non-blocking
	}
	var out criticResult
	if err := json.Unmarshal([]byte(block), &out); err != nil {
		return &criticResult{}, nil
	}
	return &out, nil
}

func researchPatch(ctx context.Context, instruction, draft string, critic *criticResult) (string, error) {
	if len(critic.Findings) == 0 {
		return draft, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Canonical instruction:\n%s\n\nDraft to patch:\n%s\n\nCritic findings:\n", instruction, draft)
	for i, f := range critic.Findings {
		fmt.Fprintf(&b, "%d. [%s] %s (section: %s)\n", i+1, f.Kind, f.Detail, f.SectionHint)
	}

	system := `You are the patcher of a sovereign research pipeline.
You receive the draft and the critic findings. Apply the findings SURGICALLY:
- rewrite only the affected sections; preserve everything else verbatim.
- never regenerate the whole report from scratch.
- keep [n<id>] citations.
Output the FULL patched report in markdown (untouched sections unchanged).`
	return researchChat(ctx, system, b.String(), 4000)
}

// ─── Archive ───

func researchWriteFile(ctx context.Context, path, content string) error {
	putURL := vaultSandboxURL() + path
	req, err := http.NewRequestWithContext(ctx, "PUT", putURL, strings.NewReader(content))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/markdown")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sandbox_not_running — cannot write %s", path)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("write %s: sandbox HTTP %d", path, resp.StatusCode)
	}
	return nil
}

// researchIndexReport makes the final report searchable in the vault.
func researchIndexReport(missionID, path, instruction, report string) {
	title := capRunes(instruction, 80)
	_, _ = db.Exec(
		`INSERT INTO research_notes (id, title, url, tags, body, fetched_at, path, suggested_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"report-"+missionID, "Report: "+title, "mission://"+missionID,
		"report research", report, time.Now().UTC().Format(time.RFC3339), path, "",
	)
}

// ─── The pipeline ───

func runResearchPipeline(ctx context.Context, missionID string) {
	log.Printf("research: pipeline started for mission %s", missionID)

	m, ok := moksa.GetMission(missionID)
	if !ok {
		return
	}
	instruction := m.Instruction

	// Mark mode + create the progress quests.
	_, _ = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
		miss.Mode = "research"
		miss.Status = "active"
		return nil
	})
	steps := []struct{ id, title, tool string }{
		{"q1", "Decompose the research question", "llm.chat"},
		{"q2", "Sweep sources (vault first, then web)", "research.fetch"},
		{"q3", "Evidence digest", "llm.chat"},
		{"q4", "Draft the report", "llm.chat"},
		{"q5", "Adversarial critic", "llm.chat"},
		{"q6", "Patch the draft", "llm.chat"},
		{"q7", "Archive & report", "sandbox.file_write"},
	}
	for i, s := range steps {
		var deps []string
		if i > 0 {
			deps = []string{steps[i-1].id}
		}
		_, _ = moksa.AddQuest(missionID, s.title, "suparna_vpc", s.tool, map[string]interface{}{}, deps, 30)
	}

	dir := vaultMissionsDir + "/" + missionID

	// ── q1 Decompose ──
	researchSetQuest(missionID, "q1", "running", nil, "")
	plan, err := researchDecompose(ctx, instruction)
	if err != nil {
		researchSetQuest(missionID, "q1", "failed", nil, err.Error())
		researchFailMission(missionID, "decompose", err)
		return
	}
	researchSetQuest(missionID, "q1", "done", map[string]interface{}{
		"questions": plan.Questions, "angles": len(plan.Sweep),
	}, "")

	// ── q2 Sweep ──
	researchSetQuest(missionID, "q2", "running", nil, "")
	sources, err := researchSweep(ctx, plan)
	if err != nil {
		researchSetQuest(missionID, "q2", "failed", nil, err.Error())
		researchFailMission(missionID, "sweep", err)
		return
	}
	vaultN := 0
	for _, s := range sources {
		if s.FromVault {
			vaultN++
		}
	}
	researchSetQuest(missionID, "q2", "done", map[string]interface{}{
		"sources": len(sources), "from_vault": vaultN,
	}, "")

	// ── q3 Digest ──
	researchSetQuest(missionID, "q3", "running", nil, "")
	digest, err := researchDigest(ctx, instruction, sources)
	if err != nil {
		researchSetQuest(missionID, "q3", "failed", nil, err.Error())
		researchFailMission(missionID, "digest", err)
		return
	}
	researchSetQuest(missionID, "q3", "done", map[string]interface{}{"claims": len(digest.Claims)}, "")

	// ── q4 Draft ──
	researchSetQuest(missionID, "q4", "running", nil, "")
	draft, err := researchDraft(ctx, instruction, digest)
	if err != nil {
		researchSetQuest(missionID, "q4", "failed", nil, err.Error())
		researchFailMission(missionID, "draft", err)
		return
	}
	researchSetQuest(missionID, "q4", "done", map[string]interface{}{"words": len(strings.Fields(draft))}, "")

	// ── q5 Critic ──
	researchSetQuest(missionID, "q5", "running", nil, "")
	critic, err := researchCritic(ctx, instruction, draft, digest)
	if err != nil {
		log.Printf("research: critic failed (non-blocking): %v", err)
		critic = &criticResult{}
	}
	researchSetQuest(missionID, "q5", "done", map[string]interface{}{"findings": len(critic.Findings)}, "")

	// ── q6 Patch ──
	researchSetQuest(missionID, "q6", "running", nil, "")
	report, err := researchPatch(ctx, instruction, draft, critic)
	if err != nil {
		log.Printf("research: patch failed, keeping draft: %v", err)
		report = draft
	}
	researchSetQuest(missionID, "q6", "done", nil, "")

	// ── q7 Archive ──
	researchSetQuest(missionID, "q7", "running", nil, "")
	var digestMD strings.Builder
	for i, c := range digest.Claims {
		fmt.Fprintf(&digestMD, "%d. %s [%s]\n", i+1, c.Claim, strings.Join(c.SourceIDs, ", "))
	}
	var criticMD strings.Builder
	for i, f := range critic.Findings {
		fmt.Fprintf(&criticMD, "%d. [%s] %s (%s)\n", i+1, f.Kind, f.Detail, f.SectionHint)
	}
	reportPath := dir + "/report.md"
	archiveOK := true
	for _, f := range []struct{ path, body string }{
		{dir + "/query.md", instruction + "\n"},
		{dir + "/digest.md", digestMD.String()},
		{dir + "/draft.md", draft},
		{dir + "/critics.md", criticMD.String()},
		{reportPath, report},
	} {
		if err := researchWriteFile(ctx, f.path, f.body); err != nil {
			log.Printf("research: archive %s failed: %v", f.path, err)
			archiveOK = false
		}
	}
	researchIndexReport(missionID, reportPath, instruction, report)
	researchSetQuest(missionID, "q7", "done", map[string]interface{}{"archived": archiveOK}, "")

	// ── Done ──
	_, _ = moksa.UpdateMission(missionID, func(miss *moksa.Mission) error {
		miss.Status = "done"
		miss.Summary = fmt.Sprintf("Research report — %d sources (%d from vault), %d claims, %d critic findings.\n\n%s\n\n_Archive: %s_",
			len(sources), vaultN, len(digest.Claims), len(critic.Findings), report, dir)
		return nil
	})
	log.Printf("research: pipeline done for mission %s", missionID)
}

func capRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
