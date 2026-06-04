package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
)

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

func smsHandler(w http.ResponseWriter, r *http.Request) {
	var params SmsParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
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

	sendJSON(w, http.StatusOK, smsList)
}

// --- Web Client Auth Handlers (Handshake Simultané) ---

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

func confirmSessionHandler(w http.ResponseWriter, r *http.Request) {
	// Called by the APK when user presses "Authorize Web Login"
	
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

	// Announce asynchronously to Cloudflare Directory
	go func() {
		payload := map[string]string{
			"phone":            req.Phone,
			"session_token":    sessionToken,
			"port":             port,
			"cert_fingerprint": CertFingerprint,
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

		var status string
		err := db.QueryRow(`SELECT status FROM gafam_sessions WHERE session_token = ? AND status = 'confirmed'`, token).Scan(&status)
		if err != nil {
			http.Error(w, "Invalid or expired session", http.StatusForbidden)
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
