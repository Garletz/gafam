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

// Persistence hooks — set by the host (package main) to make boards durable
// in SQLite. RAM stays the working store; the DB is the durable copy.
var (
	PersistHook func(m *Mission)
	DeleteHook  func(id string)
)

func newMissionID() string {
	seq++
	return fmt.Sprintf("m%d-%d", time.Now().Unix()%100000, seq)
}

func SaveMission(m *Mission) {
	storeMu.Lock()
	m.UpdatedAt = time.Now().UTC()
	missions[m.ID] = m
	hook := PersistHook
	storeMu.Unlock()
	if hook != nil {
		hook(m)
	}
}

// LoadIntoStore hydrates the in-memory store at boot.
func LoadIntoStore(list []Mission) {
	storeMu.Lock()
	defer storeMu.Unlock()
	for i := range list {
		m := list[i]
		missions[m.ID] = &m
	}
	// Keep the id sequence above anything restored to avoid collisions.
	seq = len(missions)
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
	m, ok := missions[id]
	if !ok {
		storeMu.Unlock()
		return nil, fmt.Errorf("mission not found: %s", id)
	}
	if err := fn(m); err != nil {
		storeMu.Unlock()
		return nil, err
	}
	m.UpdatedAt = time.Now().UTC()
	hook := PersistHook
	cp := *m
	quests := make([]Quest, len(m.Quests))
	copy(quests, m.Quests)
	cp.Quests = quests
	storeMu.Unlock()
	if hook != nil {
		hook(m)
	}
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
	if _, ok := missions[id]; !ok {
		storeMu.Unlock()
		return false
	}
	delete(missions, id)
	hook := DeleteHook
	storeMu.Unlock()
	if hook != nil {
		hook(id)
	}
	return true
}
