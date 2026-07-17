package moksa

import (
	_ "embed"
	"regexp"
	"strings"
	"time"
)

//go:embed worldcard.md
var worldCardMD string

func WorldCard() string {
	return strings.TrimSpace(worldCardMD)
}

var urlRe = regexp.MustCompile(`https?://[^\s]+`)

// PoseBoard builds a heuristic quest board from an instruction (method4 v1 — no LLM).
func PoseBoard(instruction string) []Quest {
	instr := strings.TrimSpace(instruction)
	lower := strings.ToLower(instr)
	var quests []Quest
	n := 0
	next := func() string {
		n++
		return "q" + itoa(n)
	}

	url := ""
	if m := urlRe.FindString(instr); m != "" {
		url = strings.TrimRight(m, ".,);]")
	}

	// Always start with understanding the demand
	qUnderstand := next()
	quests = append(quests, Quest{
		ID:        qUnderstand,
		Title:     "Clarify demand & gather context",
		OrganHint: "suparna_vpc",
		Tool:      "sandbox.storage",
		Params:    map[string]interface{}{},
		DependsOn: nil,
		Status:    "pending",
		ETA:       5,
	})

	hasURL := url != "" || strings.Contains(lower, "http") || strings.Contains(lower, "lien") || strings.Contains(lower, "url") || strings.Contains(lower, "site") || strings.Contains(lower, "web") || strings.Contains(lower, "browser")
	hasShell := strings.Contains(lower, "exec") || strings.Contains(lower, "shell") || strings.Contains(lower, "commande") || strings.Contains(lower, "command") || strings.Contains(lower, "run ") || strings.Contains(lower, "script")
	hasFile := strings.Contains(lower, "fichier") || strings.Contains(lower, "file") || strings.Contains(lower, "lire") || strings.Contains(lower, "write") || strings.Contains(lower, "notes")
	hasLogs := strings.Contains(lower, "log") || strings.Contains(lower, "sms") || strings.Contains(lower, "suparna") || strings.Contains(lower, "journée") || strings.Contains(lower, "journee")

	var lastIDs []string
	lastIDs = append(lastIDs, qUnderstand)

	if hasLogs {
		id := next()
		quests = append(quests, Quest{
			ID:        id,
			Title:     "Check sandbox storage / activity footprint",
			OrganHint: "suparna_vpc",
			Tool:      "sandbox.file_list",
			Params:    map[string]interface{}{"path": "/files"},
			DependsOn: []string{qUnderstand},
			Status:    "pending",
			ETA:       8,
		})
		lastIDs = []string{id}
	}

	if hasURL {
		qStatus := next()
		quests = append(quests, Quest{
			ID:        qStatus,
			Title:     "Check remote browser status",
			OrganHint: "suparna_vpc",
			Tool:      "browser.status",
			Params:    map[string]interface{}{},
			DependsOn: []string{qUnderstand},
			Status:    "pending",
			ETA:       5,
		})
		qShot := next()
		params := map[string]interface{}{}
		_ = url
		quests = append(quests, Quest{
			ID:        qShot,
			Title:     "Capture browser screenshot",
			OrganHint: "edge_l2_phone",
			Tool:      "browser.screenshot",
			Params:    params,
			DependsOn: []string{qStatus},
			Status:    "pending",
			ETA:       10,
		})
		lastIDs = []string{qShot}
	}

	if hasShell {
		id := next()
		cmd := "pwd && ls -la /sandbox/files 2>/dev/null || ls -la /files 2>/dev/null || ls -la"
		if strings.Contains(lower, "whoami") {
			cmd = "whoami && uname -a"
		}
		quests = append(quests, Quest{
			ID:        id,
			Title:     "Run shell command in sandbox",
			OrganHint: "edge_l2_phone",
			Tool:      "sandbox.exec",
			Params:    map[string]interface{}{"command": cmd, "timeout": 30},
			DependsOn: []string{qUnderstand},
			Status:    "pending",
			ETA:       15,
		})
		lastIDs = []string{id}
	}

	if hasFile {
		idList := next()
		quests = append(quests, Quest{
			ID:        idList,
			Title:     "List sandbox files",
			OrganHint: "suparna_vpc",
			Tool:      "sandbox.file_list",
			Params:    map[string]interface{}{"path": "/files"},
			DependsOn: []string{qUnderstand},
			Status:    "pending",
			ETA:       5,
		})
		lastIDs = []string{idList}
	}

	// If nothing matched beyond understand, add a generic explore + judge pair
	if len(quests) == 1 {
		qList := next()
		quests = append(quests, Quest{
			ID:        qList,
			Title:     "Survey sandbox workspace",
			OrganHint: "suparna_vpc",
			Tool:      "sandbox.file_list",
			Params:    map[string]interface{}{"path": "/files"},
			DependsOn: []string{qUnderstand},
			Status:    "pending",
			ETA:       8,
		})
		qStore := next()
		quests = append(quests, Quest{
			ID:        qStore,
			Title:     "Read storage usage",
			OrganHint: "suparna_vpc",
			Tool:      "sandbox.storage",
			Params:    map[string]interface{}{},
			DependsOn: []string{qList},
			Status:    "pending",
			ETA:       5,
		})
		lastIDs = []string{qStore}
	}

	// Final judge quest (reasoning placeholder — no tool, human/supervisor)
	qJudge := next()
	quests = append(quests, Quest{
		ID:        qJudge,
		Title:     "Judge results against original demand",
		OrganHint: "edge_l2_phone",
		Tool:      "",
		Params:    nil,
		DependsOn: append([]string{}, lastIDs...),
		Status:    "pending",
		ETA:       20,
	})

	return quests
}

// CreateMissionFromInstruction poses the board and stores a new mission.
func CreateMissionFromInstruction(instruction string) *Mission {
	now := time.Now().UTC()
	m := &Mission{
		ID:          newMissionID(),
		Instruction: strings.TrimSpace(instruction),
		Quests:      PoseBoard(instruction),
		Status:      "active",
		WorldCard:   WorldCard(),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	SaveMission(m)
	return m
}

// CreateEmptyMission stores a shell mission for Saṃyojaka to plan into.
// Quests stay empty until the orchestrator LLM planner fills them.
func CreateEmptyMission(instruction string) *Mission {
	now := time.Now().UTC()
	m := &Mission{
		ID:          newMissionID(),
		Instruction: strings.TrimSpace(instruction),
		Quests:      nil,
		Status:      "planning",
		WorldCard:   WorldCard(),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	SaveMission(m)
	return m
}
