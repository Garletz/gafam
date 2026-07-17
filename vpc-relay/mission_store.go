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
		if err != nil {
			log.Printf("moksa: persist failed for %s: %v", m.ID, err)
		}
	}
	moksa.DeleteHook = func(id string) {
		_, _ = db.Exec(`DELETE FROM moksa_missions WHERE id = ?`, id)
	}

	loadMissionsFromDB()
}

// loadMissionsFromDB hydrates the RAM store at boot and marks missions that
// were mid-run when the relay stopped (restart / watchtower update) as
// interrupted — no phantom "running" quests left behind.
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

	interrupted := 0
	for i := range list {
		m := &list[i]
		if m.Status != "planning" && m.Status != "active" && m.Status != "synthesizing" {
			continue
		}
		for j := range m.Quests {
			if m.Quests[j].Status == "running" || m.Quests[j].Status == "claimed" {
				m.Quests[j].Status = "failed"
				m.Quests[j].Error = "relay restarted mid-run"
			}
		}
		m.Status = "cancelled"
		m.Summary += "\n\n_Interrupted by relay restart — pose again or re-run._"
		interrupted++
		if moksa.PersistHook != nil {
			moksa.PersistHook(m)
		}
	}

	moksa.LoadIntoStore(list)
	log.Printf("moksa: %d missions restored from DB (%d interrupted)", len(list), interrupted)
}
