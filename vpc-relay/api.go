package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	mrand "math/rand"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

// --- Crypto Helpers ---

func deriveKey(secret string) []byte {
	hash := sha256.Sum256([]byte(secret))
	return hash[:]
}

// phoneToSubdomain normalizes MSISDN to the gafam.cloud subdomain form (e.g. 0628782725).
func phoneToSubdomain(phone string) string {
	digits := regexp.MustCompile(`[^0-9]`).ReplaceAllString(phone, "")
	if strings.HasPrefix(digits, "33") && len(digits) >= 11 {
		return "0" + digits[2:]
	}
	return digits
}

// pbkdf2Key derives a key using PBKDF2-HMAC-SHA256 (via x/crypto/pbkdf2).
func pbkdf2Key(password, salt []byte, iterations, keyLen int) []byte {
	return pbkdf2.Key(password, salt, iterations, keyLen, sha256.New)
}

func encryptAESGCM(key []byte, plaintext []byte) (string, string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	iv := make([]byte, aesgcm.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return "", "", err
	}
	ciphertext := aesgcm.Seal(nil, iv, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), base64.StdEncoding.EncodeToString(iv), nil
}

func decryptAESGCM(key []byte, encryptedBase64 string, ivBase64 string) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedBase64)
	if err != nil {
		return nil, err
	}
	iv, err := base64.StdEncoding.DecodeString(ivBase64)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(iv) != aesgcm.NonceSize() {
		return nil, errors.New("invalid IV length")
	}
	return aesgcm.Open(nil, iv, ciphertext, nil)
}

// sealSettingsValue encrypts a plaintext settings value at rest with AES-256-GCM.
// Returns a "seal:"-prefixed envelope that unsealSettingsValue can decrypt.
func sealSettingsValue(plaintext []byte) (string, error) {
	key := deriveKey(string(jwtSecret))
	encrypted, iv, err := encryptAESGCM(key, plaintext)
	if err != nil {
		return "", err
	}
	return "seal:" + iv + ":" + encrypted, nil
}

// unsealSettingsValue decrypts a value sealed with sealSettingsValue.
// Returns nil error if the value is not sealed (plaintext backward-compat).
func unsealSettingsValue(blob string) ([]byte, error) {
	if !strings.HasPrefix(blob, "seal:") {
		return nil, errors.New("not sealed")
	}
	rest := blob[5:]
	idx := strings.Index(rest, ":")
	if idx < 0 {
		return nil, errors.New("invalid seal format")
	}
	iv := rest[:idx]
	encrypted := rest[idx+1:]
	key := deriveKey(string(jwtSecret))
	return decryptAESGCM(key, encrypted, iv)
}

// --- Outbox Handlers ---

type OutboxParams struct {
	Recipient string `json:"recipient"`
	Body      string `json:"body"`
}

func queueOutboxHandler(w http.ResponseWriter, r *http.Request) {
	bodyBytes, _ := io.ReadAll(r.Body)
	r.Body.Close()

	token := r.URL.Query().Get("token")
	if token == "" {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				token = parts[1]
			}
		}
	}

	key := deriveKey(token)
	var params OutboxParams

	// Try encrypted envelope first (legacy/inner app-layer)
	var encrypted EncryptedPayload
	if json.Unmarshal(bodyBytes, &encrypted) == nil && encrypted.EncryptedData != "" {
		plaintext, decErr := decryptAESGCM(key, encrypted.EncryptedData, encrypted.IV)
		if decErr == nil {
			json.Unmarshal(plaintext, &params)
		}
	}

	// Fallback: plain JSON (used when E2E transport layer already decrypted)
	if params.Body == "" {
		json.Unmarshal(bodyBytes, &params)
	}

	if params.Body == "" {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	stmt := `INSERT INTO gafam_outbox (recipient, body) VALUES (?, ?)`
	res, err := db.Exec(stmt, params.Recipient, params.Body)
	if err != nil {
		http.Error(w, "Failed to save to outbox", http.StatusInternalServerError)
		return
	}

	// Persist in chat history so web UI still shows the message after reload
	ts := time.Now().UnixMilli()
	_, _ = db.Exec(
		`INSERT INTO gafam_sms (sender, body, timestamp, status) VALUES (?, ?, ?, ?)`,
		params.Recipient, params.Body, ts, "outbound",
	)

	id, _ := res.LastInsertId()
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"status": "queued",
		"id":     id,
	})
}

func getOutboxHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, recipient, body, created_at FROM gafam_outbox ORDER BY created_at ASC")
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var outboxList []map[string]interface{}
	for rows.Next() {
		var id int
		var recipient, body, createdAt string
		if err := rows.Scan(&id, &recipient, &body, &createdAt); err == nil {
			outboxList = append(outboxList, map[string]interface{}{
				"id":         id,
				"recipient":  recipient,
				"body":       body,
				"created_at": createdAt,
			})
		}
	}

	if outboxList == nil {
		outboxList = []map[string]interface{}{}
	}

	jsonData, err := json.Marshal(outboxList)
	if err != nil {
		http.Error(w, "Failed to marshal JSON", http.StatusInternalServerError)
		return
	}

	key := deriveKey(string(jwtSecret))
	encryptedBase64, ivBase64, err := encryptAESGCM(key, jsonData)
	if err != nil {
		http.Error(w, "Encryption failed", http.StatusInternalServerError)
		return
	}

	touchApkRelay()
	sendJSON(w, http.StatusOK, EncryptedPayload{
		EncryptedData: encryptedBase64,
		IV:            ivBase64,
	})
}

func deleteOutboxHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	_, err := db.Exec("DELETE FROM gafam_outbox WHERE id = ?", id)
	if err != nil {
		http.Error(w, "Failed to delete from outbox", http.StatusInternalServerError)
		return
	}

	touchApkRelay()
	sendJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Handlers

func pingHandler(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type PairDeviceParams struct {
	DeviceName string `json:"device_name"`
	DeviceID   string `json:"device_id"`
}

func pairDeviceHandler(w http.ResponseWriter, r *http.Request) {
	var params PairDeviceParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Check if this is the first device
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM gafam_devices").Scan(&count)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	isPrimary := 0
	if count == 0 {
		isPrimary = 1
	}

	// UPSERT into gafam_devices
	stmt := `
	INSERT INTO gafam_devices (device_name, device_id, is_primary) 
	VALUES (?, ?, ?) 
	ON CONFLICT(device_id) DO UPDATE SET is_primary = is_primary`
	
	_, err = db.Exec(stmt, params.DeviceName, params.DeviceID, isPrimary)
	if err != nil {
		http.Error(w, "Failed to pair device", http.StatusInternalServerError)
		return
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "paired",
		"is_primary": isPrimary == 1,
	})
}

type SmsParams struct {
	Sender    *string `json:"sender"`
	Body      *string `json:"body"`
	Timestamp *int64  `json:"timestamp"`
}

type EncryptedPayload struct {
	EncryptedData string `json:"encrypted_data"`
	IV            string `json:"iv"`
}

func smsHandler(w http.ResponseWriter, r *http.Request) {
	var payload EncryptedPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	key := deriveKey(string(jwtSecret))
	plaintext, err := decryptAESGCM(key, payload.EncryptedData, payload.IV)
	if err != nil {
		http.Error(w, "Decryption failed", http.StatusForbidden)
		return
	}

	var params SmsParams
	if err := json.Unmarshal(plaintext, &params); err != nil {
		http.Error(w, "Invalid decrypted JSON payload", http.StatusBadRequest)
		return
	}

	var senderStr, bodyStr string
	if params.Sender != nil {
		senderStr = *params.Sender
	}
	if params.Body != nil {
		bodyStr = *params.Body
	}

	// Self-phone remote quest trigger (Saṃyojaka via SMS, Settings → self_phone)
	if instr, mode, isSelfQuest := selfQuestInstruction(senderStr, bodyStr); isSelfQuest {
		triggerSelfQuest(senderStr, instr, mode)
	}

	// Emergency Recovery Check
	var guardianKeyword string
	
	sClean := regexp.MustCompile("[^0-9]").ReplaceAllString(senderStr, "")
	sMatch := sClean
	if len(sClean) >= 9 {
		sMatch = sClean[len(sClean)-9:]
	}

	rows, errGuard := db.Query("SELECT phone_number, keyword FROM trusted_guardians")
	if errGuard == nil {
		defer rows.Close()
		for rows.Next() {
			var p, k string
			if err := rows.Scan(&p, &k); err == nil {
				pClean := regexp.MustCompile("[^0-9]").ReplaceAllString(p, "")
				pMatch := pClean
				if len(pClean) >= 9 { pMatch = pClean[len(pClean)-9:] }

				if pMatch != "" && sMatch != "" && pMatch == sMatch {
					guardianKeyword = k
					break
				}
			}
		}
	}
	if guardianKeyword != "" && strings.Contains(strings.ToLower(bodyStr), strings.ToLower(guardianKeyword)) {
		log.Printf("EMERGENCY RECOVERY TRIGGERED by %s", senderStr)
		// Assuming 'phone' is the relay phone. Wait, the relay phone number is not strictly available here except from the db?
		// Actually, the web login needs the relay's phone number.
		// Let's get the relay's phone number from gafam_sessions or contacts?
		// We can just query `gafam_settings` or similar if needed.
		// Wait! `generateEmergencyChallenge` deposits for `relayPhone`.
		var relayPhone string
		// Since there is only one relay per VPC usually, we can just use the phone that is registered.
		// Let's fetch the phone of the owner from gafam_contacts where is_verified = 1? No, from gafam_settings or similar.
		// Wait! The user accesses `[phone].gafam.cloud`. 
		// Actually `depositSafeOnCloudflare` takes `phone`. Is there a setting for `relayPhone`?
		errPhone := db.QueryRow("SELECT phone FROM gafam_sessions LIMIT 1").Scan(&relayPhone)
		if errPhone == nil && relayPhone != "" {
			cTime, cClicks, errChal := generateEmergencyChallenge(relayPhone)
			if errChal == nil {
				tNorm := strings.ReplaceAll(cTime, ":", "")
				loginURL := fmt.Sprintf("https://%s.gafam.cloud/?t=%s", phoneToSubdomain(relayPhone), tNorm)
				replyBody := fmt.Sprintf(
					"Code GAFAM: %s - %d impulsions\nCliquez sur le lien, attendez la fin du timer, puis faites %d impulsions.\n%s",
					cTime, cClicks, cClicks, loginURL,
				)
				db.Exec(`INSERT INTO gafam_outbox (recipient, body) VALUES (?, ?)`, senderStr, replyBody)
				ts := time.Now().UnixMilli()
				db.Exec(
					`INSERT INTO gafam_sms (sender, body, timestamp, status) VALUES (?, ?, ?, ?)`,
					senderStr, replyBody, ts, "outbound",
				)
			} else {
				log.Println("Error generating emergency challenge:", errChal)
			}
		} else {
			log.Println("Cannot find relay phone for emergency challenge")
		}
	}

	// Anti-Spam: Check if sender is a verified contact
	var isVerified int
	errContact := db.QueryRow("SELECT id FROM gafam_contacts WHERE phone LIKE ?", "%"+sMatch).Scan(&isVerified)
	
	status := "purgatory"
	if errContact == nil {
		status = "inbox"
	}

	stmt := `INSERT INTO gafam_sms (sender, body, timestamp, status) VALUES (?, ?, ?, ?)`
	res, err := db.Exec(stmt, params.Sender, params.Body, params.Timestamp, status)
	if err != nil {
		http.Error(w, "Failed to save SMS", http.StatusInternalServerError)
		return
	}

	// Rolling Window: Keep max 50,000 SMS
	db.Exec(`DELETE FROM gafam_sms WHERE id NOT IN (SELECT id FROM gafam_sms ORDER BY id DESC LIMIT 50000)`)

	id, _ := res.LastInsertId()
	touchApkRelay()
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "saved",
		"id":          id,
		"spam_status": status,
	})
}

// POST /api/auth/sms/sync — bulk import recent SMS from phone history
type HistSmsMsg struct {
	Address   string `json:"address"`
	Body      string `json:"body"`
	Timestamp int64  `json:"timestamp"`
	Type      int    `json:"type"` // 1=inbox, 2=sent
}

func syncSmsHistoryHandler(w http.ResponseWriter, r *http.Request) {
	var payload EncryptedPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	key := deriveKey(string(jwtSecret))
	plaintext, err := decryptAESGCM(key, payload.EncryptedData, payload.IV)
	if err != nil {
		http.Error(w, "Decryption failed", http.StatusForbidden)
		return
	}

	var wrap struct {
		Messages []HistSmsMsg `json:"messages"`
	}
	if err := json.Unmarshal(plaintext, &wrap); err != nil {
		http.Error(w, "Invalid decrypted JSON payload", http.StatusBadRequest)
		return
	}
	if len(wrap.Messages) > 2000 {
		http.Error(w, "Too many messages", http.StatusBadRequest)
		return
	}

	inserted := 0
	for _, m := range wrap.Messages {
		if m.Address == "" || m.Body == "" || m.Timestamp == 0 {
			continue
		}
		status := "inbox"
		if m.Type == 2 {
			status = "outbound"
		}
		res, err := db.Exec(
			`INSERT OR IGNORE INTO gafam_sms (sender, body, timestamp, status) VALUES (?, ?, ?, ?)`,
			m.Address, m.Body, m.Timestamp, status,
		)
		if err != nil {
			continue
		}
		n, _ := res.RowsAffected()
		inserted += int(n)
	}

	log.Printf("SMS history sync: %d new / %d received", inserted, len(wrap.Messages))
	touchApkRelay()
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "synced",
		"received": len(wrap.Messages),
		"inserted": inserted,
	})
}

func getSmsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, sender, body, timestamp, created_at, status FROM gafam_sms ORDER BY timestamp DESC")
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var smsList []map[string]interface{}
	for rows.Next() {
		var id int
		var sender, body, createdAt, status string
		var timestamp int64
		if err := rows.Scan(&id, &sender, &body, &timestamp, &createdAt, &status); err == nil {
			direction := "inbound"
			if status == "outbound" || status == "sent" {
				direction = "outbound"
			}
			smsList = append(smsList, map[string]interface{}{
				"id":         id,
				"sender":     sender,
				"body":       body,
				"timestamp":  timestamp,
				"created_at": createdAt,
				"status":     status,
				"direction":  direction,
			})
			if direction == "inbound" {
				if codes := detectSmsCodes(body); len(codes) > 0 {
					smsList[len(smsList)-1]["codes"] = codes
				}
			}
		}
	}

	if smsList == nil {
		smsList = []map[string]interface{}{}
	}

	jsonData, err := json.Marshal(smsList)
	if err != nil {
		http.Error(w, "Failed to marshal JSON", http.StatusInternalServerError)
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				token = parts[1]
			}
		}
	}

	key := deriveKey(token)
	encryptedBase64, ivBase64, err := encryptAESGCM(key, jsonData)
	if err != nil {
		http.Error(w, "Encryption failed", http.StatusInternalServerError)
		return
	}

	sendJSON(w, http.StatusOK, EncryptedPayload{
		EncryptedData: encryptedBase64,
		IV:            ivBase64,
	})
}

// --- Legacy Web Client Auth Handlers (kept for backward compat) ---


// getPublicIP fetches the VPC's external IPv4 address
func getPublicIP() string {
	resp, err := http.Get("https://api.ipify.org")
	if err != nil {
		return "127.0.0.1"
	}
	defer resp.Body.Close()
	ip, err := io.ReadAll(resp.Body)
	if err != nil {
		return "127.0.0.1"
	}
	return string(ip)
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, `{"error":"Missing token"}`, http.StatusBadRequest)
		return
	}
	_, err := db.Exec(`DELETE FROM gafam_sessions WHERE session_token = ?`, token)
	if err != nil {
		http.Error(w, `{"error":"Failed to delete session"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}



// ============================================================
// === RENDEZ-VOUS SYNCHRONE MÉCANIQUE (Manifest 12) ===
// ============================================================

// challengeAuthHandler is called by the APK when the user programs a challenge.
// It receives the challenge parameters, encrypts the VPC info, and deposits the
// encrypted "safe" on Cloudflare.
func challengeAuthHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone           string `json:"phone"`
		ChallengeTime   string `json:"challengeTime"`   // e.g. "1836"
		ChallengeClicks int    `json:"challengeClicks"` // e.g. 4
		TtlMinutes      int    `json:"ttlMinutes"`      // 0 = eternal
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Phone == "" || req.ChallengeTime == "" || req.ChallengeClicks < 1 || req.ChallengeClicks > 8 {
		http.Error(w, "Invalid challenge parameters", http.StatusBadRequest)
		return
	}

	// 1. Generate a unique session token
	sessionID := generateToken(32)
	sessionToken := generateToken(64)

	var expiresAtStr *string
	if req.TtlMinutes > 0 {
		t := time.Now().Add(time.Duration(req.TtlMinutes) * time.Minute).Format("2006-01-02 15:04:05")
		expiresAtStr = &t
	}

	_, err := db.Exec(`INSERT INTO gafam_sessions (session_id, phone, status, session_token, created_at, device_confirmed_at, expires_at) VALUES (?, ?, 'confirmed', ?, datetime('now'), datetime('now'), ?)`,
		sessionID, req.Phone, sessionToken, expiresAtStr)
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	// 2. Build the safe payload
	port := os.Getenv("PORT")
	if port == "" {
		port = "5150"
	}
	publicIP := getPublicIP()

	safePayload := map[string]string{
		"sessionToken": sessionToken,
		"vpcUrl":       fmt.Sprintf("http://%s:%s", publicIP, port),
	}
	safeJSON, _ := json.Marshal(safePayload)

	// 3. Derive AES key via PBKDF2 from "ChallengeTime-ChallengeClicks"
	passphrase := fmt.Sprintf("%s-%d", req.ChallengeTime, req.ChallengeClicks)
	salt := make([]byte, 16)
	rand.Read(salt)

	aesKey := pbkdf2Key([]byte(passphrase), salt, 500000, 32)

	// 4. Encrypt the safe with AES-256-GCM
	encryptedSafe, ivBase64, err := encryptAESGCM(aesKey, safeJSON)
	if err != nil {
		http.Error(w, "Encryption failed", http.StatusInternalServerError)
		return
	}

	saltBase64 := base64.StdEncoding.EncodeToString(salt)

	// 5. Deposit the encrypted safe on Cloudflare using the normalized time
	go depositSafeOnCloudflare(req.Phone, encryptedSafe, saltBase64, ivBase64, req.ChallengeTime)

	log.Printf("Challenge created for %s: time=%s clicks=%d", req.Phone, req.ChallengeTime, req.ChallengeClicks)

	sendJSON(w, http.StatusOK, map[string]string{
		"status":         "challenge_created",
		"challengeTime":  req.ChallengeTime,
	})
}

// generateEmergencyChallenge generates a challenge autonomously for Social Recovery
func generateEmergencyChallenge(phone string) (string, int, error) {
	// e.g. target time is in 2 minutes
	loc, errLoc := time.LoadLocation("Europe/Paris")
	var target time.Time
	if errLoc == nil {
		target = time.Now().In(loc).Add(2 * time.Minute)
	} else {
		// Fallback to UTC+2 if tzdata is missing in the Docker container
		target = time.Now().Add(2 * time.Hour).Add(2 * time.Minute)
	}
	challengeTimeStr := target.Format("15:04")
	challengeTimeNorm := target.Format("1504")
	challengeClicks := mrand.Intn(8) + 1

	sessionID := generateToken(32)
	sessionToken := generateToken(64)
	
	t := time.Now().Add(15 * time.Minute).Format("2006-01-02 15:04:05")
	expiresAtStr := &t

	_, err := db.Exec(`INSERT INTO gafam_sessions (session_id, phone, status, session_token, created_at, device_confirmed_at, expires_at) VALUES (?, ?, 'confirmed', ?, datetime('now'), datetime('now'), ?)`,
		sessionID, phone, sessionToken, expiresAtStr)
	if err != nil {
		return "", 0, err
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "5150"
	}
	publicIP := getPublicIP()

	safePayload := map[string]string{
		"sessionToken": sessionToken,
		"vpcUrl":       fmt.Sprintf("http://%s:%s", publicIP, port),
	}
	safeJSON, _ := json.Marshal(safePayload)

	passphrase := fmt.Sprintf("%s-%d", challengeTimeNorm, challengeClicks)
	salt := make([]byte, 16)
	rand.Read(salt)

	aesKey := pbkdf2Key([]byte(passphrase), salt, 500000, 32)
	encryptedSafe, ivBase64, err := encryptAESGCM(aesKey, safeJSON)
	if err != nil {
		return "", 0, err
	}

	saltBase64 := base64.StdEncoding.EncodeToString(salt)
	go depositSafeOnCloudflare(phone, encryptedSafe, saltBase64, ivBase64, challengeTimeNorm)

	return challengeTimeStr, challengeClicks, nil
}

// depositSafeOnCloudflare sends the encrypted safe to the Cloudflare directory
func depositSafeOnCloudflare(phone, encryptedSafe, salt, iv, accessTime string) {
	payload := map[string]string{
		"phone":          phone,
		"encrypted_safe": encryptedSafe,
		"salt":           salt,
		"iv":             iv,
		"access_time":    accessTime,
	}
	jsonData, _ := json.Marshal(payload)
	resp, err := http.Post("https://gafam.cloud/api/directory", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Println("Error depositing safe on Cloudflare:", err)
	} else {
		defer resp.Body.Close()
		log.Printf("Safe deposited on Cloudflare for %s at time %s, response: %d", phone, accessTime, resp.StatusCode)
	}
}

// startHoneypotGenerator launches a background goroutine that periodically deposits
// fake safes on Cloudflare to mask the real challenge deposits (OPSEC).
func startHoneypotGenerator() {
	go func() {
		for {
			// Random interval between 10 and 40 minutes
			delay := time.Duration(10+mrand.Intn(30)) * time.Minute
			time.Sleep(delay)

			// Get a phone number from the database (if any devices are registered)
			var phone string
			err := db.QueryRow("SELECT phone FROM gafam_sessions ORDER BY created_at DESC LIMIT 1").Scan(&phone)
			if err != nil || phone == "" {
				continue // No sessions yet, skip this cycle
			}

			// Generate a fake challenge
			fakeHour := mrand.Intn(24)
			fakeMinute := mrand.Intn(60)
			fakeTime := fmt.Sprintf("%02d%02d", fakeHour, fakeMinute)
			fakeClicks := 1 + mrand.Intn(8)

			// Generate a fake safe with a credible fake IPv4 and fake token
			fakeIP := fmt.Sprintf("%d.%d.%d.%d", 1+mrand.Intn(223), mrand.Intn(256), mrand.Intn(256), mrand.Intn(256))
			fakePort := "5150"
			fakeSafe := map[string]string{
				"sessionToken": generateToken(64),
				"vpcUrl":       fmt.Sprintf("http://%s:%s", fakeIP, fakePort),
			}
			fakeJSON, _ := json.Marshal(fakeSafe)

			// Encrypt with PBKDF2 (same algorithm as real safes)
			passphrase := fmt.Sprintf("%s-%d", fakeTime, fakeClicks)
			salt := make([]byte, 16)
			rand.Read(salt)
			aesKey := pbkdf2Key([]byte(passphrase), salt, 500000, 32)

			encryptedSafe, ivBase64, err := encryptAESGCM(aesKey, fakeJSON)
			if err != nil {
				continue
			}

			saltBase64 := base64.StdEncoding.EncodeToString(salt)

			depositSafeOnCloudflare(phone, encryptedSafe, saltBase64, ivBase64, fakeTime)
			log.Printf("Honeypot deposited for %s: fakeTime=%s fakeClicks=%d", phone, fakeTime, fakeClicks)
		}
	}()
}

// ============================================================



func sessionMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				parts := strings.Split(authHeader, " ")
				if len(parts) == 2 && parts[0] == "Bearer" {
					token = parts[1]
				}
			}
		}

		if token == "" {
			http.Error(w, "Missing session token", http.StatusUnauthorized)
			return
		}

		// 1. Authenticate Request
		var status string
		var expiresAt sql.NullTime
		err := db.QueryRow(`SELECT status, expires_at FROM gafam_sessions WHERE session_token = ?`, token).Scan(&status, &expiresAt)
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, `{"error":"Invalid session"}`, http.StatusForbidden)
			} else {
				http.Error(w, `{"error":"Database error"}`, http.StatusInternalServerError)
			}
			return
		}

		if status != "confirmed" {
			http.Error(w, `{"error":"Session not confirmed"}`, http.StatusForbidden)
			return
		}

		if expiresAt.Valid && expiresAt.Time.Before(time.Now()) {
			http.Error(w, `{"error":"Session expired"}`, http.StatusForbidden)
			return
		}

		// 2. E2E decryption layer (AES-256-GCM with session token)
		useE2E := r.Header.Get("X-GAFAM-E2E") == "1"
		if useE2E && r.Body != nil && r.ContentLength > 0 {
			var payload EncryptedPayload
			bodyBytes, readErr := io.ReadAll(r.Body)
			r.Body.Close()
			if readErr == nil {
				if jsonErr := json.Unmarshal(bodyBytes, &payload); jsonErr == nil && payload.EncryptedData != "" {
					key := deriveKey(token)
					if plaintext, decErr := decryptAESGCM(key, payload.EncryptedData, payload.IV); decErr == nil {
						r.Body = io.NopCloser(bytes.NewReader(plaintext))
						r.ContentLength = int64(len(plaintext))
					} else {
						r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
					}
				} else {
					r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				}
			}
		}

		// 3. E2E encryption wrapper for response
		if useE2E {
			key := deriveKey(token)
			buf := &bytes.Buffer{}
			e2eW := &e2eResponseWriter{ResponseWriter: w, buf: buf, key: key, statusCode: http.StatusOK}
			next.ServeHTTP(e2eW, r)
			if buf.Len() > 0 && strings.HasPrefix(e2eW.header.Get("Content-Type"), "application/json") {
				encrypted, ivB64, encErr := encryptAESGCM(key, buf.Bytes())
				if encErr == nil {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(EncryptedPayload{EncryptedData: encrypted, IV: ivB64})
					return
				}
			}
			// Fallback: write buffered response as-is
			if e2eW.wroteHeader {
				w.WriteHeader(e2eW.statusCode)
			}
			w.Write(buf.Bytes())
			return
		}

		next.ServeHTTP(w, r)
	}
}

type e2eResponseWriter struct {
	http.ResponseWriter
	buf        *bytes.Buffer
	key        []byte
	statusCode int
	wroteHeader bool
	header     http.Header
}

func (w *e2eResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = w.ResponseWriter.Header().Clone()
	}
	return w.header
}

func (w *e2eResponseWriter) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}

func (w *e2eResponseWriter) WriteHeader(statusCode int) {
	w.wroteHeader = true
	w.statusCode = statusCode
}

// Generate a cryptographically random alphanumeric token
func generateToken(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

// --- NEW HANDLERS (Phase 1) ---

type ContactPayload struct {
	Phone       string `json:"phone_number"`
	DisplayName string `json:"display_name"`
	IsVerified  int    `json:"is_verified"`
}

func syncContactsHandler(w http.ResponseWriter, r *http.Request) {
	var payload EncryptedPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	key := deriveKey(string(jwtSecret))
	plaintext, err := decryptAESGCM(key, payload.EncryptedData, payload.IV)
	if err != nil {
		http.Error(w, "Decryption failed", http.StatusForbidden)
		return
	}

	var contacts []ContactPayload
	if err := json.Unmarshal(plaintext, &contacts); err != nil {
		http.Error(w, "Invalid decrypted JSON payload", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "DB error", http.StatusInternalServerError)
		return
	}

	stmt, err := tx.Prepare(`
		INSERT INTO gafam_contacts (phone, name) 
		VALUES (?, ?) 
		ON CONFLICT(phone) DO UPDATE SET name=excluded.name
	`)
	if err != nil {
		tx.Rollback()
		http.Error(w, "DB prepare error", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	for _, c := range contacts {
		_, err := stmt.Exec(c.Phone, c.DisplayName)
		if err != nil {
			tx.Rollback()
			http.Error(w, "DB insert error", http.StatusInternalServerError)
			return
		}
	}
	tx.Commit()

	sendJSON(w, http.StatusOK, map[string]string{"status": "contacts_synced"})
}

// --- Trusted Guardians Handlers ---

func getGuardiansHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, name, phone_number, keyword FROM trusted_guardians ORDER BY created_at DESC")
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id int
		var name, phone, keyword string
		if err := rows.Scan(&id, &name, &phone, &keyword); err == nil {
			list = append(list, map[string]interface{}{
				"id":      id,
				"name":    name,
				"phone":   phone,
				"keyword": keyword,
			})
		}
	}

	if list == nil {
		list = []map[string]interface{}{}
	}
	sendJSON(w, http.StatusOK, list)
}

func addGuardianHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Phone   string `json:"phone"`
		Keyword string `json:"keyword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	
	if req.Keyword == "" {
		req.Keyword = "URGENCE_GAFAM"
	}

	_, err := db.Exec("INSERT INTO trusted_guardians (name, phone_number, keyword) VALUES (?, ?, ?)", req.Name, req.Phone, req.Keyword)
	if err != nil {
		http.Error(w, "Failed to add guardian", http.StatusInternalServerError)
		return
	}
	sendJSON(w, http.StatusOK, map[string]string{"status": "added"})
}

func deleteGuardianHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing id", http.StatusBadRequest)
		return
	}
	_, err := db.Exec("DELETE FROM trusted_guardians WHERE id = ?", id)
	if err != nil {
		http.Error(w, "Failed to delete", http.StatusInternalServerError)
		return
	}
	sendJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func getContactsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT phone, name FROM gafam_contacts")
	if err != nil {
		http.Error(w, "DB error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var contacts []map[string]interface{}
	for rows.Next() {
		var phone, name string
		if err := rows.Scan(&phone, &name); err == nil {
			contacts = append(contacts, map[string]interface{}{
				"phone_number": phone,
				"display_name": name,
			})
		}
	}
	if contacts == nil {
		contacts = []map[string]interface{}{}
	}

	jsonData, err := json.Marshal(contacts)
	if err != nil {
		http.Error(w, "Failed to marshal JSON", http.StatusInternalServerError)
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				token = parts[1]
			}
		}
	}

	key := deriveKey(token)
	encryptedBase64, ivBase64, err := encryptAESGCM(key, jsonData)
	if err != nil {
		http.Error(w, "Encryption failed", http.StatusInternalServerError)
		return
	}

	sendJSON(w, http.StatusOK, EncryptedPayload{
		EncryptedData: encryptedBase64,
		IV:            ivBase64,
	})
}



func handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		rows, err := db.Query("SELECT key, value FROM gafam_settings")
		if err != nil {
			http.Error(w, "DB error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		settings := make(map[string]string)
		for rows.Next() {
			var key, val string
			if err := rows.Scan(&key, &val); err == nil {
				settings[key] = val
			}
		}
		token := r.URL.Query().Get("token")
		if token == "" {
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				parts := strings.Split(authHeader, " ")
				if len(parts) == 2 && parts[0] == "Bearer" {
					token = parts[1]
				}
			}
		}

		key := deriveKey(token)
		jsonData, err := json.Marshal(settings)
		if err != nil {
			http.Error(w, "JSON error", http.StatusInternalServerError)
			return
		}

		encryptedBase64, ivBase64, err := encryptAESGCM(key, jsonData)
		if err != nil {
			http.Error(w, "Encryption error", http.StatusInternalServerError)
			return
		}

		// APK relay polls GET /api/settings every second (not the web session route).
		if r.URL.Path == "/api/settings" {
			touchApkRelay()
		}

		sendJSON(w, http.StatusOK, EncryptedPayload{
			EncryptedData: encryptedBase64,
			IV:            ivBase64,
		})
		return
	}

	if r.Method == http.MethodPost {
		var payload EncryptedPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid body", http.StatusBadRequest)
			return
		}

		token := r.URL.Query().Get("token")
		if token == "" {
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				parts := strings.Split(authHeader, " ")
				if len(parts) == 2 && parts[0] == "Bearer" {
					token = parts[1]
				}
			}
		}

		key := deriveKey(token)
		plaintext, err := decryptAESGCM(key, payload.EncryptedData, payload.IV)
		if err != nil {
			http.Error(w, "Decryption failed", http.StatusForbidden)
			return
		}

		var setting struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal(plaintext, &setting); err != nil {
			http.Error(w, "Invalid decrypted JSON", http.StatusBadRequest)
			return
		}

		if setting.Key == "" {
			http.Error(w, "Missing key", http.StatusBadRequest)
			return
		}

		_, err = db.Exec(`
			INSERT INTO gafam_settings (key, value) 
			VALUES (?, ?) 
			ON CONFLICT(key) DO UPDATE SET value=excluded.value
		`, setting.Key, setting.Value)
		
		if err != nil {
			http.Error(w, "DB error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
