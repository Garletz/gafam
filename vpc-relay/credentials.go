package main

// Credential Vault + TOTP + Email connector — vpc-relay/credentials.go
//
// Encrypted storage for passwords, TOTP secrets, and email settings.
// Everything is encrypted at rest with AES-256-GCM (same as settings).

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

// ─── Types ───

type Credential struct {
	ID             int64  `json:"id"`
	Site           string `json:"site"`
	Username       string `json:"username"`
	PasswordHint   string `json:"password_hint,omitempty"`
	TOTPHint       string `json:"totp_hint,omitempty"`
	Notes          string `json:"notes,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// ─── DB Table ───

func initCredentialTables() {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS gafam_credentials (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site TEXT NOT NULL,
		username TEXT DEFAULT '',
		password_enc TEXT DEFAULT '',
		totp_secret_enc TEXT DEFAULT '',
		notes TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		log.Printf("credentials: table init failed: %v", err)
	}
}

// ─── TOTP (RFC 6238 — same as Google Authenticator) ───

// totpCode generates a 6-digit TOTP code from a base32-encoded secret.
// periodSeconds is the time window (default 30s).
func totpCode(secretB32 string, periodSeconds int64) string {
	if periodSeconds <= 0 {
		periodSeconds = 30
	}
	secret, err := base32.StdEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(secretB32)))
	if err != nil {
		return ""
	}
	unix := time.Now().Unix()
	counter := uint64(unix / periodSeconds)

	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, secret)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset]&0x7f)<<24 |
		uint32(sum[offset+1]&0xff)<<16 |
		uint32(sum[offset+2]&0xff)<<8 |
		uint32(sum[offset+3]&0xff)) % 1000000

	return fmt.Sprintf("%06d", code)
}

// ─── Encryption helpers ───

func sealCredentialSecret(plaintext string) string {
	if plaintext == "" {
		return ""
	}
	// Use same key derivation as settings: derive from JWT_SECRET
	key := pbkdf2.Key([]byte(string(jwtSecret)), []byte("gafam-credentials"), 100000, 32, sha1.New)
	encrypted, iv, err := encryptAESGCM(key, []byte(plaintext))
	if err != nil {
		return ""
	}
	return "seal:" + iv + ":" + encrypted
}

func unsealCredentialSecret(blob string) string {
	if blob == "" || !strings.HasPrefix(blob, "seal:") {
		return ""
	}
	rest := blob[5:]
	idx := strings.Index(rest, ":")
	if idx < 0 {
		return ""
	}
	iv := rest[:idx]
	encrypted := rest[idx+1:]
	key := pbkdf2.Key([]byte(string(jwtSecret)), []byte("gafam-credentials"), 100000, 32, sha1.New)
	plaintext, err := decryptAESGCM(key, encrypted, iv)
	if err != nil {
		return ""
	}
	return string(plaintext)
}

// ─── Handlers (session-protected) ───

func credentialsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		site := r.URL.Query().Get("site")
		if site != "" {
			// Get specific credential
			var c Credential
			err := db.QueryRow(
				`SELECT id, site, username, notes, created_at, updated_at
				 FROM gafam_credentials WHERE site = ?`, site,
			).Scan(&c.ID, &c.Site, &c.Username, &c.Notes, &c.CreatedAt, &c.UpdatedAt)
			if err != nil {
				sendJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
				return
			}
			sendJSON(w, http.StatusOK, c)
			return
		}
		// List all (masked)
		rows, err := db.Query(`SELECT id, site, username, notes, created_at, updated_at FROM gafam_credentials ORDER BY site`)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
			return
		}
		defer rows.Close()
		creds := []Credential{}
		for rows.Next() {
			var c Credential
			rows.Scan(&c.ID, &c.Site, &c.Username, &c.Notes, &c.CreatedAt, &c.UpdatedAt)
			creds = append(creds, c)
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{"credentials": creds})

	case http.MethodPost:
		var in struct {
			ID          int64  `json:"id"`
			Site        string `json:"site"`
			Username    string `json:"username"`
			Password    string `json:"password"`
			TOTPSecret  string `json:"totp_secret"`
			Notes       string `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Site == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "site required"})
			return
		}
		in.Site = strings.ToLower(strings.TrimSpace(in.Site))

		if in.ID > 0 {
			// Update existing — keep old values if empty
			var oldPass, oldTotp string
			db.QueryRow(`SELECT password_enc, totp_secret_enc FROM gafam_credentials WHERE id = ?`, in.ID).Scan(&oldPass, &oldTotp)
			if in.Password == "" {
				in.Password = oldPass
			} else {
				in.Password = sealCredentialSecret(in.Password)
			}
			if in.TOTPSecret == "" {
				in.TOTPSecret = oldTotp
			} else {
				in.TOTPSecret = sealCredentialSecret(in.TOTPSecret)
			}
			_, err := db.Exec(
				`UPDATE gafam_credentials SET site=?, username=?, password_enc=?, totp_secret_enc=?, notes=?, updated_at=datetime('now') WHERE id=?`,
				in.Site, in.Username, in.Password, in.TOTPSecret, in.Notes, in.ID,
			)
			if err != nil {
				sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			log.Printf("credentials: updated %s", in.Site)
		} else {
			// Insert new
			_, err := db.Exec(
				`INSERT INTO gafam_credentials (site, username, password_enc, totp_secret_enc, notes) VALUES (?, ?, ?, ?, ?)`,
				in.Site, in.Username, sealCredentialSecret(in.Password), sealCredentialSecret(in.TOTPSecret), in.Notes,
			)
			if err != nil {
				sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			log.Printf("credentials: added %s", in.Site)
		}
		sendJSON(w, http.StatusOK, map[string]string{"site": in.Site})

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
			return
		}
		_, err := db.Exec(`DELETE FROM gafam_credentials WHERE id = ?`, id)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		sendJSON(w, http.StatusOK, map[string]string{"deleted": id})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// credentialsOTPHandler generates a TOTP code for a credential.
func credentialsOTPHandler(w http.ResponseWriter, r *http.Request) {
	site := r.URL.Query().Get("site")
	if site == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing site"})
		return
	}
	site = strings.ToLower(site)
	var totpEnc string
	err := db.QueryRow(`SELECT totp_secret_enc FROM gafam_credentials WHERE site = ?`, site).Scan(&totpEnc)
	if err != nil || totpEnc == "" {
		sendJSON(w, http.StatusNotFound, map[string]string{"error": "no TOTP secret for this site"})
		return
	}
	secret := unsealCredentialSecret(totpEnc)
	if secret == "" {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to decrypt TOTP secret"})
		return
	}
	code := totpCode(secret, 30)
	if code == "" {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "TOTP generation failed"})
		return
	}
	sendJSON(w, http.StatusOK, map[string]string{"site": site, "code": code})
}

// credentialsPasswordHandler retrieves a decrypted password for a site.
func credentialsPasswordHandler(w http.ResponseWriter, r *http.Request) {
	site := r.URL.Query().Get("site")
	if site == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing site"})
		return
	}
	site = strings.ToLower(site)
	var passEnc string
	err := db.QueryRow(`SELECT password_enc FROM gafam_credentials WHERE site = ?`, site).Scan(&passEnc)
	if err != nil || passEnc == "" {
		sendJSON(w, http.StatusNotFound, map[string]string{"error": "no password for this site"})
		return
	}
	pass := unsealCredentialSecret(passEnc)
	if pass == "" {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to decrypt password"})
		return
	}
	sendJSON(w, http.StatusOK, map[string]string{"site": site, "password": pass})
}

// ─── Email Connector (IMAP polling for verification codes) ───

type EmailSettings struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Enabled  bool   `json:"enabled"`
}

type EmailMessage struct {
	ID      string `json:"id"`
	From    string `json:"from"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Date    string `json:"date"`
	Codes   []string `json:"codes,omitempty"` // extracted OTP codes
}

func initEmailTables() {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS gafam_email_inbox (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		message_id TEXT UNIQUE,
		from_addr TEXT,
		subject TEXT,
		body TEXT,
		codes TEXT DEFAULT '',
		received_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		log.Printf("email: table init failed: %v", err)
	}
}

// extractOTPCodes finds 4-8 digit codes in text (common verification patterns).
func extractOTPCodes(text string) []string {
	patterns := []string{
		`\b(\d{4,8})\b`,                              // standalone 4-8 digit numbers
		`code[\s:]+(\d{4,8})`,                        // "code 123456" or "code: 1234"
		`verification[\s:]+(\d{4,8})`,                // "verification 123456"
		`OTP[\s:]+(\d{4,8})`,                         // "OTP 123456"
		`password[\s:]+(\d{4,8})`,                    // "password 123456"
		`one[- ]time[\s:]+(\d{4,8})`,                 // "one-time 123456"
		`confirme[\s:]+(\d{4,8})`,                    // French "confirme 123456"
		`confirmez[\s:]+(\d{4,8})`,                   // French "confirmez 123456"
	}
	codes := []string{}
	seen := map[string]bool{}
	for _, pat := range patterns {
		matches := findAllRegex(pat, text)
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				codes = append(codes, m)
			}
		}
	}
	return codes
}

func findAllRegex(pattern, text string) []string {
	re := compileRegex(pattern)
	matches := re.FindAllStringSubmatch(text, -1)
	var out []string
	for _, m := range matches {
		if len(m) > 1 {
			out = append(out, m[1])
		} else if len(m) > 0 {
			out = append(out, m[0])
		}
	}
	return out
}

func compileRegex(pattern string) *regexp.Regexp {
	re, _ := regexp.Compile(pattern)
	return re
}

// emailSettingsHandler manages email account settings.
func emailSettingsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		raw := getSetting("email_settings")
		if raw == "" {
			sendJSON(w, http.StatusOK, EmailSettings{})
			return
		}
		var s EmailSettings
		json.Unmarshal([]byte(raw), &s)
		s.Password = "" // mask password
		sendJSON(w, http.StatusOK, s)

	case http.MethodPost:
		var in EmailSettings
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if in.Host == "" || in.Username == "" || in.Password == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "host, username, password required"})
			return
		}
		if in.Port <= 0 {
			in.Port = 993
		}
		raw, _ := json.Marshal(in)
		setSetting("email_settings", string(raw))
		log.Printf("email: settings saved for %s@%s:%d", in.Username, in.Host, in.Port)
		sendJSON(w, http.StatusOK, map[string]string{"status": "saved"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// emailInboxHandler returns recent emails with extracted codes.
func emailInboxHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, from_addr, subject, codes, received_at FROM gafam_email_inbox ORDER BY received_at DESC LIMIT 50`)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()
	msgs := []EmailMessage{}
	for rows.Next() {
		var m EmailMessage
		var codesStr string
		rows.Scan(&m.ID, &m.From, &m.Subject, &codesStr, &m.Date)
		json.Unmarshal([]byte(codesStr), &m.Codes)
		msgs = append(msgs, m)
	}
	sendJSON(w, http.StatusOK, map[string]interface{}{"inbox": msgs})
}

// emailFetchHandler fetches recent emails via IMAP (on-demand).
func emailFetchHandler(w http.ResponseWriter, r *http.Request) {
	// Read settings
	raw := getSetting("email_settings")
	if raw == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "email not configured"})
		return
	}
	var s EmailSettings
	json.Unmarshal([]byte(raw), &s)

	if s.Host == "" || s.Username == "" || s.Password == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "email not configured"})
		return
	}

	// IMAP connection
	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("cannot connect: %v", err)})
		return
	}
	defer conn.Close()

	// IMAP protocol handshake
	buf := make([]byte, 4096)
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	readIMAPResponse(conn, buf) // server greeting

	// LOGIN
	writeIMAP(conn, "a LOGIN "+s.Username+" "+s.Password)
	if !readIMAPResponse(conn, buf) {
		sendJSON(w, http.StatusForbidden, map[string]string{"error": "IMAP login failed"})
		return
	}

	// SELECT INBOX
	writeIMAP(conn, "a SELECT INBOX")
	if !readIMAPResponse(conn, buf) {
		sendJSON(w, http.StatusForbidden, map[string]string{"error": "IMAP select failed"})
		return
	}

	// SEARCH UNSEEN (or RECENT)
	writeIMAP(conn, "a SEARCH RECENT")
	resp := readIMAPResponseFull(conn, buf)

	// Parse message IDs from SEARCH response
	ids := parseIMAPSearchResponse(resp)
	if len(ids) == 0 {
		sendJSON(w, http.StatusOK, map[string]interface{}{"messages": []EmailMessage{}, "count": 0})
		return
	}

	// FETCH first 5 messages
	maxFetch := 5
	if len(ids) < maxFetch {
		maxFetch = len(ids)
	}
	msgs := []EmailMessage{}
	for i := 0; i < maxFetch; i++ {
		msg := fetchIMAPMessage(conn, buf, ids[i])
		if msg.Subject != "" || msg.Body != "" {
			msg.Codes = extractOTPCodes(msg.Body)
			if len(msg.Codes) > 0 || i < 3 { // keep first 3 even without codes
				msgs = append(msgs, msg)
			}
		}
	}

	// LOGOUT
	writeIMAP(conn, "a LOGOUT")
	conn.Close()

	// Store in DB
	for _, m := range msgs {
		codesJSON, _ := json.Marshal(m.Codes)
		db.Exec(`INSERT OR REPLACE INTO gafam_email_inbox (message_id, from_addr, subject, body, codes, received_at)
			VALUES (?, ?, ?, ?, ?, datetime('now'))`,
			m.ID, m.From, m.Subject, m.Body, string(codesJSON))
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{"messages": msgs, "count": len(msgs)})
}

// IMAP helpers
func writeIMAP(conn net.Conn, cmd string) {
	conn.Write([]byte(cmd + "\r\n"))
}

func readIMAPResponse(conn net.Conn, buf []byte) bool {
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		return false
	}
	line := string(buf[:n])
	return strings.HasPrefix(strings.ToUpper(line), "A OK") || strings.Contains(strings.ToUpper(line), "* OK")
}

func readIMAPResponseFull(conn net.Conn, buf []byte) string {
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var full strings.Builder
	for {
		n, err := conn.Read(buf)
		if err != nil || n == 0 {
			break
		}
		chunk := string(buf[:n])
		full.WriteString(chunk)
		if strings.Contains(strings.ToUpper(chunk), "A OK") || strings.Contains(strings.ToUpper(chunk), "A NO") || strings.Contains(strings.ToUpper(chunk), "A BAD") {
			break
		}
	}
	return full.String()
}

func parseIMAPSearchResponse(resp string) []string {
	lines := strings.Split(resp, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "* SEARCH") {
			fields := strings.Fields(line)
			var ids []string
			for i, f := range fields {
				if i > 1 && isNumeric(f) {
					ids = append(ids, f)
				}
			}
			return ids
		}
	}
	return nil
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func fetchIMAPMessage(conn net.Conn, buf []byte, id string) EmailMessage {
	writeIMAP(conn, fmt.Sprintf("a FETCH %s (BODY[HEADER.FIELDS (FROM SUBJECT)] BODY[TEXT])", id))
	resp := readIMAPResponseFull(conn, buf)

	msg := EmailMessage{ID: id}
	// Simple header parse
	lines := strings.Split(resp, "\n")
	for _, line := range lines {
		lower := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(lower, "from:") {
			msg.From = strings.TrimSpace(line[5:])
		}
		if strings.HasPrefix(lower, "subject:") {
			msg.Subject = strings.TrimSpace(line[8:])
		}
	}
	// Extract body (simplified: everything after blank line)
	parts := strings.SplitN(resp, "\r\n\r\n", 2)
	if len(parts) == 2 {
		msg.Body = strings.TrimSpace(parts[1])
		if len(msg.Body) > 5000 {
			msg.Body = msg.Body[:5000]
		}
	}
	return msg
}

// emailNotifHandler receives email notifications from the Android APK
// (forwarded by EmailNotificationListener via Gmail/Outlook notifications).
func emailNotifHandler(w http.ResponseWriter, r *http.Request) {
	bodyBytes, _ := io.ReadAll(r.Body)
	r.Body.Close()

	key := deriveKey(string(jwtSecret))

	var payload EncryptedPayload
	var data map[string]interface{}
	if json.Unmarshal(bodyBytes, &payload) == nil && payload.EncryptedData != "" {
		plaintext, err := decryptAESGCM(key, payload.EncryptedData, payload.IV)
		if err == nil {
			json.Unmarshal(plaintext, &data)
		}
	}
	if data == nil {
		json.Unmarshal(bodyBytes, &data)
	}

	if data == nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	from, _ := data["app"].(string)
	subject, _ := data["title"].(string)
	body, _ := data["body"].(string)
	timestamp, _ := data["timestamp"].(float64)

	codes := extractOTPCodes(body)
	codesJSON, _ := json.Marshal(codes)

	messageID := fmt.Sprintf("andr-%s-%d", from, int64(timestamp))
	db.Exec(`INSERT OR REPLACE INTO gafam_email_inbox (message_id, from_addr, subject, body, codes, received_at)
		VALUES (?, ?, ?, ?, ?, datetime('now'))`,
		messageID, from, subject, body, string(codesJSON))

	log.Printf("email: received notification from %s: %s (codes: %v)", from, subject, codes)
	sendJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "codes": codes})
}
