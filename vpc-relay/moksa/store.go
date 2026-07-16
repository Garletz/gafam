package moksa

import (
	"fmt"
	"sync"
	"time"
)

var (
	storeMu sync.RWMutex
	missions = map[string]*Mission{}
	seq     int
)

func newMissionID() string {
	seq++
	return fmt.Sprintf("m%d-%d", time.Now().Unix()%100000, seq)
}

func SaveMission(m *Mission) {
	storeMu.Lock()
	defer storeMu.Unlock()
	m.UpdatedAt = time.Now().UTC()
	missions[m.ID] = m
}

func GetMission(id string) (*Mission, bool) {
	storeMu.RLock()
	defer storeMu.RUnlock()
	m, ok := missions[id]
	if !ok {
		return nil, false
	}
	// return a shallow copy pointer — callers mutate under UpdateMission
	cp := *m
	quests := make([]Quest, len(m.Quests))
	copy(quests, m.Quests)
	cp.Quests = quests
	return &cp, true
}

func UpdateMission(id string, fn func(*Mission) error) (*Mission, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	m, ok := missions[id]
	if !ok {
		return nil, fmt.Errorf("mission not found: %s", id)
	}
	if err := fn(m); err != nil {
		return nil, err
	}
	m.UpdatedAt = time.Now().UTC()
	cp := *m
	quests := make([]Quest, len(m.Quests))
	copy(quests, m.Quests)
	cp.Quests = quests
	return &cp, nil
}

func ListMissions() []Mission {
	storeMu.RLock()
	defer storeMu.RUnlock()
	out := make([]Mission, 0, len(missions))
	for _, m := range missions {
		cp := *m
		quests := make([]Quest, len(m.Quests))
		copy(quests, m.Quests)
		cp.Quests = quests
		out = append(out, cp)
	}
	return out
}

func DeleteMission(id string) bool {
	storeMu.Lock()
	defer storeMu.Unlock()
	if _, ok := missions[id]; !ok {
		return false
	}
	delete(missions, id)
	return true
}
