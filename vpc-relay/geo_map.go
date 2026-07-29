package main

// Map pack: HD basemap + vector overlays under /app/data/geo (≈2 GiB soft quota).
// Seeded from image /app/geo-data/map-pack/ on boot. Remaining quota = OSM tile cache.

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	geoQuotaBytes     = 2_000_000_000 // 2 GiB soft budget for /app/data/geo
	geoMapPackVersion = "map-pack-v2"
)

func geoMapDir() string {
	return filepath.Join(geoDir(), "map")
}

func geoMapPackSource() string {
	candidates := []string{
		"/app/geo-data/map-pack",
		filepath.Join(dataDir(), "geo-data", "map-pack"),
		"geo-data/map-pack",
		filepath.Join("vpc-relay", "geo-data", "map-pack"),
	}
	for _, p := range candidates {
		if st, err := os.Stat(filepath.Join(p, "manifest.json")); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func initGeoMapPack() {
	_ = os.MkdirAll(geoMapDir(), 0o755)
	src := geoMapPackSource()
	if src == "" {
		log.Println("geo-map: no map-pack in image — basemap overlays unavailable until pack is present")
		return
	}
	var ver string
	_ = db.QueryRow(`SELECT value FROM gafam_settings WHERE key = 'geo_map_pack_version'`).Scan(&ver)
	need := ver != geoMapPackVersion
	if !need {
		// still ensure files exist on volume
		if _, err := os.Stat(filepath.Join(geoMapDir(), "basemap.jpg")); err != nil {
			need = true
		}
	}
	if !need {
		log.Printf("geo-map: ready (%s) quota=%dB used=%dB", geoMapPackVersion, geoQuotaBytes, geoDirBytes())
		return
	}
	log.Printf("geo-map: seeding map-pack from %s → %s", src, geoMapDir())
	if err := copyDirFiles(src, geoMapDir()); err != nil {
		log.Printf("geo-map: seed failed: %v", err)
		return
	}
	_, _ = db.Exec(`INSERT INTO gafam_settings(key, value) VALUES('geo_map_pack_version', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, geoMapPackVersion)
	log.Printf("geo-map: seeded %s (used=%dB / %dB)", geoMapPackVersion, geoDirBytes(), geoQuotaBytes)
	go enforceGeoQuota()
}

func copyDirFiles(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		// skip junk
		base := filepath.Base(path)
		if base == ".gitignore" || base == "README.md" {
			return nil
		}
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return copyFile(path, out)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, dst)
}

func geoDirBytes() int64 {
	var total int64
	_ = filepath.Walk(geoDir(), func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// enforceGeoQuota deletes oldest cached OSM tiles until under soft quota.
func enforceGeoQuota() {
	used := geoDirBytes()
	if used <= geoQuotaBytes {
		return
	}
	type tile struct {
		path string
		mod  time.Time
		size int64
	}
	var tiles []tile
	_ = filepath.Walk(geoTilesDir(), func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() && strings.HasSuffix(path, ".png") {
			tiles = append(tiles, tile{path: path, mod: info.ModTime(), size: info.Size()})
		}
		return nil
	})
	// oldest first
	for i := 0; i < len(tiles); i++ {
		for j := i + 1; j < len(tiles); j++ {
			if tiles[j].mod.Before(tiles[i].mod) {
				tiles[i], tiles[j] = tiles[j], tiles[i]
			}
		}
	}
	for _, t := range tiles {
		if used <= geoQuotaBytes {
			break
		}
		if err := os.Remove(t.path); err == nil {
			used -= t.size
		}
	}
	log.Printf("geo-map: quota enforce → used=%dB / %dB", used, geoQuotaBytes)
}

func geoMapFile(name string) string {
	// allow streets/<city>.geojson.gz
	clean := filepath.Clean("/" + strings.TrimPrefix(name, "/"))
	clean = strings.TrimPrefix(clean, "/")
	if strings.Contains(clean, "..") {
		return ""
	}
	base := filepath.Base(clean)
	dir := filepath.Dir(clean)
	switch {
	case dir == "." && (base == "basemap.jpg" || base == "manifest.json" ||
		base == "rivers.geojson.gz" || base == "roads.geojson.gz" || base == "cities.geojson.gz"):
		return filepath.Join(geoMapDir(), base)
	case dir == "streets" && (base == "index.json" || strings.HasSuffix(base, ".geojson.gz")):
		return filepath.Join(geoMapDir(), "streets", base)
	default:
		return ""
	}
}

func geoBasemapHandler(w http.ResponseWriter, r *http.Request) {
	path := geoMapFile("basemap.jpg")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	serveGeoFile(w, r, path, "image/jpeg")
}

func geoStreetsIndex() map[string]interface{} {
	p := geoMapFile("streets/index.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return map[string]interface{}{"cities": []interface{}{}}
	}
	var idx map[string]interface{}
	if json.Unmarshal(b, &idx) != nil {
		return map[string]interface{}{"cities": []interface{}{}}
	}
	return idx
}

func geoLayersListHandler(w http.ResponseWriter, r *http.Request) {
	manifestPath := geoMapFile("manifest.json")
	layers := []map[string]interface{}{}
	for _, id := range []string{"rivers", "roads", "cities"} {
		p := geoMapFile(id + ".geojson.gz")
		st, err := os.Stat(p)
		ok := err == nil && st.Size() > 0
		entry := map[string]interface{}{"id": id, "available": ok}
		if ok {
			entry["bytes"] = st.Size()
			entry["encoding"] = "gzip"
		}
		layers = append(layers, entry)
	}
	bm, err := os.Stat(geoMapFile("basemap.jpg"))
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"version":     geoMapPackVersion,
		"quota_bytes": geoQuotaBytes,
		"used_bytes":  geoDirBytes(),
		"basemap":     err == nil,
		"basemap_bytes": func() int64 {
			if err == nil {
				return bm.Size()
			}
			return 0
		}(),
		"layers":        layers,
		"streets":       geoStreetsIndex(),
		"manifest_path": manifestPath,
		"tiles_cached":  countCachedTiles(),
	})
}

func geoLayerHandler(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(strings.ToLower(r.PathValue("name")))
	name = strings.TrimSuffix(name, ".geojson")
	name = strings.TrimSuffix(name, ".gz")
	var file string
	switch name {
	case "rivers", "roads", "cities":
		file = geoMapFile(name + ".geojson.gz")
	default:
		http.Error(w, "unknown layer", http.StatusNotFound)
		return
	}
	serveGzipGeoJSON(w, file, name)
}

func geoStreetsHandler(w http.ResponseWriter, r *http.Request) {
	city := strings.TrimSpace(strings.ToLower(r.PathValue("city")))
	city = strings.TrimSuffix(city, ".geojson")
	city = strings.TrimSuffix(city, ".gz")
	if city == "" || city == "index" {
		sendJSON(w, http.StatusOK, geoStreetsIndex())
		return
	}
	// sanitize city id
	for _, c := range city {
		if !((c >= 'a' && c <= 'z') || c == '-' || c == '_') {
			http.Error(w, "bad city", http.StatusBadRequest)
			return
		}
	}
	file := geoMapFile("streets/" + city + ".geojson.gz")
	serveGzipGeoJSON(w, file, "streets/"+city)
}

func serveGzipGeoJSON(w http.ResponseWriter, file, label string) {
	if file == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	f, err := os.Open(file)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		http.Error(w, "layer corrupt", http.StatusInternalServerError)
		return
	}
	defer gz.Close()
	w.Header().Set("Content-Type", "application/geo+json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Geo-Layer", label)
	_, _ = io.Copy(w, gz)
}

func serveGeoFile(w http.ResponseWriter, r *http.Request, path, contentType string) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, filepath.Base(path), st.ModTime(), f)
}
