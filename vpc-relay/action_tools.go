package main

// Action tools — the kāraka can finally ACT on the world and REMEMBER:
// send SMS, read SMS history, search contacts, publish to the federated
// feed, and write long-term memories to the vault. Registered from main
// because they need package-main access (db, queueSmsReply, vault).
//
// Permissions (Manifest 25): everything that reaches the outside world
// (sms.send, feed.publish) is "ask" for Suparna — a human approves from
// the dashboard when the mission runs with require_approval.

import (
	"fmt"
	"log"
	"time"

	"github.com/Garletz/gafam/vpc-relay/karaka"
)

// registerActionTools wires the organic action/memory tools into the registry.
func registerActionTools() {
	karaka.RegisterTool(karaka.Tool{
		ID:          "sms.send",
		Description: "Send an SMS to any phone number via the relay phone (queued in the outbox, delivered within ~30s).",
		Category:    "sms",
		Params: map[string]karaka.ParamSpec{
			"to":   {Type: "string", Required: true, Description: "Recipient phone number (international format preferred, e.g. +33612345678)"},
			"body": {Type: "string", Required: true, Description: "SMS text (keep under ~600 chars)"},
		},
		Returns: "{ queued: bool, to: string }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			// Dev-phase kill switch (Settings → Agent controls): agents cannot
			// send SMS at all while it is ON. Replies to the self phone
			// (mission reports) are not affected — they are not agent-initiated.
			if getSetting("agent_kill_sms") == "1" {
				return nil, fmt.Errorf("sms.send blocked: agent SMS kill switch is ON (Settings → Agent controls)")
			}
			to, _ := params["to"].(string)
			body, _ := params["body"].(string)
			if to == "" || body == "" {
				return nil, fmt.Errorf("missing 'to' or 'body'")
			}
			if len(body) > 1500 {
				return nil, fmt.Errorf("body too long (%d chars, max 1500)", len(body))
			}
			queueSmsReply(to, body)
			return map[string]interface{}{"queued": true, "to": to}, nil
		},
	})

	karaka.RegisterTool(karaka.Tool{
		ID:          "sms.history",
		Description: "Read recent SMS (inbox + sent), optionally filtered by correspondent. Useful to understand context before acting.",
		Category:    "sms",
		Params: map[string]karaka.ParamSpec{
			"phone": {Type: "string", Required: false, Description: "Filter by correspondent (substring match on digits)"},
			"limit": {Type: "int", Required: false, Description: "Max messages (default 20, max 50)", Default: 20},
		},
		Returns: "{ messages: [{sender, body, timestamp, status}] }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			limit := 20
			if l, ok := params["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}
			if limit > 50 {
				limit = 50
			}
			phone, _ := params["phone"].(string)
			query := `SELECT sender, body, timestamp, status FROM gafam_sms`
			args := []interface{}{}
			if phone != "" {
				query += ` WHERE sender LIKE ?`
				args = append(args, "%"+phone+"%")
			}
			query += ` ORDER BY timestamp DESC LIMIT ?`
			args = append(args, limit)
			rows, err := db.Query(query, args...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			msgs := []map[string]interface{}{}
			for rows.Next() {
				var sender, body, status string
				var ts int64
				if err := rows.Scan(&sender, &body, &ts, &status); err == nil {
					msgs = append(msgs, map[string]interface{}{
						"sender": sender, "body": truncateStr(body, 300),
						"timestamp": ts, "status": status,
					})
				}
			}
			return map[string]interface{}{"messages": msgs, "count": len(msgs)}, nil
		},
	})

	karaka.RegisterTool(karaka.Tool{
		ID:          "contacts.search",
		Description: "Search the synced phone contacts by name or phone number fragment.",
		Category:    "contacts",
		Params: map[string]karaka.ParamSpec{
			"query": {Type: "string", Required: true, Description: "Name or phone fragment to search"},
		},
		Returns: "{ contacts: [{phone, name}] }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			q, _ := params["query"].(string)
			if q == "" {
				return nil, fmt.Errorf("missing 'query'")
			}
			rows, err := db.Query(
				`SELECT phone, name FROM gafam_contacts WHERE name LIKE ? OR phone LIKE ? LIMIT 10`,
				"%"+q+"%", "%"+q+"%",
			)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := []map[string]interface{}{}
			for rows.Next() {
				var phone, name string
				if err := rows.Scan(&phone, &name); err == nil {
					out = append(out, map[string]interface{}{"phone": phone, "name": name})
				}
			}
			return map[string]interface{}{"contacts": out, "count": len(out)}, nil
		},
	})

	karaka.RegisterTool(karaka.Tool{
		ID:          "feed.publish",
		Description: "Publish a message to this node's public federated feed — other sovereign nodes scanning your /feed will see it.",
		Category:    "feed",
		Params: map[string]karaka.ParamSpec{
			"content":   {Type: "string", Required: true, Description: "Message to publish (max 2000 chars)"},
			"recipient": {Type: "string", Required: false, Description: "Recipient phone ('*' = broadcast, default)", Default: "*"},
		},
		Returns: "{ published: bool, recipient: string }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			content, _ := params["content"].(string)
			recipient, _ := params["recipient"].(string)
			if content == "" {
				return nil, fmt.Errorf("missing 'content'")
			}
			if recipient == "" {
				recipient = "*"
			}
			selfPhone := getSelfPhone()
			if selfPhone == "" {
				return nil, fmt.Errorf("self_phone not configured")
			}
			if len(content) > 2000 {
				content = content[:1996] + "..."
			}
			sig, ts, err := signEnvelope(selfPhone, recipient, content)
			if err != nil {
				return nil, fmt.Errorf("signing failed: %w", err)
			}
			if _, err := db.Exec(
				`INSERT INTO gafam_envelopes (author_phone, recipient_phone, content, signature, signed_ts, created_at)
				 VALUES (?, ?, ?, ?, ?, datetime('now'))`,
				selfPhone, recipient, content, sig, ts,
			); err != nil {
				return nil, err
			}
			return map[string]interface{}{"published": true, "recipient": recipient}, nil
		},
	})

	karaka.RegisterTool(karaka.Tool{
		ID:          "vault.remember",
		Description: "Write a long-term memory to the vault (markdown note, FTS5-indexed). Future missions will find it via memory injection. Use it to persist anything worth knowing later: findings, decisions, user preferences.",
		Category:    "vault",
		Params: map[string]karaka.ParamSpec{
			"title": {Type: "string", Required: true, Description: "Short memory title"},
			"body":  {Type: "string", Required: true, Description: "Memory content (markdown)"},
			"tags":  {Type: "string", Required: false, Description: "Space-separated tags", Default: "kāraka memory"},
		},
		Returns: "{ id: string, path: string }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			title, _ := params["title"].(string)
			body, _ := params["body"].(string)
			tags, _ := params["tags"].(string)
			if title == "" || body == "" {
				return nil, fmt.Errorf("missing 'title' or 'body'")
			}
			if tags == "" {
				tags = "kāraka memory"
			}
			id := fmt.Sprintf("memory-%d", time.Now().UnixMilli())
			path := "/files/research/notes/" + id + ".md"
			md := fmt.Sprintf("---\nid: %s\ntitle: %q\ntags: [%s]\n---\n# %s\n\n%s\n", id, title, tags, title, body)
			if _, err := karaka.ExecuteTool("sandbox.file_write", map[string]interface{}{
				"path":    path,
				"content": md,
			}); err != nil {
				log.Printf("vault.remember: sandbox write failed (indexing anyway): %v", err)
				path = ""
			}
			if _, err := db.Exec(
				`INSERT INTO research_notes (id, title, url, tags, body, fetched_at, path, suggested_by)
				 VALUES (?, ?, ?, ?, ?, datetime('now'), ?, ?)`,
				id, title, "", tags, body, path, "kāraka",
			); err != nil {
				return nil, err
			}
			return map[string]interface{}{"id": id, "path": path}, nil
		},
	})

	log.Println("Action tools registered (sms.send, sms.history, contacts.search, feed.publish, vault.remember)")
}

// registerActionToolPermissions extends the default kāraka permission maps.
// Called after RegisterDefaultKarakas — values follow Manifest 25:
// read = allow, reach-the-outside-world = ask.
func registerActionToolPermissions() {
	karaka.SetPermissions("suparna_vpc", map[string]string{
		"sms.send":        "ask",
		"sms.history":     "allow",
		"contacts.search": "allow",
		"feed.publish":    "ask",
		"vault.remember":  "allow",
		"custom.*":        "allow", // agent-written scripts stay inside the sandbox
		"memory.*":        "allow", // semantic search + embeddings (read-only over own data)
		"contacts.auto_analyze": "allow",
	})
	karaka.SetPermissions("edge_l2_phone", map[string]string{
		"sms.*":      "ask",
		"contacts.*": "allow",
		"feed.*":     "ask",
		"vault.*":    "allow",
		"custom.*":   "allow",
		"memory.*":   "allow",
	})
}
