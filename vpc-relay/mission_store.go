package main

// Mission persistence — boards are RAM-fast but durable in SQLite.
// Write-through hooks on every save; hydration + interruption recovery at boot.

import (
	"encoding/json"
	"log"
	"time"

	"github.com/Garletz/gafam/vpc-relay/moksa"
)

func initMissionStore() {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS moksa_missions (
		id TEXT PRIMARY KEY,
		status TEXT,
		mode TEXT,
		instruction TEXT,
		data TEXT,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		log.Printf("moksa: failed to create missions table: %v", err)
		return
	}

	moksa.PersistHook = func(m *moksa.Mission) {
		raw, err := json.Marshal(m)
		if err != nil {
			log.Printf("moksa: persist marshal failed for %s: %v", m.ID, err)
			return
		}
		// Retry on SQLITE_BUSY — concurrent quests can lock the DB
		for attempt := 0; attempt < 5; attempt++ {
			_, err = db.Exec(
				`INSERT INTO moksa_missions (id, status, mode, instruction, data, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?)
				 ON CONFLICT(id) DO UPDATE SET
				   status = excluded.status,
				   mode = excluded.mode,
				   instruction = excluded.instruction,
				   data = excluded.data,
				   updated_at = excluded.updated_at`,
				m.ID, m.Status, m.Mode, m.Instruction, string(raw), m.UpdatedAt.UTC().Format(time.RFC3339),
			)
			if err == nil {
				return
			}
			time.Sleep(time.Duration(50*(attempt+1)) * time.Millisecond)
		}
		log.Printf("moksa: persist failed for %s: %v", m.ID, err)
	}
	moksa.DeleteHook = func(id string) {
		_, _ = db.Exec(`DELETE FROM moksa_missions WHERE id = ?`, id)
	}

	loadMissionsFromDB()
}

// loadMissionsFromDB hydrates the RAM store at boot and recovers missions that
// were mid-run when the relay stopped (restart / watchtower update): their
// running/claimed quests go back to pending, the most recent one is resumed
// automatically by Saṃyojaka, older ones are parked as "interrupted" and can
// be resumed manually (orchestrator/run with mission_id).
func loadMissionsFromDB() {
	rows, err := db.Query(`SELECT data FROM moksa_missions`)
	if err != nil {
		log.Printf("moksa: load failed: %v", err)
		return
	}
	defer rows.Close()

	var list []moksa.Mission
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var m moksa.Mission
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			continue
		}
		list = append(list, m)
	}

	resumable := 0
	for i := range list {
		m := &list[i]
		if m.Status != "planning" && m.Status != "active" && m.Status != "synthesizing" {
			continue
		}
		// Mid-flight quests become pending again — tools are re-runnable.
		for j := range m.Quests {
			if m.Quests[j].Status == "running" || m.Quests[j].Status == "claimed" {
				m.Quests[j].Status = "pending"
				m.Quests[j].Error = ""
			}
		}
		m.Status = "interrupted"
		m.Summary += "\n\n_Interrupted by relay restart._"
		resumable++
		if moksa.PersistHook != nil {
			moksa.PersistHook(m)
		}
	}

	moksa.LoadIntoStore(list)
	log.Printf("moksa: %d missions restored from DB (%d interrupted)", len(list), resumable)
}

// resumeInterruptedMissions auto-resumes the most recent interrupted mission.
// MUST be called after the kāraka tool registry is populated (tools would be
// unknown otherwise). Older interrupted missions stay parked — resumable
// manually via orchestrator/run with mission_id.
func resumeInterruptedMissions() {
	var newest *moksa.Mission
	for _, m := range moksa.ListMissions() {
		m := m // copy from list
		if m.Status != "interrupted" {
			continue
		}
		if newest == nil || m.UpdatedAt.After(newest.UpdatedAt) {
			newest = &m
		}
	}
	if newest == nil {
		return
	}
	log.Printf("moksa: auto-resuming mission %s after restart", newest.ID)
	if !launchOrchestration(newest.ID, "suparna_vpc", 6, "", nil, false, "", false) {
		log.Printf("moksa: auto-resume of %s deferred (orchestrator busy)", newest.ID)
	}
}
