package main

// SMS compose aids — personal place book + time presets for rendezvous SMS
// (contacts who don't use GAFAM). Stored on the VPC SQLite; no external geo API.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type ComposePlace struct {
	ID        int64    `json:"id"`
	Label     string   `json:"label"`
	Address   string   `json:"address,omitempty"`
	Lat       *float64 `json:"lat,omitempty"`
	Lon       *float64 `json:"lon,omitempty"`
	UseCount  int      `json:"use_count"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

type ComposeTimePreset struct {
	ID       int64  `json:"id"`
	Label    string `json:"label"`
	Kind     string `json:"kind"` // absolute | relative
	Value    string `json:"value"`
	Sort     int    `json:"sort"`
	UseCount int    `json:"use_count"`
}

func initComposeSnippets() {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS gafam_places (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		label TEXT NOT NULL,
		address TEXT DEFAULT '',
		lat REAL,
		lon REAL,
		use_count INTEGER DEFAULT 0,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		log.Printf("compose: places table: %v", err)
		return
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS gafam_time_presets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		label TEXT NOT NULL,
		kind TEXT NOT NULL,
		value TEXT NOT NULL,
		sort INTEGER DEFAULT 0,
		use_count INTEGER DEFAULT 0
	)`)
	if err != nil {
		log.Printf("compose: time_presets table: %v", err)
		return
	}

	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM gafam_time_presets`).Scan(&n)
	if n == 0 {
		seeds := []ComposeTimePreset{
			{Label: "dans 15 min", Kind: "relative", Value: "+15m", Sort: 10},
			{Label: "dans 30 min", Kind: "relative", Value: "+30m", Sort: 20},
			{Label: "dans 1 h", Kind: "relative", Value: "+60m", Sort: 30},
			{Label: "demain 10h", Kind: "relative", Value: "tomorrow_10:00", Sort: 40},
			{Label: "demain 14h", Kind: "relative", Value: "tomorrow_14:00", Sort: 50},
			{Label: "ce soir 19h", Kind: "relative", Value: "today_19:00", Sort: 60},
		}
		for _, s := range seeds {
			_, err := db.Exec(
				`INSERT INTO gafam_time_presets (label, kind, value, sort) VALUES (?, ?, ?, ?)`,
				s.Label, s.Kind, s.Value, s.Sort,
			)
			if err != nil {
				log.Printf("compose: seed time preset: %v", err)
			}
		}
		log.Println("compose: seeded default time presets")
	}
}

// ─── Places ───

func composePlacesHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		limit := 40
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
				limit = n
			}
		}
		var rows *sql.Rows
		var err error
		if q != "" {
			like := "%" + q + "%"
			rows, err = db.Query(
				`SELECT id, label, address, lat, lon, use_count, updated_at
				 FROM gafam_places
				 WHERE label LIKE ? OR address LIKE ?
				 ORDER BY use_count DESC, updated_at DESC
				 LIMIT ?`, like, like, limit,
			)
		} else {
			rows, err = db.Query(
				`SELECT id, label, address, lat, lon, use_count, updated_at
				 FROM gafam_places
				 ORDER BY use_count DESC, updated_at DESC
				 LIMIT ?`, limit,
			)
		}
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		list := []ComposePlace{}
		for rows.Next() {
			var p ComposePlace
			var lat, lon sql.NullFloat64
			var addr, updated sql.NullString
			if err := rows.Scan(&p.ID, &p.Label, &addr, &lat, &lon, &p.UseCount, &updated); err != nil {
				continue
			}
			p.Address = addr.String
			p.UpdatedAt = updated.String
			if lat.Valid {
				v := lat.Float64
				p.Lat = &v
			}
			if lon.Valid {
				v := lon.Float64
				p.Lon = &v
			}
			list = append(list, p)
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{"places": list})

	case http.MethodPost:
		var in struct {
			Label   string   `json:"label"`
			Address string   `json:"address"`
			Lat     *float64 `json:"lat"`
			Lon     *float64 `json:"lon"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		in.Label = strings.TrimSpace(in.Label)
		if in.Label == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "label required"})
			return
		}
		res, err := db.Exec(
			`INSERT INTO gafam_places (label, address, lat, lon, updated_at) VALUES (?, ?, ?, ?, ?)`,
			in.Label, strings.TrimSpace(in.Address), nullFloat(in.Lat), nullFloat(in.Lon),
			time.Now().UTC().Format(time.RFC3339),
		)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		id, _ := res.LastInsertId()
		sendJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "id": id})

	case http.MethodPut:
		var in struct {
			ID      int64    `json:"id"`
			Label   string   `json:"label"`
			Address string   `json:"address"`
			Lat     *float64 `json:"lat"`
			Lon     *float64 `json:"lon"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ID <= 0 {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "id + fields required"})
			return
		}
		in.Label = strings.TrimSpace(in.Label)
		if in.Label == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "label required"})
			return
		}
		_, err := db.Exec(
			`UPDATE gafam_places SET label=?, address=?, lat=?, lon=?, updated_at=? WHERE id=?`,
			in.Label, strings.TrimSpace(in.Address), nullFloat(in.Lat), nullFloat(in.Lon),
			time.Now().UTC().Format(time.RFC3339), in.ID,
		)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		sendJSON(w, http.StatusOK, map[string]string{"ok": "updated"})

	case http.MethodDelete:
		id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		if id <= 0 {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
			return
		}
		_, err := db.Exec(`DELETE FROM gafam_places WHERE id=?`, id)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		sendJSON(w, http.StatusOK, map[string]string{"ok": "deleted"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func composePlaceUseHandler(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id <= 0 {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}
	_, err := db.Exec(
		`UPDATE gafam_places SET use_count = use_count + 1, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, map[string]string{"ok": "used"})
}

// ─── Time presets ───

func composeTimesHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := db.Query(
			`SELECT id, label, kind, value, sort, use_count
			 FROM gafam_time_presets
			 ORDER BY use_count DESC, sort ASC, id ASC`,
		)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		list := []ComposeTimePreset{}
		for rows.Next() {
			var t ComposeTimePreset
			if err := rows.Scan(&t.ID, &t.Label, &t.Kind, &t.Value, &t.Sort, &t.UseCount); err != nil {
				continue
			}
			list = append(list, t)
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{"times": list})

	case http.MethodPost:
		var in ComposeTimePreset
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		in.Label = strings.TrimSpace(in.Label)
		in.Kind = strings.TrimSpace(in.Kind)
		in.Value = strings.TrimSpace(in.Value)
		if in.Label == "" || in.Value == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "label and value required"})
			return
		}
		if in.Kind != "absolute" && in.Kind != "relative" {
			in.Kind = "relative"
		}
		res, err := db.Exec(
			`INSERT INTO gafam_time_presets (label, kind, value, sort) VALUES (?, ?, ?, ?)`,
			in.Label, in.Kind, in.Value, in.Sort,
		)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		id, _ := res.LastInsertId()
		sendJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "id": id})

	case http.MethodPut:
		var in ComposeTimePreset
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ID <= 0 {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "id + fields required"})
			return
		}
		in.Label = strings.TrimSpace(in.Label)
		in.Value = strings.TrimSpace(in.Value)
		if in.Label == "" || in.Value == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "label and value required"})
			return
		}
		if in.Kind != "absolute" && in.Kind != "relative" {
			in.Kind = "relative"
		}
		_, err := db.Exec(
			`UPDATE gafam_time_presets SET label=?, kind=?, value=?, sort=? WHERE id=?`,
			in.Label, in.Kind, in.Value, in.Sort, in.ID,
		)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		sendJSON(w, http.StatusOK, map[string]string{"ok": "updated"})

	case http.MethodDelete:
		id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		if id <= 0 {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
			return
		}
		_, err := db.Exec(`DELETE FROM gafam_time_presets WHERE id=?`, id)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		sendJSON(w, http.StatusOK, map[string]string{"ok": "deleted"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func composeTimeUseHandler(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id <= 0 {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}
	_, err := db.Exec(`UPDATE gafam_time_presets SET use_count = use_count + 1 WHERE id = ?`, id)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, map[string]string{"ok": "used"})
}

func nullFloat(p *float64) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

// Format helpers used by tests / future kāraka tools.
func formatPlaceSnippet(p ComposePlace) string {
	parts := []string{}
	if p.Label != "" {
		parts = append(parts, p.Label)
	}
	if p.Address != "" && p.Address != p.Label {
		parts = append(parts, p.Address)
	}
	base := strings.Join(parts, " — ")
	if p.Lat != nil && p.Lon != nil {
		return fmt.Sprintf("%s (%.5f,%.5f)", base, *p.Lat, *p.Lon)
	}
	return base
}
