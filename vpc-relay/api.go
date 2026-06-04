package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
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
	"strings"
	"time"
)

// --- Crypto Helpers ---

func deriveKey(secret string) []byte {
	hash := sha256.Sum256([]byte(secret))
	return hash[:]
}

// PBKDF2 implementation (no external dependency needed)
func pbkdf2Key(password, salt []byte, iterations, keyLen int) []byte {
	hmacSha256 := func(key, data []byte) []byte {
		// HMAC-SHA256
		blockSize := 64
		if len(key) > blockSize {
			h := sha256.Sum256(key)
			key = h[:]
		}
		if len(key) < blockSize {
			key = append(key, make([]byte, blockSize-len(key))...)
		}
		ipad := make([]byte, blockSize)
		opad := make([]byte, blockSize)
		for i := 0; i < blockSize; i++ {
			ipad[i] = key[i] ^ 0x36
			opad[i] = key[i] ^ 0x5c
		}
		inner := sha256.Sum256(append(ipad, data...))
		outer := sha256.Sum256(append(opad, inner[:]...))
		return outer[:]
	}

	numBlocks := (keyLen + 31) / 32
	dk := make([]byte, 0, numBlocks*32)

	for block := 1; block <= numBlocks; block++ {
		// U1 = PRF(Password, Salt || INT_32_BE(i))
		saltBlock := make([]byte, len(salt)+4)
		copy(saltBlock, salt)
		saltBlock[len(salt)+0] = byte(block >> 24)
		saltBlock[len(salt)+1] = byte(block >> 16)
		saltBlock[len(salt)+2] = byte(block >> 8)
		saltBlock[len(salt)+3] = byte(block)

		u := hmacSha256(password, saltBlock)
		result := make([]byte, 32)
		copy(result, u)

		for i := 1; i < iterations; i++ {
			u = hmacSha256(password, u)
			for j := 0; j < 32; j++ {
				result[j] ^= u[j]
			}
		}
		dk = append(dk, result...)
	}
	return dk[:keyLen]
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

// --- Outbox Handlers ---

type OutboxParams struct {
	Recipient string `json:"recipient"`
	Body      string `json:"body"`
}

func queueOutboxHandler(w http.ResponseWriter, r *http.Request) {
	var payload EncryptedPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
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

	var params OutboxParams
	if err := json.Unmarshal(plaintext, &params); err != nil {
		http.Error(w, "Invalid decrypted JSON payload", http.StatusBadRequest)
		return
	}

	stmt := `INSERT INTO gafam_outbox (recipient, body) VALUES (?, ?)`
	res, err := db.Exec(stmt, params.Recipient, params.Body)
	if err != nil {
		http.Error(w, "Failed to save to outbox", http.StatusInternalServerError)
		return
	}

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

	stmt := `INSERT INTO gafam_sms (sender, body, timestamp) VALUES (?, ?, ?)`
	res, err := db.Exec(stmt, params.Sender, params.Body, params.Timestamp)
	if err != nil {
		http.Error(w, "Failed to save SMS", http.StatusInternalServerError)
		return
	}

	id, _ := res.LastInsertId()
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"status": "saved",
		"id":     id,
	})
}

func getSmsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, sender, body, timestamp, created_at FROM gafam_sms ORDER BY timestamp DESC")
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var smsList []map[string]interface{}
	for rows.Next() {
		var id int
		var sender, body, createdAt string
		var timestamp int64
		if err := rows.Scan(&id, &sender, &body, &timestamp, &createdAt); err == nil {
			smsList = append(smsList, map[string]interface{}{
				"id":         id,
				"sender":     sender,
				"body":       body,
				"timestamp":  timestamp,
				"created_at": createdAt,
			})
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

func requestSessionHandler(w http.ResponseWriter, r *http.Request) {
	var params struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil || params.Phone == "" {
		http.Error(w, "Missing phone", http.StatusBadRequest)
		return
	}

	sessionID := generateToken(32)

	stmt := `INSERT INTO gafam_sessions (session_id, phone, status, web_requested_at) VALUES (?, ?, 'pending', datetime('now'))`
	_, err := db.Exec(stmt, sessionID, params.Phone)
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	sendJSON(w, http.StatusOK, map[string]string{
		"session_id": sessionID,
		"status":     "pending",
	})
}

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

func confirmSessionHandler(w http.ResponseWriter, r *http.Request) {
	// Called by the APK when user presses "Authorize Web Login" (LEGACY)
	
	var req struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Phone == "" {
		http.Error(w, "Phone is required", http.StatusBadRequest)
		return
	}

	// For zero-config, we pre-generate a session for the web client
	sessionID := generateToken(32)
	sessionToken := generateToken(64)

	_, err := db.Exec(`INSERT INTO gafam_sessions (session_id, phone, status, session_token, created_at, device_confirmed_at) VALUES (?, ?, 'confirmed', ?, datetime('now'), datetime('now'))`, sessionID, req.Phone, sessionToken)
	if err != nil {
		http.Error(w, "Failed to confirm session", http.StatusInternalServerError)
		return
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "5150"
	}

	// Announce asynchronously to Cloudflare Directory (legacy flow)
	go func() {
		payload := map[string]string{
			"phone":         req.Phone,
			"session_token": sessionToken,
			"port":          port,
		}
		jsonData, _ := json.Marshal(payload)
		resp, err := http.Post("https://gafam.cloud/api/directory", "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			log.Println("Error announcing to Cloudflare:", err)
		} else {
			defer resp.Body.Close()
			log.Println("Announced to Cloudflare, response:", resp.StatusCode)
		}
	}()

	sendJSON(w, http.StatusOK, map[string]string{
		"status":     "confirmed",
		"session_id": sessionID,
	})
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

	// 5. Deposit the encrypted safe on Cloudflare
	go depositSafeOnCloudflare(req.Phone, encryptedSafe, saltBase64, ivBase64, req.ChallengeTime)

	log.Printf("Challenge created for %s: time=%s clicks=%d", req.Phone, req.ChallengeTime, req.ChallengeClicks)

	sendJSON(w, http.StatusOK, map[string]string{
		"status":         "challenge_created",
		"challengeTime":  req.ChallengeTime,
	})
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

func checkSessionHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, "Missing session_id", http.StatusBadRequest)
		return
	}

	var status string
	var sessionToken *string
	err := db.QueryRow(`SELECT status, session_token FROM gafam_sessions WHERE session_id = ?`, sessionID).Scan(&status, &sessionToken)
	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	result := map[string]string{"status": status}
	if status == "confirmed" && sessionToken != nil {
		result["session_token"] = *sessionToken
	}

	sendJSON(w, http.StatusOK, result)
}

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

		next.ServeHTTP(w, r)
	}
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
