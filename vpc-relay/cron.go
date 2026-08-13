package main

// Cron — scheduled Saṃyojaka missions (the missing "in time" dimension).
// Jobs live in SQLite; a ticker wakes every minute and launches due
// missions through the normal orchestrator (same mutex, same pipeline,
// same SMS feedback if the job was created with a notify phone).

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Garletz/gafam/vpc-relay/moksa"
)

type CronJob struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Instruction  string `json:"instruction"`
	Mode         string `json:"mode"` // "" (action) | research
	EveryMinutes int    `json:"every_minutes"`
	Enabled      bool   `json:"enabled"`
	NotifyPhone  string `json:"notify_phone,omitempty"` // SMS the result here (empty = silent)
	LastRun      int64  `json:"last_run,omitempty"`
	CreatedAt    int64  `json:"created_at"`
}

func initCron() {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS gafam_cron (
		id TEXT PRIMARY KEY,
		name TEXT,
		instruction TEXT,
		mode TEXT DEFAULT '',
		every_minutes INTEGER,
		enabled INTEGER DEFAULT 1,
		notify_phone TEXT DEFAULT '',
		last_run INTEGER DEFAULT 0,
		created_at INTEGER
	)`); err != nil {
		log.Printf("cron: table creation failed: %v", err)
		return
	}
	go cronTicker()
	log.Println("cron: scheduler started (60s tick)")
}

func cronTicker() {
	tick := time.NewTicker(60 * time.Second)
	defer tick.Stop()
	for range tick.C {
		runDueCronJobs()
	}
}

func runDueCronJobs() {
	rows, err := db.Query(`SELECT id, name, instruction, mode, every_minutes, notify_phone, last_run
		FROM gafam_cron WHERE enabled = 1`)
	if err != nil {
		log.Printf("cron: list failed: %v", err)
		return
	}
	defer rows.Close()

	type dueJob struct {
		id, name, instruction, mode, notifyPhone string
		everyMinutes                             int
		lastRun                                  int64
	}
	var jobs []dueJob
	for rows.Next() {
		var j dueJob
		if err := rows.Scan(&j.id, &j.name, &j.instruction, &j.mode, &j.everyMinutes, &j.notifyPhone, &j.lastRun); err == nil {
			jobs = append(jobs, j)
		}
	}

	now := time.Now().Unix()
	for _, j := range jobs {
		if j.everyMinutes <= 0 {
			continue
		}
		if now-j.lastRun < int64(j.everyMinutes)*60 {
			continue
		}
		// Mark as run BEFORE launching (avoid double-fire on next tick if busy).
		if _, err := db.Exec(`UPDATE gafam_cron SET last_run = ? WHERE id = ?`, now, j.id); err != nil {
			log.Printf("cron: update last_run %s failed: %v", j.id, err)
			continue
		}
		log.Printf("cron: firing job %s (%s)", j.id, j.name)
		m := moksa.CreateEmptyMission(j.instruction)
		_, _ = moksa.UpdateMission(m.ID, func(miss *moksa.Mission) error {
			miss.Mode = "cron"
			return nil
		})
		notify := j.notifyPhone
		if notify == "self" {
			notify = getSelfPhone()
		}
		ok := launchOrchestration(m.ID, "suparna_vpc", 6, j.mode, func(done *moksa.Mission) {
			if notify == "" {
				return
			}
			total := len(done.Quests)
			msg := fmt.Sprintf("GAFAM ⏰ cron %s done: %s — %d quests. %s",
				j.name, done.Status, total, truncateStr(done.Summary, 300))
			queueSmsReply(notify, msg)
		}, false, "", false)
		if !ok {
			log.Printf("cron: orchestrator busy — job %s skipped this cycle", j.id)
			moksa.DeleteMission(m.ID)
		}
	}
}

// ─── HTTP handlers (session-protected) ───

// cronHandler — GET (list) / POST (create) / DELETE (?id=)
func cronHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := db.Query(`SELECT id, name, instruction, mode, every_minutes, enabled, notify_phone, last_run, created_at
			FROM gafam_cron ORDER BY created_at DESC`)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		jobs := []CronJob{}
		for rows.Next() {
			var j CronJob
			var enabled int
			if err := rows.Scan(&j.ID, &j.Name, &j.Instruction, &j.Mode, &j.EveryMinutes, &enabled, &j.NotifyPhone, &j.LastRun, &j.CreatedAt); err == nil {
				j.Enabled = enabled == 1
				jobs = append(jobs, j)
			}
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{"jobs": jobs})

	case http.MethodPost:
		var in CronJob
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if in.Instruction == "" || in.EveryMinutes < 5 {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "instruction required, every_minutes >= 5"})
			return
		}
		if in.Mode != "" && in.Mode != "research" && in.Mode != "action" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid mode"})
			return
		}
		if in.Mode == "action" {
			in.Mode = ""
		}
		if in.ID == "" {
			in.ID = fmt.Sprintf("cron-%d", time.Now().UnixMilli())
		}
		if in.Name == "" {
			in.Name = truncateStr(in.Instruction, 40)
		}
		enabled := 0
		if in.Enabled {
			enabled = 1
		}
		in.CreatedAt = time.Now().Unix()
		if _, err := db.Exec(
			`INSERT INTO gafam_cron (id, name, instruction, mode, every_minutes, enabled, notify_phone, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET name=excluded.name, instruction=excluded.instruction,
			   mode=excluded.mode, every_minutes=excluded.every_minutes, enabled=excluded.enabled,
			   notify_phone=excluded.notify_phone`,
			in.ID, in.Name, in.Instruction, in.Mode, in.EveryMinutes, enabled, in.NotifyPhone, in.CreatedAt,
		); err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("cron: job upserted: %s (%s, every %dm, enabled=%v)", in.ID, in.Name, in.EveryMinutes, in.Enabled)
		sendJSON(w, http.StatusOK, map[string]interface{}{"job": in})

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
			return
		}
		res, err := db.Exec(`DELETE FROM gafam_cron WHERE id = ?`, id)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			sendJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
			return
		}
		sendJSON(w, http.StatusOK, map[string]string{"deleted": id})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
