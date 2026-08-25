package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ContactExtended represents the full metadata model for a contact.
type ContactExtended struct {
	ID             int64    `json:"id,omitempty"`
	Phone          string   `json:"phone_number"`
	Name           string   `json:"display_name"`
	Email          string   `json:"email"`
	Profession     string   `json:"profession"`
	Skills         []string `json:"skills"`
	Languages      []string `json:"languages"`
	Notes          string   `json:"notes"`
	AutoProfile    string   `json:"auto_profile"`
	AutoSkills     []string `json:"auto_skills"`
	AutoLanguages  []string `json:"auto_languages"`
	AutoProfession string   `json:"auto_profession"`
	LastAnalyzedAt string   `json:"last_analyzed_at,omitempty"`
	UpdatedAt      string   `json:"updated_at,omitempty"`
}

func initContactsSchema() {
	migrations := []string{
		`ALTER TABLE gafam_contacts ADD COLUMN email TEXT DEFAULT ''`,
		`ALTER TABLE gafam_contacts ADD COLUMN profession TEXT DEFAULT ''`,
		`ALTER TABLE gafam_contacts ADD COLUMN skills TEXT DEFAULT '[]'`,
		`ALTER TABLE gafam_contacts ADD COLUMN languages TEXT DEFAULT '[]'`,
		`ALTER TABLE gafam_contacts ADD COLUMN notes TEXT DEFAULT ''`,
		`ALTER TABLE gafam_contacts ADD COLUMN auto_profile TEXT DEFAULT ''`,
		`ALTER TABLE gafam_contacts ADD COLUMN auto_skills TEXT DEFAULT '[]'`,
		`ALTER TABLE gafam_contacts ADD COLUMN auto_languages TEXT DEFAULT '[]'`,
		`ALTER TABLE gafam_contacts ADD COLUMN auto_profession TEXT DEFAULT ''`,
		`ALTER TABLE gafam_contacts ADD COLUMN last_analyzed_at DATETIME`,
		`ALTER TABLE gafam_contacts ADD COLUMN last_analysis_ts INTEGER DEFAULT 0`,
	}
	for _, m := range migrations {
		db.Exec(m) // Ignore error if column already exists
	}
}

// getContactsExtendedHandler returns the full contact list with extended attributes.
func getContactsExtended(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT id, phone, name, COALESCE(email, ''), COALESCE(profession, ''),
		       COALESCE(skills, '[]'), COALESCE(languages, '[]'), COALESCE(notes, ''),
		       COALESCE(auto_profile, ''), COALESCE(auto_skills, '[]'),
		       COALESCE(auto_languages, '[]'), COALESCE(auto_profession, ''),
		       COALESCE(last_analyzed_at, ''), COALESCE(updated_at, '')
		FROM gafam_contacts
		ORDER BY name ASC, phone ASC
	`)
	if err != nil {
		http.Error(w, "DB error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var list []ContactExtended
	for rows.Next() {
		var c ContactExtended
		var skillsJSON, langsJSON, autoSkillsJSON, autoLangsJSON string
		if err := rows.Scan(
			&c.ID, &c.Phone, &c.Name, &c.Email, &c.Profession,
			&skillsJSON, &langsJSON, &c.Notes,
			&c.AutoProfile, &autoSkillsJSON,
			&autoLangsJSON, &c.AutoProfession,
			&c.LastAnalyzedAt, &c.UpdatedAt,
		); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(skillsJSON), &c.Skills)
		if c.Skills == nil {
			c.Skills = []string{}
		}
		_ = json.Unmarshal([]byte(langsJSON), &c.Languages)
		if c.Languages == nil {
			c.Languages = []string{}
		}
		_ = json.Unmarshal([]byte(autoSkillsJSON), &c.AutoSkills)
		if c.AutoSkills == nil {
			c.AutoSkills = []string{}
		}
		_ = json.Unmarshal([]byte(autoLangsJSON), &c.AutoLanguages)
		if c.AutoLanguages == nil {
			c.AutoLanguages = []string{}
		}
		list = append(list, c)
	}
	if list == nil {
		list = []ContactExtended{}
	}

	jsonData, err := json.Marshal(list)
	if err != nil {
		http.Error(w, "JSON marshal error", http.StatusInternalServerError)
		return
	}

	token := extractToken(r)
	if token != "" {
		key := deriveKey(token)
		encryptedBase64, ivBase64, err := encryptAESGCM(key, jsonData)
		if err == nil {
			sendJSON(w, http.StatusOK, EncryptedPayload{
				EncryptedData: encryptedBase64,
				IV:            ivBase64,
			})
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(jsonData)
}

// saveContactHandler handles manual edits to a contact.
func saveContactHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	plaintext, err := decryptRequestBody(r)
	if err != nil {
		body, rErr := io.ReadAll(r.Body)
		if rErr != nil || len(body) == 0 {
			http.Error(w, "Bad request payload", http.StatusBadRequest)
			return
		}
		plaintext = body
	}

	var req struct {
		Phone      string   `json:"phone_number"`
		Name       string   `json:"display_name"`
		Email      string   `json:"email"`
		Profession string   `json:"profession"`
		Skills     []string `json:"skills"`
		Languages  []string `json:"languages"`
		Notes      string   `json:"notes"`
	}
	if err := json.Unmarshal(plaintext, &req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Phone == "" {
		http.Error(w, "phone_number required", http.StatusBadRequest)
		return
	}

	skillsJSON, _ := json.Marshal(req.Skills)
	if req.Skills == nil {
		skillsJSON = []byte("[]")
	}
	langsJSON, _ := json.Marshal(req.Languages)
	if req.Languages == nil {
		langsJSON = []byte("[]")
	}

	_, err = db.Exec(`
		INSERT INTO gafam_contacts (phone, name, email, profession, skills, languages, notes, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(phone) DO UPDATE SET
			name = excluded.name,
			email = excluded.email,
			profession = excluded.profession,
			skills = excluded.skills,
			languages = excluded.languages,
			notes = excluded.notes,
			updated_at = datetime('now')
	`, req.Phone, req.Name, req.Email, req.Profession, string(skillsJSON), string(langsJSON), req.Notes)
	if err != nil {
		http.Error(w, "DB save error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Update the semantic embedding in the background.
	go embedContactFields(req.Name, req.Phone, req.Email, req.Profession, req.Skills, req.Languages, req.Notes)

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"status": "saved",
		"phone":  req.Phone,
	})
}

// embedContactFields builds the canonical contact profile document and
// (re)embeds it into semantic memory. Shared by save and CSV import so the
// profile text stays identical everywhere.
func embedContactFields(name, phone, email, profession string, skills, languages []string, notes string) {
	doc := fmt.Sprintf("Contact: %s (%s). Email: %s. Métier/Profession: %s. Compétences: %s. Langues: %s. Notes: %s",
		name, phone, email, profession, strings.Join(skills, ", "), strings.Join(languages, ", "), notes)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	if vec, model, err := embedText(ctx, doc); err == nil {
		_ = upsertEmbedding("contact", phone, model, doc, vec)
	}
}

// analyzeContactHandler deduces profession, skills, languages and summary from SMS history.
func analyzeContactHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	plaintext, err := decryptRequestBody(r)
	if err != nil {
		body, _ := io.ReadAll(r.Body)
		plaintext = body
	}

	var req struct {
		Phone string `json:"phone_number"`
	}
	if len(plaintext) > 0 {
		_ = json.Unmarshal(plaintext, &req)
	}

	if req.Phone == "" {
		http.Error(w, "phone_number required", http.StatusBadRequest)
		return
	}

	res, err := analyzeSingleContact(req.Phone)
	if err != nil {
		http.Error(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	sendJSON(w, http.StatusOK, res)
}

func analyzeSingleContact(phone string) (*ContactExtended, error) {
	var c ContactExtended
	var skillsJSON, langsJSON, notes string
	err := db.QueryRow(`
		SELECT id, phone, name, COALESCE(email, ''), COALESCE(profession, ''),
		       COALESCE(skills, '[]'), COALESCE(languages, '[]'), COALESCE(notes, '')
		FROM gafam_contacts WHERE phone = ?`, phone).Scan(
		&c.ID, &c.Phone, &c.Name, &c.Email, &c.Profession, &skillsJSON, &langsJSON, &notes,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(skillsJSON), &c.Skills)
	_ = json.Unmarshal([]byte(langsJSON), &c.Languages)
	c.Notes = notes

	// Collect last 30 SMS messages
	digits := onlyDigits(phone)
	suffix := digits
	if len(suffix) > 9 {
		suffix = suffix[len(suffix)-9:]
	}
	var messages []string
	if suffix != "" {
		rows, err := db.Query(`SELECT body FROM gafam_sms WHERE sender LIKE '%' || ? ORDER BY timestamp DESC LIMIT 30`, suffix)
		if err == nil {
			for rows.Next() {
				var b string
				if rows.Scan(&b) == nil && strings.TrimSpace(b) != "" {
					messages = append(messages, strings.TrimSpace(b))
				}
			}
			rows.Close()
		}
	}

	if len(messages) == 0 {
		c.AutoProfile = "Aucun historique SMS récent disponible pour déduire des compétences."
		c.AutoSkills = []string{}
		c.AutoLanguages = []string{"fr"}
		c.AutoProfession = c.Profession
		return &c, nil
	}

	// Prepare deduction prompt
	historyText := strings.Join(messages, "\n- ")
	prompt := fmt.Sprintf(`Analyse les SMS suivants échangés avec le contact "%s" (%s) et déduis son profil.
Réponds UNIQUEMENT sous forme d'un objet JSON strict avec cette structure :
{
  "summary": "Résumé de 1-2 phrases sur qui est cette personne et ce dont vous parlez",
  "profession": "Métier / profession déduite ou estimée (ex: Plombier, Avocat, Dev, Ami...)",
  "skills": ["compétence1", "compétence2", "compétence3"],
  "languages": ["fr", "en"]
}

SMS :
- %s`, c.Name, c.Phone, historyText)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var deduced struct {
		Summary    string   `json:"summary"`
		Profession string   `json:"profession"`
		Skills     []string `json:"skills"`
		Languages  []string `json:"languages"`
	}

	aiRes, err := chatWithEngine(ctx, "light_task", "Tu es un extracteur d'entités et d'attributs sémantiques. Réponds UNIQUEMENT en JSON valide.", prompt, 1024)
	if err == nil && aiRes != nil && aiRes.Content != "" {
		raw := cleanJSONString(aiRes.Content)
		_ = json.Unmarshal([]byte(raw), &deduced)
	}

	// Fallback heuristic if LLM didn't return skills
	if len(deduced.Skills) == 0 {
		deduced.Skills = extractHeuristicSkills(messages)
	}
	if deduced.Summary == "" {
		deduced.Summary = fmt.Sprintf("Contact avec %d messages récents.", len(messages))
	}
	if len(deduced.Languages) == 0 {
		deduced.Languages = []string{"fr"}
	}

	c.AutoProfile = deduced.Summary
	c.AutoSkills = deduced.Skills
	c.AutoLanguages = deduced.Languages
	c.AutoProfession = deduced.Profession
	nowStr := time.Now().Format("2006-01-02 15:04:05")
	c.LastAnalyzedAt = nowStr

	autoSkillsJSON, _ := json.Marshal(c.AutoSkills)
	autoLangsJSON, _ := json.Marshal(c.AutoLanguages)

	_, _ = db.Exec(`
		UPDATE gafam_contacts
		SET auto_profile = ?, auto_skills = ?, auto_languages = ?, auto_profession = ?, last_analyzed_at = datetime('now'), last_analysis_ts = ?, updated_at = datetime('now')
		WHERE phone = ?
	`, c.AutoProfile, string(autoSkillsJSON), string(autoLangsJSON), c.AutoProfession, time.Now().UnixMilli(), c.Phone)

	// Update vector embedding
	fullDoc := fmt.Sprintf("Contact %s (%s). Métier: %s (Déduit: %s). Compétences: %s (Déduites: %s). Langues: %s. Résumé: %s",
		c.Name, c.Phone, c.Profession, c.AutoProfession, strings.Join(c.Skills, ", "), strings.Join(c.AutoSkills, ", "), strings.Join(c.Languages, ", "), c.AutoProfile)
	if vec, model, err := embedText(ctx, fullDoc); err == nil {
		_ = upsertEmbedding("contact", c.Phone, model, fullDoc, vec)
	}

	return &c, nil
}

func cleanJSONString(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimSuffix(s, "```")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

func extractHeuristicSkills(messages []string) []string {
	keywords := map[string][]string{
		"plomberie":        {"fuite", "robinet", "tuyau", "plombier", "eau", "chauffe-eau", "évier"},
		"électricité":      {"disjoncteur", "prise", "électricien", "câble", "tableau", "ampoule"},
		"mécanique auto":   {"voiture", "pneu", "moteur", "garage", "frein", "vidange", "révision"},
		"informatique":     {"code", "serveur", "site", "bug", "dev", "python", "javascript", "wifi", "linux"},
		"immobilier":       {"appartement", "loyer", "bail", "caution", "visite", "agence", "maison"},
		"juridique":        {"contrat", "avocat", "clause", "procès", "tribunal", "notaire"},
		"médical/santé":    {"docteur", "médecin", "rdv", "ordonnance", "médicament", "clinique", "dentiste"},
		"bâtiment/travaux": {"peinture", "carrelage", "maçon", "travaux", "chantier", "isolation"},
	}
	allText := strings.ToLower(strings.Join(messages, " "))
	var detected []string
	for skill, words := range keywords {
		for _, w := range words {
			if strings.Contains(allText, w) {
				detected = append(detected, skill)
				break
			}
		}
	}
	return detected
}

// exportContactsCSVHandler exports all contacts as standard CSV.
func exportContactsCSVHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT phone, name, COALESCE(email, ''), COALESCE(profession, ''),
		       COALESCE(skills, '[]'), COALESCE(languages, '[]'), COALESCE(notes, ''),
		       COALESCE(auto_profession, ''), COALESCE(auto_skills, '[]')
		FROM gafam_contacts
		ORDER BY name ASC
	`)
	if err != nil {
		http.Error(w, "DB error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"gafam_contacts.csv\"")

	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"phone_number", "display_name", "email", "profession", "skills", "languages", "notes", "auto_profession", "auto_skills"})

	for rows.Next() {
		var phone, name, email, prof, skillsJSON, langsJSON, notes, autoProf, autoSkillsJSON string
		if err := rows.Scan(&phone, &name, &email, &prof, &skillsJSON, &langsJSON, &notes, &autoProf, &autoSkillsJSON); err == nil {
			var skills, langs, autoSkills []string
			_ = json.Unmarshal([]byte(skillsJSON), &skills)
			_ = json.Unmarshal([]byte(langsJSON), &langs)
			_ = json.Unmarshal([]byte(autoSkillsJSON), &autoSkills)

			_ = writer.Write([]string{
				phone,
				name,
				email,
				prof,
				strings.Join(skills, "; "),
				strings.Join(langs, "; "),
				notes,
				autoProf,
				strings.Join(autoSkills, "; "),
			})
		}
	}
	writer.Flush()
}

// importContactsCSVHandler imports contacts from a CSV file.
func importContactsCSVHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		http.Error(w, "Empty CSV payload", http.StatusBadRequest)
		return
	}

	reader := csv.NewReader(bytes.NewReader(body))
	records, err := reader.ReadAll()
	if err != nil {
		http.Error(w, "CSV parse error: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(records) < 2 {
		http.Error(w, "CSV contains no data rows", http.StatusBadRequest)
		return
	}

	header := records[0]
	colMap := map[string]int{}
	for i, col := range header {
		colMap[strings.ToLower(strings.TrimSpace(col))] = i
	}

	imported := 0
	for _, row := range records[1:] {
		getVal := func(keys ...string) string {
			for _, k := range keys {
				if idx, ok := colMap[k]; ok && idx < len(row) {
					return strings.TrimSpace(row[idx])
				}
			}
			return ""
		}

		phone := getVal("phone_number", "phone", "telephone", "tel")
		if phone == "" {
			continue
		}
		name := getVal("display_name", "name", "nom")
		email := getVal("email", "mail")
		profession := getVal("profession", "metier", "job")
		notes := getVal("notes", "note", "commentaires")

		parseList := func(raw string) string {
			if raw == "" {
				return "[]"
			}
			parts := strings.Split(raw, ";")
			if len(parts) == 1 && strings.Contains(raw, ",") {
				parts = strings.Split(raw, ",")
			}
			var list []string
			for _, p := range parts {
				if s := strings.TrimSpace(p); s != "" {
					list = append(list, s)
				}
			}
			b, _ := json.Marshal(list)
			return string(b)
		}

		skillsJSON := parseList(getVal("skills", "competences"))
		langsJSON := parseList(getVal("languages", "langues"))

		_, _ = db.Exec(`
			INSERT INTO gafam_contacts (phone, name, email, profession, skills, languages, notes, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
			ON CONFLICT(phone) DO UPDATE SET
				name = CASE WHEN excluded.name != '' THEN excluded.name ELSE gafam_contacts.name END,
				email = CASE WHEN excluded.email != '' THEN excluded.email ELSE gafam_contacts.email END,
				profession = CASE WHEN excluded.profession != '' THEN excluded.profession ELSE gafam_contacts.profession END,
				skills = CASE WHEN excluded.skills != '[]' THEN excluded.skills ELSE gafam_contacts.skills END,
				languages = CASE WHEN excluded.languages != '[]' THEN excluded.languages ELSE gafam_contacts.languages END,
				notes = CASE WHEN excluded.notes != '' THEN excluded.notes ELSE gafam_contacts.notes END,
				updated_at = datetime('now')
		`, phone, name, email, profession, skillsJSON, langsJSON, notes)

		// Keep semantic memory in sync with imported rows.
		var skills, langs []string
		_ = json.Unmarshal([]byte(skillsJSON), &skills)
		_ = json.Unmarshal([]byte(langsJSON), &langs)
		go embedContactFields(name, phone, email, profession, skills, langs, notes)

		imported++
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "imported",
		"imported": imported,
	})
}

func extractToken(r *http.Request) string {
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return parts[1]
		}
	}
	return ""
}

func decryptRequestBody(r *http.Request) ([]byte, error) {
	token := extractToken(r)
	if token == "" {
		return nil, fmt.Errorf("no token")
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	var enc EncryptedPayload
	if err := json.Unmarshal(body, &enc); err != nil || enc.EncryptedData == "" || enc.IV == "" {
		return nil, fmt.Errorf("not an encrypted payload")
	}
	key := deriveKey(token)
	return decryptAESGCM(key, enc.EncryptedData, enc.IV)
}
