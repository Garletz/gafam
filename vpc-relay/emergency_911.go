package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ─── 911 Emergency Cascade ────────────────────────────────────────────────
//
// Same trusted-circle principle as the recovery phone, but the alert flows
// OUTWARD: a trusted sender (guardian via its 911 code, or the self phone
// with "911") triggers a cascade. Each GAFAM node that receives a well-formed
// GAFAM911 alert relays it to ITS OWN 911-relay guardians — opt-in at every
// hop ("semi-public": each node decides its own relays).
//
// Guards against flooding:
//   - unique alert ID per cascade, each node relays at most once per ID
//   - hard hop limit (max911Hops)
//   - trigger (starting a NEW cascade) restricted to trusted senders;
//     continuation relays accept any well-formed GAFAM911 body.

const (
	gafam911Prefix = "GAFAM911"
	max911Hops     = 3
	default911Code = "911"
)

// initEmergency911Schema migrates trusted_guardians and creates the dedup table.
func initEmergency911Schema() {
	db.Exec(`ALTER TABLE trusted_guardians ADD COLUMN keyword_911 TEXT DEFAULT '911'`)
	db.Exec(`ALTER TABLE trusted_guardians ADD COLUMN relay_911 INTEGER DEFAULT 1`)
	db.Exec(`CREATE TABLE IF NOT EXISTS gafam_911_seen (
		alert_id TEXT PRIMARY KEY,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
}

// new911AlertID builds a unique cascade identifier.
func new911AlertID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixMilli())
	}
	return fmt.Sprintf("%d-%s", time.Now().UnixMilli(), hex.EncodeToString(b))
}

// mark911Seen records an alert ID and returns false if it was already seen
// (loop guard). Old entries are pruned opportunistically.
func mark911Seen(alertID string) bool {
	if alertID == "" {
		return true
	}
	res, err := db.Exec("INSERT OR IGNORE INTO gafam_911_seen (alert_id) VALUES (?)", alertID)
	if err != nil {
		return true
	}
	n, _ := res.RowsAffected()
	db.Exec(`DELETE FROM gafam_911_seen WHERE created_at < datetime('now', '-2 days')`)
	return n > 0
}

// sanitize911Text strips characters that would break the " | " wire format.
func sanitize911Text(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "/")
	return strings.TrimSpace(s)
}

// build911RelayBody renders the wire format:
//
//	GAFAM911 <alertID> <hop> <origPhone> | <origName> | <message>
func build911RelayBody(alertID string, hop int, origPhone, origName, message string) string {
	return fmt.Sprintf("%s %s %d %s | %s | %s",
		gafam911Prefix,
		alertID,
		hop,
		sanitize911Text(origPhone),
		sanitize911Text(origName),
		sanitize911Text(message),
	)
}

// parse911Relay extracts the fields of a GAFAM911 alert body.
func parse911Relay(body string) (alertID, origPhone, origName, message string, hop int, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(body), " | ", 3)
	if len(parts) < 2 {
		return "", "", "", "", 0, false
	}
	head := strings.Fields(parts[0])
	if len(head) != 4 || !strings.EqualFold(head[0], gafam911Prefix) {
		return "", "", "", "", 0, false
	}
	h, err := strconv.Atoi(head[2])
	if err != nil || h < 1 {
		return "", "", "", "", 0, false
	}
	msg := ""
	if len(parts) == 3 {
		msg = parts[2]
	}
	return head[1], head[3], parts[1], msg, h, true
}

// queue911Sms inserts a relay SMS into the outbox (the APK sends it) and
// mirrors it into the history, exactly like the recovery reply flow.
func queue911Sms(recipient, body string) {
	ts := time.Now().UnixMilli()
	if _, err := db.Exec("INSERT INTO gafam_outbox (recipient, body) VALUES (?, ?)", recipient, body); err != nil {
		log.Println("911: outbox insert failed:", err)
		return
	}
	_, _, _ = insertSmsDeduped(recipient, body, ts, "outbound")
}

// contactNameForPhone resolves a display name for the origin phone.
func contactNameForPhone(phone string) string {
	var name string
	if err := db.QueryRow("SELECT name FROM gafam_contacts WHERE phone LIKE ?", "%"+phoneDigits(phone)).Scan(&name); err == nil && name != "" {
		return name
	}
	return phone
}

func phoneDigits(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			out.WriteByte(s[i])
		}
	}
	return out.String()
}

// trigger911Cascade starts (or continues) the distress cascade: the alert is
// relayed to every guardian with relay_911 enabled, excluding the immediate
// sender. hop is the current hop count (1 for the first relay).
func trigger911Cascade(senderStr string, alertID string, hop int, origPhone, origName, message string) int {
	if alertID == "" {
		alertID = new911AlertID()
	}
	if !mark911Seen(alertID) {
		log.Printf("911: alert %s already seen, skipping", alertID)
		return 0
	}
	if hop > max911Hops {
		log.Printf("911: alert %s exceeds hop limit %d", alertID, max911Hops)
		return 0
	}

	rows, err := db.Query("SELECT phone_number, name FROM trusted_guardians WHERE relay_911 = 1")
	if err != nil {
		log.Println("911: relay list query failed:", err)
		return 0
	}
	defer rows.Close()

	body := build911RelayBody(alertID, hop, origPhone, origName, message)
	relayed := 0
	for rows.Next() {
		var p, n string
		if err := rows.Scan(&p, &n); err != nil {
			continue
		}
		if phonesMatch(p, senderStr) {
			continue
		}
		queue911Sms(p, body)
		relayed++
	}
	log.Printf("911: alert %s relayed to %d guardian(s)", alertID, relayed)
	return relayed
}

// relay911Alert handles an incoming relayed alert: dedup, hop check, then
// forward to this node's own relays with hop+1.
func relay911Alert(senderStr, body string) bool {
	alertID, origPhone, origName, message, hop, ok := parse911Relay(body)
	if !ok {
		return false
	}
	log.Printf("911: relayed alert %s hop %d received from %s", alertID, hop, senderStr)
	if !mark911Seen(alertID) {
		return true
	}
	if hop >= max911Hops {
		return true
	}
	trigger911Cascade(senderStr, alertID, hop+1, origPhone, origName, message)
	return true
}

// containsCodeWord matches a code as a whole word (boundary-safe).
func containsCodeWord(body, code string) bool {
	lower := strings.ToLower(strings.TrimSpace(body))
	c := strings.ToLower(code)
	if c == "" {
		return false
	}
	for _, tok := range strings.Fields(lower) {
		if strings.Trim(tok, ".,!?;:()[]{}") == c {
			return true
		}
	}
	return false
}

// messageAfterCode returns the free text following the trigger code.
func messageAfterCode(body, code string) string {
	idx := strings.Index(strings.ToLower(body), strings.ToLower(code))
	if idx == -1 {
		return ""
	}
	return strings.TrimSpace(body[idx+len(code):])
}

// trigger911WebHandler handles an explicit 911 emergency broadcast from the web interface.
func trigger911WebHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	selfPhone := getSetting("self_phone")
	origPhone := phoneDigits(selfPhone)
	origName := "Admin GAFAM"
	if selfPhone != "" {
		origName = contactNameForPhone(selfPhone)
	}

	alertID := new911AlertID()
	relayed := trigger911Cascade("", alertID, 1, origPhone, origName, req.Message)

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "broadcasted",
		"alert_id": alertID,
		"relayed":  relayed,
	})
}
