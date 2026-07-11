package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxLogsBytes     = int64(1 << 30) // 1 GiB ring buffer
	defaultDayLimit  = 500
	maxDayLimit      = 2000
	maxBatchEntries  = 500
)

var (
	logsDir  string
	logsMu   sync.Mutex
)

type LogEntry struct {
	Ts      int64  `json:"ts"`
	Source  string `json:"source"`  // apk | adb | event
	Level   string `json:"level"`   // D I W E V
	Tag     string `json:"tag"`
	Message string `json:"message"`
}

type LogDayInfo struct {
	Day     string `json:"day"`
	Bytes   int64  `json:"bytes"`
	Lines   int64  `json:"lines"`
	Updated string `json:"updated_at"`
}

func initLogsStore() {
	logsDir = filepath.Join(dataDir(), "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		log.Fatal("Failed to create logs dir:", err)
	}

	createLogDays := `
	CREATE TABLE IF NOT EXISTS log_days (
		day TEXT PRIMARY KEY,
		bytes INTEGER DEFAULT 0,
		lines INTEGER DEFAULT 0,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(createLogDays); err != nil {
		log.Fatal("Failed to create log_days table:", err)
	}

	// Rebuild index from existing files if table empty
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM log_days`).Scan(&count)
	if count == 0 {
		rebuildLogIndex()
	}

	log.Printf("Logs store ready at %s (quota %d bytes)", logsDir, maxLogsBytes)
}

func dataDir() string {
	if os.Getenv("ENV") == "development" {
		return "."
	}
	return "/app/data"
}

func rebuildLogIndex() {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		day := strings.TrimSuffix(e.Name(), ".jsonl")
		path := filepath.Join(logsDir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		lines := countFileLines(path)
		_, _ = db.Exec(
			`INSERT INTO log_days (day, bytes, lines, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(day) DO UPDATE SET bytes=excluded.bytes, lines=excluded.lines, updated_at=CURRENT_TIMESTAMP`,
			day, info.Size(), lines,
		)
	}
}

func countFileLines(path string) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	var n int64
	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		n++
	}
	return n
}

func dayFilePath(day string) string {
	return filepath.Join(logsDir, day+".jsonl")
}

func appendLogEntries(entries []LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	logsMu.Lock()
	defer logsMu.Unlock()

	byDay := map[string][]LogEntry{}
	for _, e := range entries {
		if e.Ts == 0 {
			e.Ts = time.Now().UnixMilli()
		}
		if e.Source == "" {
			e.Source = "apk"
		}
		if e.Level == "" {
			e.Level = "I"
		}
		if e.Tag == "" {
			e.Tag = "GAFAM"
		}
		// Cap message size to avoid abuse
		if len(e.Message) > 8000 {
			e.Message = e.Message[:8000] + "…[truncated]"
		}
		day := time.UnixMilli(e.Ts).UTC().Format("2006-01-02")
		byDay[day] = append(byDay[day], e)
	}

	for day, dayEntries := range byDay {
		path := dayFilePath(day)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		var written int64
		var lines int64
		w := bufio.NewWriter(f)
		for _, e := range dayEntries {
			b, err := json.Marshal(e)
			if err != nil {
				continue
			}
			n, err := w.Write(append(b, '\n'))
			if err != nil {
				f.Close()
				return err
			}
			written += int64(n)
			lines++
		}
		if err := w.Flush(); err != nil {
			f.Close()
			return err
		}
		f.Close()

		_, err = db.Exec(
			`INSERT INTO log_days (day, bytes, lines, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(day) DO UPDATE SET
			   bytes = bytes + excluded.bytes,
			   lines = lines + excluded.lines,
			   updated_at = CURRENT_TIMESTAMP`,
			day, written, lines,
		)
		if err != nil {
			return err
		}
	}

	enforceLogQuota()
	return nil
}

func enforceLogQuota() {
	var total int64
	_ = db.QueryRow(`SELECT COALESCE(SUM(bytes), 0) FROM log_days`).Scan(&total)
	if total <= maxLogsBytes {
		return
	}

	rows, err := db.Query(`SELECT day, bytes FROM log_days ORDER BY day ASC`)
	if err != nil {
		return
	}
	defer rows.Close()

	type dayBytes struct {
		day   string
		bytes int64
	}
	var days []dayBytes
	for rows.Next() {
		var d dayBytes
		if err := rows.Scan(&d.day, &d.bytes); err == nil {
			days = append(days, d)
		}
	}

	for _, d := range days {
		if total <= maxLogsBytes {
			break
		}
		path := dayFilePath(d.day)
		_ = os.Remove(path)
		_, _ = db.Exec(`DELETE FROM log_days WHERE day = ?`, d.day)
		total -= d.bytes
		log.Printf("Logs quota: erased day %s (%d bytes)", d.day, d.bytes)
	}
}

func listLogDays() ([]LogDayInfo, int64, error) {
	rows, err := db.Query(`SELECT day, bytes, lines, updated_at FROM log_days ORDER BY day DESC`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []LogDayInfo
	var total int64
	for rows.Next() {
		var d LogDayInfo
		if err := rows.Scan(&d.Day, &d.Bytes, &d.Lines, &d.Updated); err != nil {
			continue
		}
		out = append(out, d)
		total += d.Bytes
	}
	if out == nil {
		out = []LogDayInfo{}
	}
	return out, total, nil
}

func readLogDay(day string, offset, limit int) ([]LogEntry, int64, error) {
	if !validDay(day) {
		return nil, 0, fmt.Errorf("invalid day")
	}
	path := dayFilePath(day)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []LogEntry{}, 0, nil
		}
		return nil, 0, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	var all []LogEntry
	for sc.Scan() {
		var e LogEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue
		}
		all = append(all, e)
	}
	total := int64(len(all))
	if total == 0 {
		return []LogEntry{}, 0, nil
	}

	// Newest first
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}

	if offset >= len(all) {
		return []LogEntry{}, total, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], total, nil
}

func validDay(day string) bool {
	_, err := time.Parse("2006-01-02", day)
	return err == nil
}

// --- Handlers ---

// POST /api/auth/logs — APK / ADB bridge (JWT_SECRET)
func postLogsHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20)) // 2 MiB max batch
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	var entries []LogEntry

	// Prefer encrypted envelope (same as SMS)
	var enc EncryptedPayload
	if err := json.Unmarshal(body, &enc); err == nil && enc.EncryptedData != "" && enc.IV != "" {
		key := deriveKey(string(jwtSecret))
		plaintext, err := decryptAESGCM(key, enc.EncryptedData, enc.IV)
		if err != nil {
			http.Error(w, "Decryption failed", http.StatusForbidden)
			return
		}
		var batch struct {
			Entries []LogEntry `json:"entries"`
		}
		if err := json.Unmarshal(plaintext, &batch); err != nil {
			// Allow raw array
			if err2 := json.Unmarshal(plaintext, &entries); err2 != nil {
				http.Error(w, "Invalid log payload", http.StatusBadRequest)
				return
			}
		} else {
			entries = batch.Entries
		}
	} else {
		// Plain JSON fallback (dev / bridge)
		var batch struct {
			Entries []LogEntry `json:"entries"`
		}
		if err := json.Unmarshal(body, &batch); err == nil && len(batch.Entries) > 0 {
			entries = batch.Entries
		} else if err := json.Unmarshal(body, &entries); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
	}

	if len(entries) > maxBatchEntries {
		entries = entries[:maxBatchEntries]
	}

	if err := appendLogEntries(entries); err != nil {
		log.Println("appendLogEntries:", err)
		http.Error(w, "Failed to store logs", http.StatusInternalServerError)
		return
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"stored":  len(entries),
	})
}

// GET /api/web/logs — list days or day content (session)
func getWebLogsHandler(w http.ResponseWriter, r *http.Request) {
	day := r.URL.Query().Get("day")

	if day == "" {
		days, total, err := listLogDays()
		if err != nil {
			http.Error(w, "Failed to list logs", http.StatusInternalServerError)
			return
		}
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"days":       days,
			"total_bytes": total,
			"quota_bytes": maxLogsBytes,
		})
		return
	}

	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = defaultDayLimit
	}
	if limit > maxDayLimit {
		limit = maxDayLimit
	}

	entries, totalLines, err := readLogDay(day, offset, limit)
	if err != nil {
		http.Error(w, "Failed to read day", http.StatusBadRequest)
		return
	}

	// Newest first for UI (file is append-only chronological)
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"day":         day,
		"entries":     entries,
		"offset":      offset,
		"limit":       limit,
		"total_lines": totalLines,
	})
}

// DELETE /api/web/logs — clear one day (?day=) or all logs
func deleteWebLogsHandler(w http.ResponseWriter, r *http.Request) {
	day := r.URL.Query().Get("day")

	logsMu.Lock()
	defer logsMu.Unlock()

	if day != "" {
		if !validDay(day) {
			http.Error(w, "Invalid day", http.StatusBadRequest)
			return
		}
		_ = os.Remove(dayFilePath(day))
		_, _ = db.Exec(`DELETE FROM log_days WHERE day = ?`, day)
		sendJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "cleared": day})
		return
	}

	entries, err := os.ReadDir(logsDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
				_ = os.Remove(filepath.Join(logsDir, e.Name()))
			}
		}
	}
	_, _ = db.Exec(`DELETE FROM log_days`)
	sendJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "cleared": "all"})
}
