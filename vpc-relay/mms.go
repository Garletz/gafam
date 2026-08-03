package main

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// --- MMS relay (carrier MMS only — RCS media never reaches the provider) ---
//
// The APK reads content://mms + content://mms/part and uploads each message
// with its parts (media as base64) to /api/auth/mms/sync. The web client lists
// MMS via /api/web/mms and fetches individual media parts (base64, encrypted
// with the session token) via /api/web/mms/part/{id}.

type MmsPartPayload struct {
	ContentType string `json:"content_type"`
	Name        string `json:"name"`
	Text        string `json:"text,omitempty"`
	DataBase64  string `json:"data_base64,omitempty"`
}

type MmsMsgPayload struct {
	Address   string           `json:"address"`
	Timestamp int64            `json:"timestamp"`
	Type      int              `json:"type"` // 1=inbox, 2=sent
	Parts     []MmsPartPayload `json:"parts"`
}

// POST /api/auth/mms/sync — bulk import MMS from the phone (APK-authenticated)
func syncMmsHandler(w http.ResponseWriter, r *http.Request) {
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
		Messages []MmsMsgPayload `json:"messages"`
	}
	if err := json.Unmarshal(plaintext, &wrap); err != nil {
		http.Error(w, "Invalid decrypted JSON payload", http.StatusBadRequest)
		return
	}
	if len(wrap.Messages) > 500 {
		http.Error(w, "Too many messages", http.StatusBadRequest)
		return
	}

	inserted := 0
	for _, m := range wrap.Messages {
		if m.Address == "" || m.Timestamp == 0 || len(m.Parts) == 0 {
			continue
		}
		status := "inbox"
		if m.Type == 2 {
			status = "outbound"
		}
		res, err := db.Exec(
			`INSERT OR IGNORE INTO gafam_mms (address, timestamp, status) VALUES (?, ?, ?)`,
			m.Address, m.Timestamp, status,
		)
		if err != nil {
			continue
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			continue // duplicate (address, timestamp)
		}
		mmsId, _ := res.LastInsertId()
		for _, p := range m.Parts {
			if p.ContentType == "" {
				continue
			}
			var dataBlob []byte
			if p.DataBase64 != "" {
				b, err := base64.StdEncoding.DecodeString(p.DataBase64)
				if err != nil || len(b) > 4*1024*1024 {
					continue
				}
				dataBlob = b
			}
			db.Exec(
				`INSERT INTO gafam_mms_parts (mms_id, content_type, name, text, data) VALUES (?, ?, ?, ?, ?)`,
				mmsId, p.ContentType, p.Name, p.Text, dataBlob,
			)
		}
		inserted++
	}

	// Rolling window: keep max 5,000 MMS
	db.Exec(`DELETE FROM gafam_mms WHERE id NOT IN (SELECT id FROM gafam_mms ORDER BY id DESC LIMIT 5000)`)

	log.Printf("MMS sync: %d new / %d received", inserted, len(wrap.Messages))
	touchApkRelay()
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "synced",
		"received": len(wrap.Messages),
		"inserted": inserted,
	})
}

// GET /api/web/mms — list MMS with text parts inline + media part metadata
func getMmsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, address, timestamp, status, created_at FROM gafam_mms ORDER BY timestamp DESC LIMIT 2000`)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type mmsEntry struct {
		id        int64
		address   string
		timestamp int64
		status    string
		createdAt string
	}
	var entries []mmsEntry
	for rows.Next() {
		var e mmsEntry
		if err := rows.Scan(&e.id, &e.address, &e.timestamp, &e.status, &e.createdAt); err == nil {
			entries = append(entries, e)
		}
	}

	mmsList := []map[string]interface{}{}
	for _, e := range entries {
		partRows, err := db.Query(
			`SELECT id, content_type, name, text, LENGTH(data) FROM gafam_mms_parts WHERE mms_id = ?`,
			e.id,
		)
		parts := []map[string]interface{}{}
		if err == nil {
			for partRows.Next() {
				var pid int64
				var ct, name, text string
				var dataLen int64
				if err := partRows.Scan(&pid, &ct, &name, &text, &dataLen); err == nil {
					p := map[string]interface{}{
						"id":           pid,
						"content_type": ct,
						"name":         name,
					}
					if text != "" {
						p["text"] = text
					}
					if dataLen > 0 {
						p["size"] = dataLen
						p["has_media"] = true
					}
					parts = append(parts, p)
				}
			}
			partRows.Close()
		}
		direction := "inbound"
		if e.status == "outbound" || e.status == "sent" {
			direction = "outbound"
		}
		mmsList = append(mmsList, map[string]interface{}{
			"id":         e.id,
			"sender":     e.address,
			"timestamp":  e.timestamp,
			"status":     e.status,
			"direction":  direction,
			"created_at": e.createdAt,
			"parts":      parts,
			"is_mms":     true,
		})
	}

	jsonData, err := json.Marshal(mmsList)
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

// GET /api/web/mms/part/{id} — returns one media part (base64 + content type,
// encrypted with the session token like the other web payloads)
func getMmsPartHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	partId, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || partId <= 0 {
		http.Error(w, "Invalid part id", http.StatusBadRequest)
		return
	}

	var ct, name string
	var data []byte
	err = db.QueryRow(`SELECT content_type, name, data FROM gafam_mms_parts WHERE id = ?`, partId).
		Scan(&ct, &name, &data)
	if err != nil || len(data) == 0 {
		http.Error(w, "Part not found", http.StatusNotFound)
		return
	}

	jsonData, _ := json.Marshal(map[string]interface{}{
		"id":           partId,
		"content_type": ct,
		"name":         name,
		"data_base64":  base64.StdEncoding.EncodeToString(data),
	})

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
