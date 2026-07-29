package main

// Local geocoding: bundled worldwide GeoNames (cities500) + OSM tile cache.
// The SQLite dump lives in geo-data/geonames.sqlite.gz (vendored in git / Docker image).
// No runtime download from geonames.org.

import (
	"compress/gzip"
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
	osmTileUA        = "GAFAM-Relay/1.0 (personal VPC tile cache)"
	geoBundleVersion = "cities500-v1"
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

// geoBundlePath looks for the vendored dump (Docker image or cwd for local dev).
func geoBundlePath() string {
	candidates := []string{
		"/app/geo-data/geonames.sqlite.gz",
		filepath.Join(dataDir(), "geo-data", "geonames.sqlite.gz"),
		"geo-data/geonames.sqlite.gz",
		filepath.Join("vpc-relay", "geo-data", "geonames.sqlite.gz"),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Size() > 0 {
			return p
		}
	}
	return ""
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
		country TEXT DEFAULT '',
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
	var ver string
	_ = db.QueryRow(`SELECT value FROM gafam_settings WHERE key = 'geo_bundle_version'`).Scan(&ver)
	if n == 0 || ver != geoBundleVersion {
		log.Printf("geo: seeding bundled GeoNames (rows=%d ver=%q → %s)", n, ver, geoBundleVersion)
		go runGeoSeed(true)
	} else {
		log.Printf("geo: ready (%d places, %s)", n, geoBundleVersion)
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
	var countries int
	_ = db.QueryRow(`SELECT COUNT(DISTINCT country) FROM gafam_geonames`).Scan(&countries)
	bundle := geoBundlePath()
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"imported":     count > 0,
		"count":        count,
		"countries":    countries,
		"bundle":       bundle != "",
		"bundle_path":  bundle,
		"source":       "cities500 (worldwide, bundled)",
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
		sendJSON(w, http.StatusConflict, map[string]string{"error": "seed already running"})
		return
	}
	go runGeoSeed(true)
	sendJSON(w, http.StatusAccepted, map[string]string{"status": "seed_started", "source": "local_bundle"})
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
			WHERE name LIKE ? OR asciiname LIKE ? OR admin1 LIKE ? OR country LIKE ?
			ORDER BY population DESC
			LIMIT ?`, like, like, like, like, limit)
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
	if country != "" {
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
	yRaw := strings.TrimSuffix(r.PathValue("y"), ".png")

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

// runGeoSeed loads the vendored gzip SQLite into gafam_geonames (offline).
func runGeoSeed(force bool) {
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

	bundle := geoBundlePath()
	if bundle == "" {
		log.Println("geo: no bundled geonames.sqlite.gz found — search disabled until image includes geo-data/")
		return
	}

	log.Printf("geo: seeding from %s …", bundle)
	tmpDir := filepath.Join(geoDir(), "seed")
	_ = os.MkdirAll(tmpDir, 0o755)
	sqlitePath := filepath.Join(tmpDir, "geonames.sqlite")
	if err := gunzipFile(bundle, sqlitePath); err != nil {
		log.Printf("geo: gunzip: %v", err)
		return
	}
	defer os.Remove(sqlitePath)

	src, err := sql.Open("sqlite", sqlitePath+"?mode=ro")
	if err != nil {
		log.Printf("geo: open bundle: %v", err)
		return
	}
	defer src.Close()

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

	rows, err := src.Query(`SELECT geoname_id, name, asciiname, admin1, country, lat, lon, feature, population FROM gafam_geonames`)
	if err != nil {
		stmt.Close()
		_ = tx.Rollback()
		log.Printf("geo: bundle query: %v", err)
		return
	}
	inserted := 0
	for rows.Next() {
		var id int64
		var name, ascii, admin1, country, feature string
		var lat, lon float64
		var pop int
		if rows.Scan(&id, &name, &ascii, &admin1, &country, &lat, &lon, &feature, &pop) != nil {
			continue
		}
		if _, err := stmt.Exec(id, name, ascii, admin1, country, lat, lon, feature, pop); err != nil {
			continue
		}
		inserted++
	}
	rows.Close()
	stmt.Close()

	_, _ = tx.Exec(`INSERT INTO gafam_geonames_fts(gafam_geonames_fts) VALUES('rebuild')`)
	if err := tx.Commit(); err != nil {
		log.Printf("geo: commit: %v", err)
		return
	}
	log.Printf("geo: seeded %d places from bundle (offline)", inserted)
	_, _ = db.Exec(
		`INSERT INTO gafam_settings (key, value) VALUES ('geo_bundle_version', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		geoBundleVersion,
	)
}

func gunzipFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	gr, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer gr.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, io.LimitReader(gr, 200<<20))
	return err
}
