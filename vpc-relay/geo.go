package main

// Local geocoding (GeoNames FR) + OSM tile cache on the VPC.
// Dump downloaded once into SQLite FTS5; tiles cached under /data/geo/tiles.

import (
	"archive/zip"
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	geoNamesFRURL = "https://download.geonames.org/export/dump/FR.zip"
	geoNamesMCURL = "https://download.geonames.org/export/dump/MC.zip"
	osmTileUA     = "GAFAM-Relay/1.0 (personal VPC tile cache)"
)

var (
	geoImporting atomic.Bool
	geoImportMu  sync.Mutex
)

func geoDir() string {
	return filepath.Join(dataDir(), "geo")
}

func geoTilesDir() string {
	return filepath.Join(geoDir(), "tiles")
}

func initGeo() {
	if err := os.MkdirAll(geoTilesDir(), 0o755); err != nil {
		log.Printf("geo: mkdir: %v", err)
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS gafam_geonames (
		geoname_id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		asciiname TEXT DEFAULT '',
		admin1 TEXT DEFAULT '',
		country TEXT DEFAULT 'FR',
		lat REAL NOT NULL,
		lon REAL NOT NULL,
		feature TEXT DEFAULT '',
		population INTEGER DEFAULT 0
	)`); err != nil {
		log.Printf("geo: table: %v", err)
		return
	}

	if _, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS gafam_geonames_fts USING fts5(
		name, asciiname, admin1,
		content='gafam_geonames',
		content_rowid='geoname_id'
	)`); err != nil {
		log.Printf("geo: fts5: %v", err)
	}

	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM gafam_geonames`).Scan(&n)
	if n == 0 {
		log.Println("geo: empty — scheduling GeoNames FR import")
		go runGeoImport(false)
	} else {
		log.Printf("geo: ready (%d places)", n)
	}
}

type GeoResult struct {
	Name    string  `json:"name"`
	Display string  `json:"display"`
	Admin1  string  `json:"admin1,omitempty"`
	Country string  `json:"country,omitempty"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Pop     int     `json:"population,omitempty"`
	Source  string  `json:"source"`
}

func geoStatusHandler(w http.ResponseWriter, r *http.Request) {
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM gafam_geonames`).Scan(&count)
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"imported":     count > 0,
		"count":        count,
		"tiles_cached": countCachedTiles(),
		"importing":    geoImporting.Load(),
	})
}

func geoImportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if geoImporting.Load() {
		sendJSON(w, http.StatusConflict, map[string]string{"error": "import already running"})
		return
	}
	go runGeoImport(true)
	sendJSON(w, http.StatusAccepted, map[string]string{"status": "import_started"})
}

func geoSearchHandler(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		sendJSON(w, http.StatusOK, map[string]interface{}{"results": []GeoResult{}})
		return
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}
	sendJSON(w, http.StatusOK, map[string]interface{}{"results": searchGeonames(q, limit)})
}

func searchGeonames(q string, limit int) []GeoResult {
	out := []GeoResult{}
	seen := map[int64]bool{}

	ftsQ := ftsQuery(q)
	if ftsQ != "" {
		rows, err := db.Query(`
			SELECT g.geoname_id, g.name, g.admin1, g.country, g.lat, g.lon, g.population
			FROM gafam_geonames_fts f
			JOIN gafam_geonames g ON g.geoname_id = f.rowid
			WHERE gafam_geonames_fts MATCH ?
			ORDER BY g.population DESC
			LIMIT ?`, ftsQ, limit)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id int64
				var name, admin1, country string
				var lat, lon float64
				var pop int
				if rows.Scan(&id, &name, &admin1, &country, &lat, &lon, &pop) != nil {
					continue
				}
				seen[id] = true
				out = append(out, geoDisplay(name, admin1, country, lat, lon, pop))
			}
		}
	}

	if len(out) < limit {
		like := "%" + q + "%"
		rows, err := db.Query(`
			SELECT geoname_id, name, admin1, country, lat, lon, population
			FROM gafam_geonames
			WHERE name LIKE ? OR asciiname LIKE ? OR admin1 LIKE ?
			ORDER BY population DESC
			LIMIT ?`, like, like, like, limit)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id int64
				var name, admin1, country string
				var lat, lon float64
				var pop int
				if rows.Scan(&id, &name, &admin1, &country, &lat, &lon, &pop) != nil {
					continue
				}
				if seen[id] {
					continue
				}
				seen[id] = true
				out = append(out, geoDisplay(name, admin1, country, lat, lon, pop))
				if len(out) >= limit {
					break
				}
			}
		}
	}
	return out
}

func geoDisplay(name, admin1, country string, lat, lon float64, pop int) GeoResult {
	disp := name
	if admin1 != "" {
		disp = name + ", " + admin1
	}
	if country != "" && country != "FR" {
		disp += " (" + country + ")"
	}
	return GeoResult{
		Name: name, Display: disp, Admin1: admin1, Country: country,
		Lat: lat, Lon: lon, Pop: pop, Source: "geonames",
	}
}

func ftsQuery(q string) string {
	parts := strings.Fields(strings.ToLower(q))
	if len(parts) == 0 {
		return ""
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		cleaned := strings.Builder{}
		for _, r := range p {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r > 127 {
				cleaned.WriteRune(r)
			}
		}
		s := cleaned.String()
		if s == "" {
			continue
		}
		out = append(out, `"`+strings.ReplaceAll(s, `"`, "")+`"*`)
	}
	return strings.Join(out, " ")
}

func geoTilesHandler(w http.ResponseWriter, r *http.Request) {
	z := r.PathValue("z")
	x := r.PathValue("x")
	yRaw := r.PathValue("y")
	yRaw = strings.TrimSuffix(yRaw, ".png")

	zi, errZ := strconv.Atoi(z)
	xi, errX := strconv.Atoi(x)
	yi, errY := strconv.Atoi(yRaw)
	if errZ != nil || errX != nil || errY != nil || zi < 0 || zi > 18 || xi < 0 || yi < 0 {
		http.Error(w, "bad tile coords", http.StatusBadRequest)
		return
	}

	cachePath := filepath.Join(geoTilesDir(), z, x, fmt.Sprintf("%d.png", yi))
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("X-Geo-Cache", "HIT")
		_, _ = w.Write(data)
		return
	}

	url := fmt.Sprintf("https://tile.openstreetmap.org/%d/%d/%d.png", zi, xi, yi)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		http.Error(w, "tile request failed", http.StatusBadGateway)
		return
	}
	req.Header.Set("User-Agent", osmTileUA)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "tile upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, "tile not found", resp.StatusCode)
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil || len(data) == 0 {
		http.Error(w, "tile read failed", http.StatusBadGateway)
		return
	}
	_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
	_ = os.WriteFile(cachePath, data, 0o644)

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Geo-Cache", "MISS")
	_, _ = w.Write(data)
}

func countCachedTiles() int {
	n := 0
	_ = filepath.Walk(geoTilesDir(), func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".png") {
			n++
		}
		return nil
	})
	return n
}

func runGeoImport(force bool) {
	if !geoImporting.CompareAndSwap(false, true) {
		return
	}
	defer geoImporting.Store(false)

	geoImportMu.Lock()
	defer geoImportMu.Unlock()

	if !force {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM gafam_geonames`).Scan(&n)
		if n > 0 {
			return
		}
	}

	log.Println("geo: importing GeoNames FR (+ MC)…")
	tmpDir := filepath.Join(geoDir(), "import")
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		log.Printf("geo: import mkdir: %v", err)
		return
	}
	defer os.RemoveAll(tmpDir)

	frTxt, err := downloadAndExtractGeo(geoNamesFRURL, tmpDir, "FR.txt")
	if err != nil {
		log.Printf("geo: FR download: %v", err)
		return
	}
	mcTxt, _ := downloadAndExtractGeo(geoNamesMCURL, tmpDir, "MC.txt")

	tx, err := db.Begin()
	if err != nil {
		log.Printf("geo: begin: %v", err)
		return
	}
	if force {
		_, _ = tx.Exec(`DELETE FROM gafam_geonames`)
		_, _ = tx.Exec(`DELETE FROM gafam_geonames_fts`)
	}

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO gafam_geonames
		(geoname_id, name, asciiname, admin1, country, lat, lon, feature, population)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		log.Printf("geo: prepare: %v", err)
		return
	}

	inserted := 0
	for _, path := range []string{frTxt, mcTxt} {
		if path == "" {
			continue
		}
		n, err := loadGeonamesFile(path, stmt)
		if err != nil {
			log.Printf("geo: parse %s: %v", path, err)
			continue
		}
		inserted += n
	}
	_ = stmt.Close()

	_, _ = tx.Exec(`INSERT INTO gafam_geonames_fts(gafam_geonames_fts) VALUES('rebuild')`)
	if err := tx.Commit(); err != nil {
		log.Printf("geo: commit: %v", err)
		return
	}
	log.Printf("geo: import done (%d places)", inserted)
}

func downloadAndExtractGeo(url, destDir, wantFile string) (string, error) {
	zipPath := filepath.Join(destDir, filepath.Base(url))
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", osmTileUA)
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 80<<20)); err != nil {
		f.Close()
		return "", err
	}
	f.Close()

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if filepath.Base(zf.Name) != wantFile {
			continue
		}
		outPath := filepath.Join(destDir, wantFile)
		rc, err := zf.Open()
		if err != nil {
			return "", err
		}
		out, err := os.Create(outPath)
		if err != nil {
			rc.Close()
			return "", err
		}
		_, err = io.Copy(out, io.LimitReader(rc, 120<<20))
		rc.Close()
		out.Close()
		if err != nil {
			return "", err
		}
		return outPath, nil
	}
	return "", fmt.Errorf("%s not in zip", wantFile)
}

// loadGeonamesFile keeps only populated places (feature class P) for a light DB.
func loadGeonamesFile(path string, stmt *sql.Stmt) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 256*1024)
	sc.Buffer(buf, 1024*1024)
	n := 0
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 15 {
			continue
		}
		// 6 = feature class, 7 = feature code
		if cols[6] != "P" {
			continue
		}
		id, err := strconv.ParseInt(cols[0], 10, 64)
		if err != nil {
			continue
		}
		lat, err1 := strconv.ParseFloat(cols[4], 64)
		lon, err2 := strconv.ParseFloat(cols[5], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		pop, _ := strconv.Atoi(cols[14])
		name := cols[1]
		ascii := cols[2]
		country := cols[8]
		admin1 := cols[10]
		feature := cols[6] + "." + cols[7]
		if _, err := stmt.Exec(id, name, ascii, admin1, country, lat, lon, feature, pop); err != nil {
			continue
		}
		n++
	}
	return n, sc.Err()
}
