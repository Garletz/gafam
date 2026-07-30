package main

// Sync basemap.pmtiles from a GitHub Release (sharded parts ≤95 MiB).
// Manifest ships in the image; shards download onto the VPC volume once.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type pmtilesManifest struct {
	Version  string                `json:"version"`
	Tag      string                `json:"tag"`
	BaseURL  string                `json:"base_url"`
	Filename string                `json:"filename"`
	Bytes    int64                 `json:"bytes"`
	SHA256   string                `json:"sha256"`
	PartSize int64                 `json:"part_size"`
	Parts    []pmtilesManifestPart `json:"parts"`
}

type pmtilesManifestPart struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type pmtilesSyncState struct {
	Syncing  atomic.Bool
	Progress atomic.Int32 // 0–100
	Done     atomic.Int32 // parts completed
	Total    atomic.Int32
	Err      atomic.Value // string
	Version  atomic.Value // string
}

var pmtilesSync pmtilesSyncState

func init() {
	pmtilesSync.Err.Store("")
	pmtilesSync.Version.Store("")
}

func geoPmtilesManifestPath() string {
	candidates := []string{
		"/app/geo-data/pmtiles-manifest.json",
		filepath.Join("geo-data", "pmtiles-manifest.json"),
		filepath.Join("vpc-relay", "geo-data", "pmtiles-manifest.json"),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func loadPmtilesManifest() (*pmtilesManifest, error) {
	p := geoPmtilesManifestPath()
	if p == "" {
		return nil, fmt.Errorf("pmtiles-manifest.json not found in image")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var m pmtilesManifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.BaseURL == "" || len(m.Parts) == 0 || m.SHA256 == "" {
		return nil, fmt.Errorf("invalid pmtiles manifest")
	}
	return &m, nil
}

func geoPmtilesPartsDir() string {
	return filepath.Join(geoDir(), "pmtiles-parts")
}

func geoPmtilesTarget() string {
	return filepath.Join(geoDir(), "basemap.pmtiles")
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func pmtilesAlreadyReady(m *pmtilesManifest) bool {
	p := geoPmtilesPath()
	if p == "" {
		return false
	}
	st, err := os.Stat(p)
	if err != nil || st.Size() != m.Bytes {
		return false
	}
	var ver string
	_ = db.QueryRow(`SELECT value FROM gafam_settings WHERE key = 'geo_pmtiles_version'`).Scan(&ver)
	if ver == m.Version {
		return true
	}
	// Version unset but size matches — verify hash once
	sum, err := fileSHA256(p)
	if err != nil || sum != m.SHA256 {
		return false
	}
	_, _ = db.Exec(`INSERT INTO gafam_settings(key, value) VALUES('geo_pmtiles_version', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, m.Version)
	return true
}

func geoPmtilesSyncStatus() map[string]interface{} {
	errStr, _ := pmtilesSync.Err.Load().(string)
	ver, _ := pmtilesSync.Version.Load().(string)
	out := map[string]interface{}{
		"syncing":  pmtilesSync.Syncing.Load(),
		"progress": pmtilesSync.Progress.Load(),
		"done":     pmtilesSync.Done.Load(),
		"total":    pmtilesSync.Total.Load(),
		"error":    errStr,
		"version":  ver,
	}
	if m, err := loadPmtilesManifest(); err == nil {
		out["manifest_version"] = m.Version
		out["manifest_bytes"] = m.Bytes
		out["base_url"] = m.BaseURL
		if ver == "" {
			var stored string
			_ = db.QueryRow(`SELECT value FROM gafam_settings WHERE key = 'geo_pmtiles_version'`).Scan(&stored)
			out["version"] = stored
		}
	}
	return out
}

// Enrich existing geoPmtilesStatus with sync fields.
func mergePmtilesStatus(base map[string]interface{}) map[string]interface{} {
	for k, v := range geoPmtilesSyncStatus() {
		base[k] = v
	}
	base["ready"] = base["pmtiles"] == true && !pmtilesSync.Syncing.Load()
	return base
}

func geoPmtilesStatusHandler(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, http.StatusOK, mergePmtilesStatus(geoPmtilesStatus()))
}

func geoPmtilesSyncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	m, err := loadPmtilesManifest()
	if err != nil {
		sendJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	if pmtilesAlreadyReady(m) {
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "already_ready",
			"version": m.Version,
			"bytes":   m.Bytes,
		})
		return
	}
	if !pmtilesSync.Syncing.CompareAndSwap(false, true) {
		sendJSON(w, http.StatusConflict, map[string]interface{}{
			"status":   "already_syncing",
			"progress": pmtilesSync.Progress.Load(),
		})
		return
	}
	go syncPmtilesFromRelease(m)
	sendJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":  "started",
		"version": m.Version,
		"parts":   len(m.Parts),
	})
}

var pmtilesSyncMu sync.Mutex

func syncPmtilesFromRelease(m *pmtilesManifest) {
	pmtilesSyncMu.Lock()
	defer pmtilesSyncMu.Unlock()
	defer pmtilesSync.Syncing.Store(false)

	pmtilesSync.Err.Store("")
	pmtilesSync.Version.Store(m.Version)
	pmtilesSync.Total.Store(int32(len(m.Parts)))
	pmtilesSync.Done.Store(0)
	pmtilesSync.Progress.Store(0)

	log.Printf("geo-pmtiles: sync start version=%s parts=%d bytes=%d", m.Version, len(m.Parts), m.Bytes)
	partsDir := geoPmtilesPartsDir()
	if err := os.MkdirAll(partsDir, 0o755); err != nil {
		pmtilesSync.Err.Store(err.Error())
		log.Printf("geo-pmtiles: mkdir parts: %v", err)
		return
	}
	_ = os.MkdirAll(geoDir(), 0o755)

	client := &http.Client{Timeout: 30 * time.Minute}
	for i, part := range m.Parts {
		partPath := filepath.Join(partsDir, part.Name)
		if st, err := os.Stat(partPath); err == nil && st.Size() == part.Bytes {
			if sum, err := fileSHA256(partPath); err == nil && sum == part.SHA256 {
				pmtilesSync.Done.Store(int32(i + 1))
				pmtilesSync.Progress.Store(int32((i + 1) * 100 / len(m.Parts)))
				continue
			}
		}
		url := m.BaseURL + "/" + part.Name
		if err := downloadPmtilesPart(client, url, partPath, part); err != nil {
			pmtilesSync.Err.Store(fmt.Sprintf("part %s: %v", part.Name, err))
			log.Printf("geo-pmtiles: download %s: %v", part.Name, err)
			return
		}
		pmtilesSync.Done.Store(int32(i + 1))
		pmtilesSync.Progress.Store(int32((i + 1) * 90 / len(m.Parts))) // leave 10% for assemble
		log.Printf("geo-pmtiles: got %s (%d/%d)", part.Name, i+1, len(m.Parts))
	}

	tmp := geoPmtilesTarget() + ".tmp"
	_ = os.Remove(tmp)
	out, err := os.Create(tmp)
	if err != nil {
		pmtilesSync.Err.Store(err.Error())
		return
	}
	h := sha256.New()
	w := io.MultiWriter(out, h)
	for _, part := range m.Parts {
		partPath := filepath.Join(partsDir, part.Name)
		f, err := os.Open(partPath)
		if err != nil {
			out.Close()
			_ = os.Remove(tmp)
			pmtilesSync.Err.Store(err.Error())
			return
		}
		_, copyErr := io.Copy(w, f)
		f.Close()
		if copyErr != nil {
			out.Close()
			_ = os.Remove(tmp)
			pmtilesSync.Err.Store(copyErr.Error())
			return
		}
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		pmtilesSync.Err.Store(err.Error())
		return
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if sum != m.SHA256 {
		_ = os.Remove(tmp)
		pmtilesSync.Err.Store(fmt.Sprintf("sha256 mismatch: got %s want %s", sum, m.SHA256))
		log.Printf("geo-pmtiles: %s", pmtilesSync.Err.Load())
		return
	}
	if err := os.Rename(tmp, geoPmtilesTarget()); err != nil {
		pmtilesSync.Err.Store(err.Error())
		return
	}
	_, _ = db.Exec(`INSERT INTO gafam_settings(key, value) VALUES('geo_pmtiles_version', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, m.Version)
	// Free shard copies — assembled file is enough (keep under soft geo quota).
	_ = os.RemoveAll(partsDir)
	pmtilesSync.Progress.Store(100)
	log.Printf("geo-pmtiles: synced %s (%d bytes)", geoPmtilesTarget(), m.Bytes)
	go enforceGeoQuota()
}

func downloadPmtilesPart(client *http.Client, url, dest string, part pmtilesManifestPart) error {
	tmp := dest + ".tmp"
	_ = os.Remove(tmp)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "gafam-vpc-pmtiles-sync/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), resp.Body)
	closeErr := out.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if n != part.Bytes {
		_ = os.Remove(tmp)
		return fmt.Errorf("size %d != %d", n, part.Bytes)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if sum != part.SHA256 {
		_ = os.Remove(tmp)
		return fmt.Errorf("sha256 mismatch")
	}
	return os.Rename(tmp, dest)
}

func initGeoPmtiles() {
	m, err := loadPmtilesManifest()
	if err != nil {
		log.Printf("geo-pmtiles: no manifest (%v)", err)
		p := geoPmtilesPath()
		if p == "" {
			log.Printf("geo-pmtiles: no basemap.pmtiles yet")
		} else {
			st, _ := os.Stat(p)
			log.Printf("geo-pmtiles: ready %s (%d bytes) [no manifest]", p, st.Size())
		}
		return
	}
	pmtilesSync.Version.Store(m.Version)
	if pmtilesAlreadyReady(m) {
		st, _ := os.Stat(geoPmtilesPath())
		log.Printf("geo-pmtiles: ready %s (%d bytes) version=%s", geoPmtilesPath(), st.Size(), m.Version)
		return
	}
	log.Printf("geo-pmtiles: missing/outdated — auto-sync from %s", m.BaseURL)
	if pmtilesSync.Syncing.CompareAndSwap(false, true) {
		go syncPmtilesFromRelease(m)
	}
}
