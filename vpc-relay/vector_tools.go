package main

// Vector tools — semantic memory exposed to the kāraka, plus the ingester
// that builds a semantic profile for each contact from their SMS history.
// This is what makes "/q qui dans mes contacts peut m'aider pour X" or
// "monte une équipe" possible: contacts become searchable by meaning.

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Garletz/gafam/vpc-relay/karaka"
)

func registerVectorTools() {
	karaka.RegisterTool(karaka.Tool{
		ID:          "memory.semantic_search",
		Description: "Semantic search over the node's own memory (contact profiles, vault notes, missions). Finds entities by MEANING, not exact keywords — e.g. 'qui peut m'aider à réparer un vélo'. Returns ranked matches with a similarity score.",
		Category:    "memory",
		Params: map[string]karaka.ParamSpec{
			"query":       {Type: "string", Required: true, Description: "What you're looking for, in natural language"},
			"entity_type": {Type: "string", Required: false, Description: "Restrict to one type (contact, note, mission…)"},
			"k":           {Type: "int", Required: false, Description: "Number of results (default 5, max 50)", Default: 5},
		},
		Returns: "{ matches: [{entity_type, entity_id, score}] }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			query, _ := params["query"].(string)
			entityType, _ := params["entity_type"].(string)
			k := 5
			if kk, ok := params["k"].(float64); ok {
				k = int(kk)
			}
			if query == "" {
				return nil, fmt.Errorf("missing 'query'")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			hits, err := SemanticSearch(ctx, entityType, query, k)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"matches": hits}, nil
		},
	})

	karaka.RegisterTool(karaka.Tool{
		ID:          "memory.remember_embed",
		Description: "Store an entity's semantic embedding in long-term memory (e.g. a contact profile, a note, a mission summary). Later retrievable via memory.semantic_search.",
		Category:    "memory",
		Params: map[string]karaka.ParamSpec{
			"entity_type": {Type: "string", Required: true, Description: "Entity category: contact, note, mission…"},
			"entity_id":   {Type: "string", Required: true, Description: "Unique id (phone, note id…)"},
			"text":        {Type: "string", Required: true, Description: "The text to embed"},
		},
		Returns: "{ stored: bool, model: string, dims: int }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			etype, _ := params["entity_type"].(string)
			eid, _ := params["entity_id"].(string)
			text, _ := params["text"].(string)
			if etype == "" || eid == "" || text == "" {
				return nil, fmt.Errorf("entity_type, entity_id and text required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			vec, model, err := embedText(ctx, text)
			if err != nil {
				return nil, err
			}
			if err := upsertEmbedding(etype, eid, model, text, vec); err != nil {
				return nil, err
			}
			return map[string]interface{}{"stored": true, "model": model, "dims": len(vec)}, nil
		},
	})

	karaka.RegisterTool(karaka.Tool{
		ID:          "memory.build_contact_profiles",
		Description: "Ingest contact profiles into semantic memory: for each contact, summarize their recent SMS history and embed it, so later queries like 'qui peut m'aider pour X' can find them. Call periodically (or via cron).",
		Category:    "memory",
		Params: map[string]karaka.ParamSpec{
			"max_contacts": {Type: "int", Required: false, Description: "Max contacts to profile (default 200)", Default: 200},
		},
		Returns: "{ profiled: int }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			max := 200
			if m, ok := params["max_contacts"].(float64); ok && m > 0 {
				max = int(m)
			}
			n, err := ingestContactProfiles(max)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"profiled": n}, nil
		},
	})

	log.Println("Vector tools registered (memory.semantic_search, memory.remember_embed, memory.build_contact_profiles)")
}

// ingestContactProfiles builds a semantic profile per contact from their SMS
// history and stores it in the embedding table.
func ingestContactProfiles(maxContacts int) (int, error) {
	rows, err := db.Query(`SELECT phone, name FROM gafam_contacts ORDER BY phone LIMIT ?`, maxContacts)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type contact struct{ phone, name string }
	var contacts []contact
	for rows.Next() {
		var c contact
		if rows.Scan(&c.phone, &c.name) == nil {
			contacts = append(contacts, c)
		}
	}

	profiled := 0
	for _, c := range contacts {
		digits := onlyDigits(c.phone)
		suffix := digits
		if len(suffix) > 9 {
			suffix = suffix[len(suffix)-9:]
		}
		if suffix == "" {
			continue
		}
		smsRows, err := db.Query(
			`SELECT body FROM gafam_sms WHERE sender LIKE '%' || ? ORDER BY timestamp DESC LIMIT 20`,
			suffix,
		)
		if err != nil {
			continue
		}
		var bodies []string
		for smsRows.Next() {
			var b string
			if smsRows.Scan(&b) == nil && strings.TrimSpace(b) != "" {
				bodies = append(bodies, strings.TrimSpace(b))
			}
		}
		smsRows.Close()

		profile := "Contact " + c.name + " (" + c.phone + "). "
		if len(bodies) > 0 {
			profile += "Historique récent: " + strings.Join(bodies, " | ")
		} else {
			profile += "Pas d'historique SMS récent."
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		vec, model, err := embedText(ctx, profile)
		cancel()
		if err != nil {
			log.Printf("vector: embed contact %s failed: %v", c.phone, err)
			continue
		}
		if err := upsertEmbedding("contact", c.phone, model, profile, vec); err != nil {
			log.Printf("vector: upsert contact %s failed: %v", c.phone, err)
			continue
		}
		profiled++
	}
	log.Printf("vector: profiled %d contacts", profiled)
	return profiled, nil
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
