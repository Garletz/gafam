package main

import (
	"log"
	"time"
)

// Window for treating two SMS rows as the same message when the unique
// index (sender, body, timestamp) cannot catch them — e.g. live push with
// wall-clock ms vs history sync with provider date, or SMS_RECEIVED +
// SMS_DELIVER a few ms apart.
const smsDedupWindowMs int64 = 180_000 // 3 minutes

type smsRow struct {
	id        int64
	sender    string
	body      string
	timestamp int64
}

func abs64(a int64) int64 {
	if a < 0 {
		return -a
	}
	return a
}

// smsNearDuplicateID returns an existing row id if the same peer+body
// already exists within the dedup window (phone match = last 9 digits).
func smsNearDuplicateID(sender, body string, timestamp int64) (int64, bool) {
	if body == "" || timestamp == 0 {
		return 0, false
	}
	rows, err := db.Query(
		`SELECT id, sender, timestamp FROM gafam_sms
		 WHERE body = ? AND ABS(timestamp - ?) < ?
		 ORDER BY id ASC LIMIT 40`,
		body, timestamp, smsDedupWindowMs,
	)
	if err != nil {
		return 0, false
	}
	defer rows.Close()
	for rows.Next() {
		var id, ts int64
		var s string
		if err := rows.Scan(&id, &s, &ts); err != nil {
			continue
		}
		if phonesMatch(sender, s) {
			return id, true
		}
	}
	return 0, false
}

// insertSmsDeduped inserts into gafam_sms unless a near-duplicate exists.
// Returns (id, inserted, err).
func insertSmsDeduped(sender, body string, timestamp int64, status string) (int64, bool, error) {
	if timestamp == 0 {
		timestamp = time.Now().UnixMilli()
	}
	if id, ok := smsNearDuplicateID(sender, body, timestamp); ok {
		return id, false, nil
	}
	res, err := db.Exec(
		`INSERT OR IGNORE INTO gafam_sms (sender, body, timestamp, status) VALUES (?, ?, ?, ?)`,
		sender, body, timestamp, status,
	)
	if err != nil {
		return 0, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Exact unique-index hit
		if id, ok := smsNearDuplicateID(sender, body, timestamp); ok {
			return id, false, nil
		}
		return 0, false, nil
	}
	id, _ := res.LastInsertId()
	return id, true, nil
}

// purgeNearDuplicateSms removes historical near-duplicates (keeps lowest id).
// Runs once at startup so the web UI cleans itself after a VPC pull.
func purgeNearDuplicateSms() {
	rows, err := db.Query(`SELECT id, sender, body, timestamp FROM gafam_sms ORDER BY id ASC`)
	if err != nil {
		log.Printf("sms dedup purge: query failed: %v", err)
		return
	}
	defer rows.Close()

	var all []smsRow
	for rows.Next() {
		var r smsRow
		if err := rows.Scan(&r.id, &r.sender, &r.body, &r.timestamp); err != nil {
			continue
		}
		all = append(all, r)
	}

	// body → kept rows (small lists; we only retain those still in window range of scan tip)
	keptByBody := make(map[string][]smsRow, 256)
	var toDelete []int64

	for _, r := range all {
		cands := keptByBody[r.body]
		dup := false
		// Drop kept entries that can no longer collide (far in the past vs current)
		alive := cands[:0]
		for _, k := range cands {
			if abs64(r.timestamp-k.timestamp) < smsDedupWindowMs*2 {
				alive = append(alive, k)
			}
		}
		cands = alive
		for _, k := range cands {
			if phonesMatch(r.sender, k.sender) && abs64(r.timestamp-k.timestamp) < smsDedupWindowMs {
				dup = true
				break
			}
		}
		if dup {
			toDelete = append(toDelete, r.id)
		} else {
			cands = append(cands, r)
			keptByBody[r.body] = cands
		}
	}

	if len(toDelete) == 0 {
		return
	}
	deleted := 0
	for _, id := range toDelete {
		if _, err := db.Exec(`DELETE FROM gafam_sms WHERE id = ?`, id); err == nil {
			deleted++
		}
	}
	log.Printf("sms dedup purge: removed %d near-duplicate row(s)", deleted)
}
