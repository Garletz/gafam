package main

import (
	"encoding/json"
	"net/http"
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
