package moksa

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Garletz/gafam/vpc-relay/karaka"
)

func ClaimQuest(missionID, questID, karakaID string) (*Mission, error) {
	return UpdateMission(missionID, func(m *Mission) error {
		if m.Status == "cancelled" || m.Status == "done" {
			return fmt.Errorf("mission is %s", m.Status)
		}
		q := m.FindQuest(questID)
		if q == nil {
			return fmt.Errorf("quest not found: %s", questID)
		}
		if q.Status != "pending" && q.Status != "claimed" {
			return fmt.Errorf("quest status is %s", q.Status)
		}
		if karakaID == "" {
			return fmt.Errorf("missing karaka_id")
		}
		// Auto-pick organ hint if empty claim matches hint when possible
		q.Claim = karakaID
		q.Status = "claimed"
		return nil
	})
}

func RunQuest(missionID, questID string) (*Mission, error) {
	var runErr error
	var result interface{}

	m, err := UpdateMission(missionID, func(m *Mission) error {
		q := m.FindQuest(questID)
		if q == nil {
			return fmt.Errorf("quest not found: %s", questID)
		}
		if q.Status != "claimed" && q.Status != "failed" {
			return fmt.Errorf("quest must be claimed before run (status=%s)", q.Status)
		}
		if q.Tool == "" {
			return fmt.Errorf("quest has no tool — mark reward manually (judge quest)")
		}
		if q.Claim == "" {
			return fmt.Errorf("quest has no claim")
		}

		perm := karaka.CheckPermission(q.Claim, q.Tool)
		if perm == "deny" {
			return fmt.Errorf("permission deny: %s cannot use %s", q.Claim, q.Tool)
		}
		// ask: still allow run in v1 but annotate — human already on board
		q.Status = "running"
		return nil
	})
	if err != nil {
		return nil, err
	}

	q := m.FindQuest(questID)
	if q == nil {
		return nil, fmt.Errorf("quest not found after claim")
	}

	params := q.Params
	if params == nil {
		params = map[string]interface{}{}
	}

	result, runErr = karaka.ExecuteTool(q.Tool, params)

	return UpdateMission(missionID, func(m *Mission) error {
		q := m.FindQuest(questID)
		if q == nil {
			return fmt.Errorf("quest not found: %s", questID)
		}
		if runErr != nil {
			q.Status = "failed"
			q.Error = runErr.Error()
			q.Result = nil
			q.Reward = &Reward{Verdict: "failed", Score: 0, Reason: runErr.Error()}
			return nil
		}
		q.Status = "done"
		q.Error = ""
		q.Result = result
		return nil
	})
}

func ApplyReward(missionID, questID, verdict string, score float64, reason string, autoAdd bool) (*Mission, error) {
	verdict = strings.TrimSpace(strings.ToLower(verdict))
	switch verdict {
	case "done", "failed", "needs_more":
	default:
		return nil, fmt.Errorf("invalid verdict: %s", verdict)
	}

	return UpdateMission(missionID, func(m *Mission) error {
		q := m.FindQuest(questID)
		if q == nil {
			return fmt.Errorf("quest not found: %s", questID)
		}
		q.Reward = &Reward{Verdict: verdict, Score: score, Reason: reason}
		switch verdict {
		case "done":
			if q.Status == "running" || q.Status == "claimed" || q.Status == "pending" {
				q.Status = "done"
			}
		case "failed":
			q.Status = "failed"
		case "needs_more":
			if autoAdd {
				nid := m.NextQuestID()
				title := "Follow-up: " + q.Title
				if reason != "" {
					title = "Follow-up — " + truncate(reason, 60)
				}
				m.Quests = append(m.Quests, Quest{
					ID:        nid,
					Title:     title,
					OrganHint: q.OrganHint,
					Tool:      q.Tool,
					Params:    map[string]interface{}{},
					DependsOn: []string{q.ID},
					Status:    "pending",
					ETA:       q.ETA,
				})
			}
		}
		return nil
	})
}

func AddQuest(missionID string, title, organHint, tool string, params map[string]interface{}, dependsOn []string, eta int) (*Mission, error) {
	return UpdateMission(missionID, func(m *Mission) error {
		if m.Status == "cancelled" {
			return fmt.Errorf("mission cancelled")
		}
		if title == "" {
			return fmt.Errorf("missing title")
		}
		if eta <= 0 {
			eta = 10
		}
		if params == nil {
			params = map[string]interface{}{}
		}
		m.Quests = append(m.Quests, Quest{
			ID:        m.NextQuestID(),
			Title:     title,
			OrganHint: organHint,
			Tool:      tool,
			Params:    params,
			DependsOn: dependsOn,
			Status:    "pending",
			ETA:       eta,
		})
		return nil
	})
}

func Synthesize(missionID string) (*Mission, error) {
	m, ok := GetMission(missionID)
	if !ok {
		return nil, fmt.Errorf("mission not found: %s", missionID)
	}

	// The deliverable: the most substantial text a quest actually produced
	// (llm.chat content, browser.sense answer). It leads the report — the
	// quest table becomes a technical appendix, not the main read.
	deliverable := ""
	for _, q := range m.Quests {
		if q.Status != "done" || q.Result == nil {
			continue
		}
		if rm, ok := q.Result.(map[string]interface{}); ok {
			for _, key := range []string{"content", "answer"} {
				if s, ok := rm[key].(string); ok && len(s) > len(deliverable) {
					deliverable = s
				}
			}
		}
	}

	var b strings.Builder
	b.WriteString("# " + m.Instruction + "\n\n")
	if deliverable != "" {
		b.WriteString(deliverable + "\n\n---\n\n")
	}
	if m.Judge != nil {
		b.WriteString(fmt.Sprintf("**⚖️ Verdict:** %s (%.0f%%) — %s\n\n", m.Judge.Verdict, m.Judge.Score*100, m.Judge.Reason))
	}
	b.WriteString("## Appendix — quest log\n\n")
	b.WriteString("**ID:** " + m.ID + "\n\n")
	for _, q := range m.Quests {
		b.WriteString("### " + q.ID + " — " + q.Title + "\n")
		b.WriteString("- status: " + q.Status + "\n")
		b.WriteString("- organ: " + q.OrganHint + "\n")
		b.WriteString("- tool: " + q.Tool + "\n")
		b.WriteString("- claim: " + q.Claim + "\n")
		if q.Reward != nil {
			b.WriteString(fmt.Sprintf("- reward: %s (%.2f) — %s\n", q.Reward.Verdict, q.Reward.Score, q.Reward.Reason))
		}
		if q.Error != "" {
			b.WriteString("- error: " + q.Error + "\n")
		}
		if q.Result != nil {
			// JSON, pas du %v Go — lisible par un humain.
			if raw, err := json.Marshal(q.Result); err == nil {
				rs := string(raw)
				if len(rs) > 1500 {
					rs = rs[:1497] + "…"
				}
				b.WriteString("- result: `" + rs + "`\n")
			}
		}
		b.WriteString("\n")
	}
	report := b.String()
	path := "/files/missions/" + m.ID + "/report.md"

	// Best-effort write via sandbox tool
	_, writeErr := karaka.ExecuteTool("sandbox.file_write", map[string]interface{}{
		"path":    path,
		"content": report,
	})

	return UpdateMission(missionID, func(m *Mission) error {
		m.Summary = report
		if writeErr != nil {
			m.Summary += "\n\n_Note: sandbox write failed: " + writeErr.Error() + "_\n"
		} else {
			m.Summary += "\n\n_Written to sandbox: " + path + "_\n"
		}
		m.Status = "done"
		return nil
	})
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
