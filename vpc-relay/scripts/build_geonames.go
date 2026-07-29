// Build worldwide GeoNames SQLite (cities500) for bundling in the repo.
// Usage: go run ./scripts/build_geonames.go
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

const cities500URL = "https://download.geonames.org/export/dump/cities500.zip"

func main() {
	root, _ := os.Getwd()
	// allow running from repo root or vpc-relay/
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

	fmt.Println("downloading cities500.zip (worldwide, pop≥500)…")
	zipPath := filepath.Join(tmpDir, "cities500.zip")
	if err := download(cities500URL, zipPath); err != nil {
		fatal(err)
	}
	txtPath, err := unzipOne(zipPath, tmpDir, "cities500.txt")
	if err != nil {
		fatal(err)
	}

	dbPath := filepath.Join(tmpDir, "geonames.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fatal(err)
	}
	defer db.Close()

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
	stmt, err := tx.Prepare(`INSERT INTO gafam_geonames
		(geoname_id, name, asciiname, admin1, country, lat, lon, feature, population)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		fatal(err)
	}

	f, err := os.Open(txtPath)
	if err != nil {
		fatal(err)
	}
	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 256*1024)
	sc.Buffer(buf, 1024*1024)
	n := 0
	for sc.Scan() {
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) < 15 {
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
		feature := cols[6] + "." + cols[7]
		if _, err := stmt.Exec(id, cols[1], cols[2], cols[10], cols[8], lat, lon, feature, pop); err != nil {
			continue
		}
		n++
	}
	f.Close()
	stmt.Close()
	if err := tx.Commit(); err != nil {
		fatal(err)
	}
	if err := sc.Err(); err != nil {
		fatal(err)
	}

	if _, err := db.Exec(`CREATE VIRTUAL TABLE gafam_geonames_fts USING fts5(
		name, asciiname, admin1,
		content='gafam_geonames',
		content_rowid='geoname_id'
	)`); err != nil {
		fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gafam_geonames_fts(gafam_geonames_fts) VALUES('rebuild')`); err != nil {
		fatal(err)
	}
	_, _ = db.Exec(`VACUUM`)
	db.Close()

	gzPath := filepath.Join(outDir, "geonames.sqlite.gz")
	if err := gzipFile(dbPath, gzPath); err != nil {
		fatal(err)
	}
	info, _ := os.Stat(gzPath)
	fmt.Printf("wrote %s (%d places, %.1f MiB gzip)\n", gzPath, n, float64(info.Size())/(1<<20))
}

func download(url, dest string) error {
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
	_, err = io.Copy(f, io.LimitReader(resp.Body, 80<<20))
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
		_, err = io.Copy(f, io.LimitReader(rc, 120<<20))
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
