package main

// Contact auto-analysis — smart, incremental background enrichment.
// Instead of re-analyzing every contact on a schedule, only contacts with
// NEW SMS since their last analysis are re-run through the LLM deducer.
// A ticker runs this periodically; a kāraka tool lets the owner trigger it
// by SMS ("/q analyse mes nouveaux contacts") without waiting for the ticker.

import (
	"log"
	"time"

	"github.com/Garletz/gafam/vpc-relay/karaka"
)

// autoAnalyzeMu serialises auto-analyze runs (best-effort: skip if busy).
var autoAnalyzeMu = make(chan struct{}, 1)

// contactNeedsAnalysis reports whether a contact has SMS newer than its last
// analysis. Contacts with no SMS at all are skipped (nothing to deduce).
func contactNeedsAnalysis(phone string) bool {
	digits := onlyDigits(phone)
	suffix := digits
	if len(suffix) > 9 {
		suffix = suffix[len(suffix)-9:]
	}
	if suffix == "" {
		return false
	}
	var maxTs int64
	if err := db.QueryRow(`SELECT COALESCE(MAX(timestamp), 0) FROM gafam_sms WHERE sender LIKE '%' || ?`, suffix).Scan(&maxTs); err != nil || maxTs == 0 {
		return false
	}
	var lastTs int64
	if err := db.QueryRow(`SELECT COALESCE(last_analysis_ts, 0) FROM gafam_contacts WHERE phone = ?`, phone).Scan(&lastTs); err != nil {
		return true
	}
	return lastTs == 0 || maxTs > lastTs
}

// autoAnalyzeContacts re-analyzes (up to limit) contacts that need it.
// Returns (analyzed, skipped).
func autoAnalyzeContacts(limit int) (int, int) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(`SELECT phone FROM gafam_contacts ORDER BY name ASC`)
	if err != nil {
		return 0, 0
	}
	defer rows.Close()

	analyzed, skipped := 0, 0
	for rows.Next() {
		var phone string
		if rows.Scan(&phone) != nil {
			continue
		}
		if analyzed >= limit {
			skipped++
			continue
		}
		if !contactNeedsAnalysis(phone) {
			skipped++
			continue
		}
		if _, err := analyzeSingleContact(phone); err != nil {
			log.Printf("contacts auto-analyze: %s failed: %v", phone, err)
			skipped++
			continue
		}
		analyzed++
	}
	return analyzed, skipped
}

// runAutoAnalyzeSafe runs a cycle unless one is already in flight or an
// orchestrator mission is mid-execution (avoid piling LLM load).
func runAutoAnalyzeSafe(limit int) {
	select {
	case autoAnalyzeMu <- struct{}{}:
	default:
		return
	}
	defer func() { <-autoAnalyzeMu }()

	orchestratorStateMu.RLock()
	busy := orchestratorRunning
	orchestratorStateMu.RUnlock()
	if busy {
		log.Println("contacts auto-analyze: orchestrator busy, deferring")
		return
	}
	a, s := autoAnalyzeContacts(limit)
	if a > 0 || s > 0 {
		log.Printf("contacts auto-analyze: %d analyzed, %d skipped", a, s)
	}
}

// startAutoAnalyzeTicker runs the incremental analyzer in the background:
// a warm-up pass shortly after boot, then every 6 hours.
func startAutoAnalyzeTicker() {
	go func() {
		time.Sleep(5 * time.Minute)
		for {
			runAutoAnalyzeSafe(20)
			time.Sleep(6 * time.Hour)
		}
	}()
	log.Println("contacts auto-analyze: ticker started (every 6h, incremental)")
}

func registerContactsAutoTool() {
	karaka.RegisterTool(karaka.Tool{
		ID:          "contacts.auto_analyze",
		Description: "Analyze the contacts that have new SMS since their last analysis — deduces profession/skills/languages and updates semantic memory. Incremental: never re-analyzes unchanged contacts. Use to enrich 'qui peut m'aider' on demand.",
		Category:    "contacts",
		Params: map[string]karaka.ParamSpec{
			"limit": {Type: "int", Required: false, Description: "Max contacts to analyze this run (default 20)", Default: 20},
		},
		Returns: "{ analyzed: int, skipped: int }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			limit := 20
			if l, ok := params["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}
			a, s := autoAnalyzeContacts(limit)
			return map[string]interface{}{"analyzed": a, "skipped": s}, nil
		},
	})
	log.Println("contacts.auto_analyze tool registered")
}
