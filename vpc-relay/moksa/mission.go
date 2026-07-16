package moksa

import "time"

// Reward is a quest verdict — trajectory filter, not XP.
type Reward struct {
	Verdict string  `json:"verdict"` // done | failed | needs_more | ""
	Score   float64 `json:"score"`
	Reason  string  `json:"reason"`
}

// Quest is one cell on the mission board (method4).
type Quest struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	OrganHint string                 `json:"organ_hint"`
	Tool      string                 `json:"tool"`
	Params    map[string]interface{} `json:"params,omitempty"`
	DependsOn []string               `json:"depends_on"`
	Status    string                 `json:"status"` // pending | claimed | running | done | failed | cancelled
	Claim     string                 `json:"claim"`  // karaka_id
	ETA       int                    `json:"eta"`    // seconds estimate
	Result    interface{}            `json:"result,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Reward    *Reward                `json:"reward,omitempty"`
}

// Mission holds the quest board for one user instruction.
type Mission struct {
	ID          string    `json:"id"`
	Instruction string    `json:"instruction"`
	Quests      []Quest   `json:"quests"`
	Status      string    `json:"status"` // planning | active | synthesizing | done | cancelled
	WorldCard   string    `json:"world_card"`
	Summary     string    `json:"summary,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (m *Mission) FindQuest(qid string) *Quest {
	for i := range m.Quests {
		if m.Quests[i].ID == qid {
			return &m.Quests[i]
		}
	}
	return nil
}

func (m *Mission) NextQuestID() string {
	return "q" + itoa(len(m.Quests)+1)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
