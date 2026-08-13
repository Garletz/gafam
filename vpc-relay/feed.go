package main

// VPC↔VPC Federation — Poneglyph Part 3 (Manifest 17)
//
// "Boîte Auto-Publieuse" model:
//   You publish on your VPC → others scan your /feed
//   No push, no relay, no central queue — just pull.

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// ─── Types ───

type Link struct {
	ID            int64  `json:"id"`
	Phone         string `json:"phone"`
	Name          string `json:"name"`
	VpcURL        string `json:"vpc_url,omitempty"`
	PublicKey     string `json:"public_key,omitempty"`
	LastPoll      string `json:"last_poll,omitempty"`
	LastPublished string `json:"last_published,omitempty"`
	CreatedAt     string `json:"created_at"`
}

type Envelope struct {
	ID             int64  `json:"id"`
	AuthorPhone    string `json:"author_phone"`
	RecipientPhone string `json:"recipient_phone"`
	Content        string `json:"content"`
	Signature      string `json:"signature,omitempty"`
	SignedTs       int64  `json:"signed_ts,omitempty"`
	CreatedAt      string `json:"created_at"`
}

type InboxEntry struct {
	ID          int64  `json:"id"`
	LinkID      int64  `json:"link_id"`
	AuthorPhone string `json:"author_phone"`
	Content     string `json:"content"`
	FetchedAt   string `json:"fetched_at"`
}

// ─── Keypair ───

const settingNodeKeypair = "node_keypair"

func getNodeKeypair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	raw := getSetting(settingNodeKeypair)
	if raw != "" {
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) == 2 {
			pub, err1 := hex.DecodeString(parts[0])
			priv, err2 := hex.DecodeString(parts[1])
			if err1 == nil && err2 == nil && len(pub) == ed25519.PublicKeySize && len(priv) == ed25519.PrivateKeySize {
				return ed25519.PublicKey(pub), ed25519.PrivateKey(priv), nil
			}
		}
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	store := hex.EncodeToString(pub) + ":" + hex.EncodeToString(priv)
	if err := setSetting(settingNodeKeypair, store); err != nil {
		log.Printf("feed: failed to persist keypair: %v", err)
	}
	return pub, priv, nil
}

func getNodePubkeyHex() string {
	pub, _, err := getNodeKeypair()
	if err != nil {
		return ""
	}
	return hex.EncodeToString(pub)
}

func getSelfPhone() string {
	return strings.TrimSpace(getSetting("self_phone"))
}

// ─── DB tables (called from initDB) ───

func initFeedTables() {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS gafam_links (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			phone TEXT UNIQUE NOT NULL,
			name TEXT DEFAULT '',
			vpc_url TEXT DEFAULT '',
			public_key TEXT DEFAULT '',
			last_poll DATETIME,
			last_published DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS gafam_envelopes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			author_phone TEXT NOT NULL,
			recipient_phone TEXT DEFAULT '*',
			content TEXT NOT NULL,
			signature TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS gafam_inbox (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			link_id INTEGER,
			author_phone TEXT NOT NULL,
			content TEXT NOT NULL,
			envelope_signature TEXT DEFAULT '',
			fetched_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (link_id) REFERENCES gafam_links(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_envelopes_created ON gafam_envelopes(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_inbox_link ON gafam_inbox(link_id)`,
	}
	for _, ddl := range tables {
		if _, err := db.Exec(ddl); err != nil {
			log.Printf("feed: DDL error: %v", err)
		}
	}
	// Migration: envelopes carry the signed timestamp so signatures are
	// actually verifiable by receiving nodes.
	if !columnExists("gafam_envelopes", "signed_ts") {
		if _, err := db.Exec(`ALTER TABLE gafam_envelopes ADD COLUMN signed_ts INTEGER DEFAULT 0`); err != nil {
			log.Printf("feed: signed_ts migration failed: %v", err)
		}
	}
}

// ─── Signing ───

// envelopePayload is the canonical signed message. The timestamp is part of
// the payload AND stored/served with the envelope — without it the signature
// is unverifiable even in principle (that was the pre-fix bug).
func envelopePayload(authorPhone, recipientPhone string, ts int64, content string) string {
	return fmt.Sprintf("%s|%s|%d|%s", authorPhone, recipientPhone, ts, content)
}

func signEnvelope(authorPhone, recipientPhone, content string) (string, int64, error) {
	_, priv, err := getNodeKeypair()
	if err != nil {
		return "", 0, err
	}
	ts := time.Now().UnixMilli()
	sig := ed25519.Sign(priv, []byte(envelopePayload(authorPhone, recipientPhone, ts, content)))
	return hex.EncodeToString(sig), ts, nil
}

// verifyEnvelope checks an incoming envelope against the sender link's
// public key. Returns false for malformed/mismatched signatures.
func verifyEnvelope(pubKeyHex string, e Envelope) bool {
	pub, err := hex.DecodeString(pubKeyHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	sig, err := hex.DecodeString(e.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	msg := envelopePayload(e.AuthorPhone, e.RecipientPhone, e.SignedTs, e.Content)
	return ed25519.Verify(ed25519.PublicKey(pub), []byte(msg), sig)
}

// ─── Public feed endpoint (called by other VPCs) ───

func publicFeedHandler(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	var rows *sql.Rows
	var err error
	if since != "" {
		rows, err = db.Query(
			`SELECT id, author_phone, recipient_phone, content, signature, signed_ts, created_at 
			 FROM gafam_envelopes WHERE created_at > ? ORDER BY created_at DESC LIMIT 100`,
			since,
		)
	} else {
		rows, err = db.Query(
			`SELECT id, author_phone, recipient_phone, content, signature, signed_ts, created_at 
			 FROM gafam_envelopes ORDER BY created_at DESC LIMIT 50`,
		)
	}
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	envelopes := []Envelope{}
	for rows.Next() {
		var e Envelope
		if err := rows.Scan(&e.ID, &e.AuthorPhone, &e.RecipientPhone, &e.Content, &e.Signature, &e.SignedTs, &e.CreatedAt); err != nil {
			continue
		}
		envelopes = append(envelopes, e)
	}
	if envelopes == nil {
		envelopes = []Envelope{}
	}
	sendJSON(w, http.StatusOK, map[string]interface{}{"envelopes": envelopes})
}

// ─── Links (session-protected) ───

func linksHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := db.Query(`SELECT id, phone, name, vpc_url, public_key, last_poll, last_published, created_at FROM gafam_links ORDER BY created_at DESC`)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
			return
		}
		defer rows.Close()
		links := []Link{}
		for rows.Next() {
			var l Link
			var lastPoll, lastPub, vpcURL, pubKey sql.NullString
			if err := rows.Scan(&l.ID, &l.Phone, &l.Name, &vpcURL, &pubKey, &lastPoll, &lastPub, &l.CreatedAt); err != nil {
				continue
			}
			l.VpcURL = vpcURL.String
			l.PublicKey = pubKey.String
			l.LastPoll = lastPoll.String
			l.LastPublished = lastPub.String
			links = append(links, l)
		}
		if links == nil {
			links = []Link{}
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{"links": links})

	case http.MethodPost:
		var in struct {
			Phone string `json:"phone"`
			Name  string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Phone == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "phone required"})
			return
		}
		in.Phone = phoneToSubdomain(in.Phone)
		in.Name = strings.TrimSpace(in.Name)

		// Discover VPC URL via gafam.cloud directory
		vpcURL, pubKey := discoverVpcURL(in.Phone)

		_, err := db.Exec(
			`INSERT INTO gafam_links (phone, name, vpc_url, public_key) VALUES (?, ?, ?, ?)
			 ON CONFLICT(phone) DO UPDATE SET name=excluded.name, vpc_url=excluded.vpc_url, public_key=excluded.public_key`,
			in.Phone, in.Name, vpcURL, pubKey,
		)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("feed: link added: %s (%s)", in.Name, in.Phone)
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"phone":     in.Phone,
			"name":      in.Name,
			"vpc_url":   vpcURL,
			"public_key": pubKey,
		})

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
			return
		}
		_, err := db.Exec(`DELETE FROM gafam_links WHERE id = ?`, id)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		sendJSON(w, http.StatusOK, map[string]string{"deleted": id})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// discoverVpcURL looks up a phone number in the gafam.cloud directory.
func discoverVpcURL(phone string) (vpcURL, pubKey string) {
	selfPhone := getSelfPhone()
	if selfPhone == phone {
		return "", ""
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(fmt.Sprintf("https://%s.gafam.cloud/api/directory?phone=%s&caller=%s", phone, phone, selfPhone))
	if err != nil {
		log.Printf("feed: directory lookup failed for %s: %v", phone, err)
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", ""
	}
	var result struct {
		VpcURL    string `json:"vpc_url"`
		PublicKey string `json:"public_key"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&result)
	return result.VpcURL, result.PublicKey
}

// feedOwnHandler returns envelopes published by this VPC node.
func feedOwnHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(
		`SELECT id, author_phone, recipient_phone, content, signature, signed_ts, created_at
		 FROM gafam_envelopes ORDER BY created_at DESC LIMIT 100`,
	)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()
	envelopes := []Envelope{}
	for rows.Next() {
		var e Envelope
		if err := rows.Scan(&e.ID, &e.AuthorPhone, &e.RecipientPhone, &e.Content, &e.Signature, &e.SignedTs, &e.CreatedAt); err != nil {
			continue
		}
		envelopes = append(envelopes, e)
	}
	if envelopes == nil {
		envelopes = []Envelope{}
	}
	sendJSON(w, http.StatusOK, map[string]interface{}{"envelopes": envelopes, "public_key": getNodePubkeyHex()})
}

// ─── Feed publish (session-protected) ───

func feedPublishHandler(w http.ResponseWriter, r *http.Request) {
	selfPhone := getSelfPhone()
	if selfPhone == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "self_phone not set"})
		return
	}

	var in struct {
		RecipientPhone string `json:"recipient_phone"`
		Content        string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Content == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "content required"})
		return
	}
	if in.RecipientPhone == "" {
		in.RecipientPhone = "*"
	}

	sig, ts, err := signEnvelope(selfPhone, in.RecipientPhone, in.Content)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "signing failed"})
		return
	}

	result, err := db.Exec(
		`INSERT INTO gafam_envelopes (author_phone, recipient_phone, content, signature, signed_ts, created_at)
		 VALUES (?, ?, ?, ?, ?, datetime('now'))`,
		selfPhone, in.RecipientPhone, in.Content, sig, ts,
	)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Update last_published on matching links
	db.Exec(`UPDATE gafam_links SET last_published = datetime('now') WHERE phone = ?`, in.RecipientPhone)

	id, _ := result.LastInsertId()
	log.Printf("feed: published envelope %d by %s", id, selfPhone)
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"id":              id,
		"author_phone":    selfPhone,
		"recipient_phone": in.RecipientPhone,
	})
}

// ─── Scan target feed (session-protected) ───

func feedScanHandler(w http.ResponseWriter, r *http.Request) {
	phone := r.PathValue("phone")
	if phone == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing phone"})
		return
	}

	var linkID int64
	var vpcURL, linkPubKey string
	err := db.QueryRow(`SELECT id, vpc_url, public_key FROM gafam_links WHERE phone = ?`, phone).Scan(&linkID, &vpcURL, &linkPubKey)
	if err != nil {
		sendJSON(w, http.StatusNotFound, map[string]string{"error": "link not found"})
		return
	}
	if vpcURL == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "link has no vpc_url — add via POST /api/web/links first"})
		return
	}

	selfPhone := getSelfPhone()

	url := strings.TrimRight(vpcURL, "/") + "/feed"
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("unreachable: %v", err)})
		return
	}
	defer resp.Body.Close()

	var result struct {
		Envelopes []Envelope `json:"envelopes"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 1<<18)).Decode(&result)

	newCount := 0
	rejectedCount := 0
	for _, env := range result.Envelopes {
		// Only ingest if addressed to us or broadcast
		if env.RecipientPhone != "*" && env.RecipientPhone != selfPhone {
			continue
		}
		// Signature verification: an envelope claiming to come from the linked
		// node must verify against the link's stored public key. Unsigned
		// legacy envelopes are accepted but logged; forged ones are dropped.
		if env.Signature != "" && linkPubKey != "" {
			if !verifyEnvelope(linkPubKey, env) {
				rejectedCount++
				log.Printf("feed: REJECTED envelope with invalid signature (link %s, author %s)", phone, env.AuthorPhone)
				continue
			}
		} else if env.Signature == "" {
			log.Printf("feed: accepting unsigned envelope from %s (legacy)", env.AuthorPhone)
		}
		// Avoid duplicates
		var exists int
		db.QueryRow(`SELECT COUNT(*) FROM gafam_inbox WHERE link_id = ? AND content = ? AND author_phone = ?`,
			linkID, env.Content, env.AuthorPhone).Scan(&exists)
		if exists > 0 {
			continue
		}
		db.Exec(
			`INSERT INTO gafam_inbox (link_id, author_phone, content, envelope_signature, fetched_at)
			 VALUES (?, ?, ?, ?, datetime('now'))`,
			linkID, env.AuthorPhone, env.Content, env.Signature,
		)
		newCount++
	}

	db.Exec(`UPDATE gafam_links SET last_poll = datetime('now') WHERE id = ?`, linkID)
	log.Printf("feed: scanned %s — %d new, %d rejected (bad signature)", phone, newCount, rejectedCount)
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"phone":    phone,
		"total":    len(result.Envelopes),
		"new":      newCount,
		"rejected": rejectedCount,
	})
}

// ─── Inbox (session-protected) ───

func inboxHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(
		`SELECT i.id, i.link_id, i.author_phone, i.content, i.fetched_at
		 FROM gafam_inbox i ORDER BY i.fetched_at DESC LIMIT 200`,
	)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()
	entries := []InboxEntry{}
	for rows.Next() {
		var e InboxEntry
		if err := rows.Scan(&e.ID, &e.LinkID, &e.AuthorPhone, &e.Content, &e.FetchedAt); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []InboxEntry{}
	}
	sendJSON(w, http.StatusOK, map[string]interface{}{"inbox": entries})
}

// ─── Circles (session-protected) ───

func circlesHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		raw := getSetting("circles")
		circles := []Circle{}
		if raw != "" {
			json.Unmarshal([]byte(raw), &circles)
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{"circles": circles})

	case http.MethodPost:
		var c Circle
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil || c.Name == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
			return
		}
		c.Name = strings.TrimSpace(c.Name)
		raw := getSetting("circles")
		circles := []Circle{}
		if raw != "" {
			json.Unmarshal([]byte(raw), &circles)
		}
		found := false
		for i, existing := range circles {
			if existing.Name == c.Name {
				circles[i] = c
				found = true
				break
			}
		}
		if !found {
			circles = append(circles, c)
		}
		data, _ := json.Marshal(circles)
		setSetting("circles", string(data))
		log.Printf("feed: circle upserted: %s (%d members)", c.Name, len(c.Phones))
		sendJSON(w, http.StatusOK, map[string]interface{}{"circle": c})

	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		if name == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing name"})
			return
		}
		raw := getSetting("circles")
		circles := []Circle{}
		if raw != "" {
			json.Unmarshal([]byte(raw), &circles)
		}
		filtered := []Circle{}
		for _, c := range circles {
			if c.Name != name {
				filtered = append(filtered, c)
			}
		}
		data, _ := json.Marshal(filtered)
		setSetting("circles", string(data))
		log.Printf("feed: circle deleted: %s", name)
		sendJSON(w, http.StatusOK, map[string]string{"deleted": name})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

type Circle struct {
	Name   string   `json:"name"`
	Phones []string `json:"phones"`
}

// circleFeedHandler returns combined inbox from all circle members.
func circleFeedHandler(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing circle name"})
		return
	}
	raw := getSetting("circles")
	circles := []Circle{}
	if raw != "" {
		json.Unmarshal([]byte(raw), &circles)
	}
	var phoneSet map[string]bool
	for _, c := range circles {
		if c.Name == name {
			phoneSet = make(map[string]bool, len(c.Phones))
			for _, p := range c.Phones {
				phoneSet[p] = true
			}
			break
		}
	}
	if phoneSet == nil {
		sendJSON(w, http.StatusNotFound, map[string]string{"error": "circle not found"})
		return
	}

	// Build IN clause
	phones := make([]string, 0, len(phoneSet))
	placeholders := make([]string, 0, len(phoneSet))
	args := make([]interface{}, 0, len(phoneSet))
	for p := range phoneSet {
		phones = append(phones, p)
		placeholders = append(placeholders, "?")
		args = append(args, p)
	}

	query := fmt.Sprintf(
		`SELECT i.id, i.link_id, i.author_phone, i.content, i.fetched_at
		 FROM gafam_inbox i
		 JOIN gafam_links l ON i.link_id = l.id
		 WHERE l.phone IN (%s)
		 ORDER BY i.fetched_at DESC LIMIT 200`,
		strings.Join(placeholders, ","),
	)

	rows, err := db.Query(query, args...)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	entries := []InboxEntry{}
	for rows.Next() {
		var e InboxEntry
		if err := rows.Scan(&e.ID, &e.LinkID, &e.AuthorPhone, &e.Content, &e.FetchedAt); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []InboxEntry{}
	}
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"circle":  name,
		"members": phones,
		"inbox":   entries,
	})
}
