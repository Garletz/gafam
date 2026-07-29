// Build a serious offline GeoNames SQLite for the VPC image.
//
// Strategy (local / no runtime download):
//   1. Full country dumps for FR+neighbors (P/S/L features) → precise villages & POIs
//   2. cities500 for the rest of the world → coverage without exploding size
//   3. Resolve admin1 codes → human names
//
// Usage (from vpc-relay/): go run scripts/build_geonames.go
// Output: geo-data/geonames.sqlite.gz
package main

import (
	"archive/zip"
	"bufio"
	"compress/gzip"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

const (
	geonamesBase = "https://download.geonames.org/export/dump/"
	cities500URL = geonamesBase + "cities500.zip"
	admin1URL    = geonamesBase + "admin1CodesASCII.txt"
)

// Detailed dumps: francophone / nearby — all villages + spots + areas.
var detailCountries = []string{"FR", "BE", "CH", "LU", "MC", "AD", "LI"}

// Keep place-like feature classes only (drop streams/roads/etc.).
var keepFeature = map[string]bool{
	"P": true, // city, village
	"S": true, // spot, building, farm, …
	"L": true, // parks, areas
	"T": true, // mountain, island, rock
}

func main() {
	root, _ := os.Getwd()
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		root = filepath.Join(root, "vpc-relay")
	}
	outDir := filepath.Join(root, "geo-data")
	_ = os.MkdirAll(outDir, 0o755)
	tmpDir, err := os.MkdirTemp("", "gafam-geonames-*")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	fmt.Println("downloading admin1 codes…")
	admin1Path := filepath.Join(tmpDir, "admin1CodesASCII.txt")
	if err := download(admin1URL, admin1Path, 8<<20); err != nil {
		fatal(err)
	}
	admin1 := loadAdmin1(admin1Path)
	fmt.Printf("  %d admin1 names\n", len(admin1))

	detailSet := map[string]bool{}
	for _, cc := range detailCountries {
		detailSet[cc] = true
	}

	dbPath := filepath.Join(tmpDir, "geonames.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fatal(err)
	}
	_, _ = db.Exec(`PRAGMA journal_mode=OFF; PRAGMA synchronous=OFF;`)
	if _, err := db.Exec(`CREATE TABLE gafam_geonames (
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
		fatal(err)
	}

	tx, err := db.Begin()
	if err != nil {
		fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO gafam_geonames
		(geoname_id, name, asciiname, admin1, country, lat, lon, feature, population)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		fatal(err)
	}

	total := 0
	for _, cc := range detailCountries {
		fmt.Printf("downloading %s.zip (detail P/S/L/T)…\n", cc)
		zipPath := filepath.Join(tmpDir, cc+".zip")
		if err := download(geonamesBase+cc+".zip", zipPath, 40<<20); err != nil {
			fmt.Printf("  skip %s: %v\n", cc, err)
			continue
		}
		txt, err := unzipOne(zipPath, tmpDir, cc+".txt")
		if err != nil {
			fmt.Printf("  skip %s: %v\n", cc, err)
			continue
		}
		n, err := ingestFile(txt, stmt, admin1, true, nil)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("  %s → %d rows\n", cc, n)
		total += n
	}

	fmt.Println("downloading cities500.zip (world fallback)…")
	zipPath := filepath.Join(tmpDir, "cities500.zip")
	if err := download(cities500URL, zipPath, 40<<20); err != nil {
		fatal(err)
	}
	txt, err := unzipOne(zipPath, tmpDir, "cities500.txt")
	if err != nil {
		fatal(err)
	}
	n, err := ingestFile(txt, stmt, admin1, false, detailSet)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("  cities500 (excl. detail countries) → %d rows\n", n)
	total += n

	stmt.Close()
	if err := tx.Commit(); err != nil {
		fatal(err)
	}

	fmt.Println("building FTS5…")
	if _, err := db.Exec(`CREATE VIRTUAL TABLE gafam_geonames_fts USING fts5(
		name, asciiname, admin1, country,
		content='gafam_geonames',
		content_rowid='geoname_id'
	)`); err != nil {
		fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gafam_geonames_fts(gafam_geonames_fts) VALUES('rebuild')`); err != nil {
		fatal(err)
	}
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_geo_country ON gafam_geonames(country)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_geo_pop ON gafam_geonames(population DESC)`)
	_, _ = db.Exec(`VACUUM`)
	db.Close()

	gzPath := filepath.Join(outDir, "geonames.sqlite.gz")
	if err := gzipFile(dbPath, gzPath); err != nil {
		fatal(err)
	}
	info, _ := os.Stat(gzPath)
	fmt.Printf("wrote %s (%d places, %.1f MiB gzip)\n", gzPath, total, float64(info.Size())/(1<<20))
}

func ingestFile(
	path string,
	stmt *sql.Stmt,
	admin1 map[string]string,
	filterFeatures bool,
	skipCountries map[string]bool,
) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 512*1024)
	sc.Buffer(buf, 2*1024*1024)
	n := 0
	for sc.Scan() {
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) < 15 {
			continue
		}
		country := cols[8]
		if skipCountries != nil && skipCountries[country] {
			continue
		}
		fc := cols[6]
		if filterFeatures && !keepFeature[fc] {
			continue
		}
		id, err := strconv.ParseInt(cols[0], 10, 64)
		if err != nil {
			continue
		}
		lat, e1 := strconv.ParseFloat(cols[4], 64)
		lon, e2 := strconv.ParseFloat(cols[5], 64)
		if e1 != nil || e2 != nil {
			continue
		}
		pop, _ := strconv.Atoi(cols[14])
		feature := fc + "." + cols[7]
		adminCode := cols[10]
		adminName := admin1[country+"."+adminCode]
		if adminName == "" {
			adminName = adminCode
		}
		if _, err := stmt.Exec(id, cols[1], cols[2], adminName, country, lat, lon, feature, pop); err != nil {
			continue
		}
		n++
	}
	return n, sc.Err()
}

func loadAdmin1(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		// code\tname\tnameAscii\tgeonameId
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) < 2 {
			continue
		}
		out[cols[0]] = cols[1]
	}
	return out
}

func download(url, dest string, maxBytes int64) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "GAFAM-Relay/geonames-builder")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, io.LimitReader(resp.Body, maxBytes))
	return err
}

func unzipOne(zipPath, destDir, want string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if filepath.Base(zf.Name) != want {
			continue
		}
		out := filepath.Join(destDir, want)
		rc, err := zf.Open()
		if err != nil {
			return "", err
		}
		f, err := os.Create(out)
		if err != nil {
			rc.Close()
			return "", err
		}
		_, err = io.Copy(f, io.LimitReader(rc, 200<<20))
		rc.Close()
		f.Close()
		return out, err
	}
	return "", fmt.Errorf("%s not in zip", want)
}

func gzipFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := gzip.NewWriter(out)
	zw.Name = "geonames.sqlite"
	if _, err := io.Copy(zw, in); err != nil {
		zw.Close()
		return err
	}
	return zw.Close()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
