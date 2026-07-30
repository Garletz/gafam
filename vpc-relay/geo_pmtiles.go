package main

// Serve a local Protomaps PMTiles archive with HTTP Range (no CDN).
// File lives at {dataDir}/geo/basemap.pmtiles (seeded / downloaded once onto the VPC volume).

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func geoPmtilesPath() string {
	candidates := []string{
		filepath.Join(geoDir(), "basemap.pmtiles"),
		filepath.Join(geoMapDir(), "basemap.pmtiles"),
		"/app/geo-data/basemap.pmtiles",
		"geo-data/basemap.pmtiles",
		filepath.Join("vpc-relay", "geo-data", "basemap.pmtiles"),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Size() > 0 {
			return p
		}
	}
	return ""
}

func geoPmtilesStatus() map[string]interface{} {
	p := geoPmtilesPath()
	out := map[string]interface{}{
		"pmtiles":     false,
		"path":        p,
		"quota_bytes": geoQuotaBytes,
		"used_bytes":  geoDirBytes(),
		"format":      "pmtiles",
		"stack":       "maplibre+protomaps",
		"cdn":         false,
	}
	if p != "" {
		if st, err := os.Stat(p); err == nil {
			out["pmtiles"] = true
			out["bytes"] = st.Size()
			out["mtime"] = st.ModTime().UTC().Format("2006-01-02T15:04:05Z")
		}
	}
	return mergePmtilesStatus(out)
}

// GET /api/web/geo/pmtiles — full file or Range bytes (for pmtiles.js Protocol).
func geoPmtilesHandler(w http.ResponseWriter, r *http.Request) {
	path := geoPmtilesPath()
	if path == "" {
		sendJSON(w, http.StatusNotFound, map[string]string{
			"error": "basemap.pmtiles missing — Settings → Sync basemap (GitHub Release shards)",
		})
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "cannot open pmtiles", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, "stat failed", http.StatusInternalServerError)
		return
	}
	size := st.Size()

	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", "application/vnd.pmtiles")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Geo-Pmtiles", "1")

	rangeHdr := r.Header.Get("Range")
	if rangeHdr == "" {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, f)
		return
	}

	// Parse simple bytes=START-END
	start, end, ok := parseBytesRange(rangeHdr, size)
	if !ok {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	length := end - start + 1
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		http.Error(w, "seek failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(http.StatusPartialContent)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.CopyN(w, f, length)
}

func parseBytesRange(h string, size int64) (start, end int64, ok bool) {
	h = strings.TrimSpace(h)
	if !strings.HasPrefix(h, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(h, "bytes=")
	// only first range
	if i := strings.IndexByte(spec, ','); i >= 0 {
		spec = spec[:i]
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	if parts[0] == "" {
		// suffix: bytes=-N
		n, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true
	}
	s, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || s < 0 {
		return 0, 0, false
	}
	var e int64
	if parts[1] == "" {
		e = size - 1
	} else {
		e, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || e < s {
			return 0, 0, false
		}
	}
	if s >= size {
		return 0, 0, false
	}
	if e >= size {
		e = size - 1
	}
	return s, e, true
}
