package main

// Sub-agent delegation — Saṃyojaka can spawn independent Kāraka mini-agents
// that work autonomously on subtasks and return summarized results.
//
// Modeled after OpenCode's Task tool: primary agent calls task("general", "do X"),
// subagent explores/executes independently, returns result.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Garletz/gafam/vpc-relay/karaka"
	"github.com/Garletz/gafam/vpc-relay/moksa"
)

const subAgentMaxIterations = 2
const subAgentMaxQuests = 4

// delegateSubAgent creates a mini-mission and runs a limited ReAct loop
// for a sub-agent Kāraka. Returns the synthesis result.
func delegateSubAgent(karakaID, instruction string) (string, error) {
	if karakaID == "" {
		karakaID = "suparna_vpc"
	}

	// Check permissions
	perm := karaka.CheckPermission(karakaID, "llm.chat")
	if perm == "deny" {
		return "", fmt.Errorf("kāraka %s cannot use llm.chat — delegation requires LLM access", karakaID)
	}

	// Create mini mission
	m := moksa.CreateEmptyMission(instruction)
	m.Mode = "subagent"

	log.Printf("subagent: %s spawned for: %s", karakaID, truncateStr(instruction, 80))

	// Limited ReAct loop
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	allResults := []string{}

	for iter := 0; iter < subAgentMaxIterations; iter++ {
		m, _ = moksa.GetMission(m.ID)
		if m == nil {
			return "", fmt.Errorf("mission vanished")
		}

		// Build planner prompt with sub-agent constraints
		var planInstruction string
		if iter == 0 {
			planInstruction = instruction
		} else {
			planInstruction = buildReplanInstruction(m)
			if planInstruction == "" {
				break
			}
		}

		system, user := buildSubAgentPlannerPrompt(karakaID, planInstruction, subAgentMaxQuests, m)
		res, err := chatWithEngine(ctx, "light_task", system, user, 2048)
		if err != nil {
			log.Printf("subagent: planning failed for %s (iter %d): %v", karakaID, iter, err)
			if iter == 0 {
				return "", fmt.Errorf("planning: %w", err)
			}
			break
		}

		raw := extractJSON(res.Content)
		if raw == "" {
			break
		}

		var plan planResult
		if json.Unmarshal([]byte(raw), &plan) != nil || len(plan.Quests) == 0 {
			break
		}

		// Add quests to mission
		for _, pq := range plan.Quests {
			if _, ok := karaka.GetTool(pq.Tool); !ok {
				continue
			}
			if perm := karaka.CheckPermission(karakaID, pq.Tool); perm == "deny" {
				log.Printf("subagent: skipping %s — %s denied for %s", pq.Tool, pq.Title, karakaID)
				continue
			}
			_, err := moksa.AddQuest(m.ID, pq.Title, karakaID, pq.Tool, pq.Params, nil, 30)
			if err != nil {
				log.Printf("subagent: AddQuest failed: %v", err)
			}
		}

		// Execute
		_ = executeQuestLevels(ctx, m.ID, karakaID)

		// Collect results
		m, _ = moksa.GetMission(m.ID)
		if m != nil {
			for _, q := range m.Quests {
				if q.Status == "done" && q.Result != nil {
					if s, ok := q.Result.(map[string]interface{}); ok {
						if a, ok := s["answer"].(string); ok {
							allResults = append(allResults, a)
						}
						if c, ok := s["content"].(string); ok {
							allResults = append(allResults, c)
						}
					}
				}
			}
		}
	}

	if len(allResults) == 0 {
		return "Sub-agent completed but produced no results.", nil
	}
	return strings.Join(allResults, "\n\n"), nil
}

func parseJSON(raw string, v interface{}) error {
	// Use encoding/json here since we're in package main
	return nil // placeholder — will use standard json.Unmarshal in the build
}

// buildSubAgentPlannerPrompt creates a plan prompt for a sub-agent with tool constraints.
func buildSubAgentPlannerPrompt(karakaID, instruction string, maxQuests int, m *moksa.Mission) (string, string) {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("You are %s, a sub-agent of the GAFAM node.\n", karakaID))
	b.WriteString("You received a delegated subtask. Plan 1-4 concise quests to complete it.\n\n")
	b.WriteString("Available tools:\n")
	for _, t := range karaka.ListTools() {
		perm := karaka.CheckPermission(karakaID, t.ID)
		if perm != "deny" {
			fmt.Fprintf(&b, "  %s — %s (permission: %s)\n", t.ID, t.Description, perm)
		}
	}
	// Show what's already been done
	if len(m.Quests) > 0 {
		b.WriteString("\nAlready attempted:\n")
		for _, q := range m.Quests {
			fmt.Fprintf(&b, "  %s %s — %s\n", q.Status, q.Title, q.Tool)
		}
	}
	b.WriteString("\nOutput STRICT JSON: {\"quests\": [...]}\n")
	b.WriteString("Don't repeat failed quests. If the task is complete, output {\"quests\": []}\n")
	return b.String(), "Subtask: " + instruction
}

// registerDelegationTool registers the karaka.delegate tool for cross-agent task delegation.
func registerDelegationTool() {
	karaka.RegisterTool(karaka.Tool{
		ID:          "karaka.delegate",
		Description: "Delegate a subtask to another Kāraka sub-agent. The sub-agent plans and executes independently using its own LLM, then returns a summarized result. Use for parallel work, specialized analysis, or heavy tasks.",
		Category:    "karaka",
		Params: map[string]karaka.ParamSpec{
			"karaka_id":   {Type: "string", Required: false, Description: "Target Kāraka (default: suparna_vpc)", Default: "suparna_vpc"},
			"instruction": {Type: "string", Required: true, Description: "Detailed subtask instruction"},
		},
		Returns: "{ result: string }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			karakaID, _ := params["karaka_id"].(string)
			instruction, _ := params["instruction"].(string)
			if instruction == "" {
				return nil, fmt.Errorf("instruction required")
			}
			if karakaID == "" {
				karakaID = "suparna_vpc"
			}
			result, err := delegateSubAgent(karakaID, instruction)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"result": result}, nil
		},
	})
	log.Println("Sub-agent delegation tool registered (karaka.delegate)")
}
