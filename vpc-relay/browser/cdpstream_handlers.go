package browser

// HTTP handlers for the CDP view plane. They speak the same wire contract the
// frontend already expects (length-prefixed JPEG stream for /stream, JSON
// events for /input), so BrowserView is unchanged except for a small resize
// robustness fix. When the CDP plane is not enabled or not reachable, the
// legacy handlers in proxy.go / khadyota.go proxy to the container pipeline.

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
)

// cdpStreamHandler streams Page.captureScreenshot JPEGs to the human view.
// Returns a non-nil error only if nothing was written yet (so the caller can
// fall back to the container pipeline).
func cdpStreamHandler(w http.ResponseWriter, r *http.Request) error {
	TouchActivity()
	stopAlive := keepBrowserAlive(r)
	defer stopAlive()

	if err := ensureCapture(); err != nil {
		return err
	}

	ch := make(chan cdpFrame, 2)
	captureMu.Lock()
	if !captureActive {
		captureMu.Unlock()
		return errCaptureStopped
	}
	captureSubs[ch] = struct{}{}
	captureMu.Unlock()
	defer func() {
		captureMu.Lock()
		if _, ok := captureSubs[ch]; ok {
			delete(captureSubs, ch)
		}
		captureMu.Unlock()
	}()

	// Deliver one frame up-front when available so a freshly woken browser
	// paints quickly; dimensions come from the current viewport regardless.
	vw, vh := viewportSize()
	hdr := w.Header()
	hdr.Set("Content-Type", "application/octet-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("X-Browser-Width", strconv.Itoa(vw))
	hdr.Set("X-Browser-Height", strconv.Itoa(vh))
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	var prefix [4]byte
	for frame := range ch {
		// Frames captured while a resize is being applied carry the OLD size;
		// drop them so the canvas is never painted with mismatched geometry.
		cw, chh := viewportSize()
		if frame.w != cw || frame.h != chh {
			continue
		}
		binary.BigEndian.PutUint32(prefix[:], uint32(len(frame.jpeg)))
		if _, err := w.Write(prefix[:]); err != nil {
			return nil // client went away; nothing more to do
		}
		if _, err := w.Write(frame.jpeg); err != nil {
			return nil
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	return nil
}

var errCaptureStopped = &cdpViewError{"capture not running"}

type cdpViewError struct{ msg string }

func (e *cdpViewError) Error() string { return e.msg }

// cdpInputHandler dispatches one frontend input event into the page viewport.
func cdpInputHandler(w http.ResponseWriter, r *http.Request) error {
	TouchHuman()

	var event map[string]interface{}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if len(body) > 0 {
		if err := json.Unmarshal(body, &event); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return nil
		}
	}
	if event == nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing event body"})
		return nil
	}
	if err := cdpHandleInput(event); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"ok": "false", "error": err.Error()})
		return nil
	}
	sendJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	return nil
}

// cdpResizeHandler sets the emulated viewport to the dashboard frame size.
func cdpResizeHandler(w http.ResponseWriter, r *http.Request) error {
	var payload struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&payload); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"ok": "false", "error": "invalid JSON: " + err.Error()})
		return nil
	}
	if payload.Width < 1 || payload.Height < 1 {
		sendJSON(w, http.StatusBadRequest, map[string]string{"ok": "false", "error": "missing width/height"})
		return nil
	}
	setViewport(payload.Width, payload.Height)
	// Ensure a capture loop is running so the new viewport is applied now;
	// the dashboard reconnects right after this anyway.
	_ = ensureCapture()
	TouchActivity()
	vw, vh := viewportSize()
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "width": vw, "height": vh,
	})
	return nil
}

// cdpNavigateHandler navigates the pinned page target.
func cdpNavigateHandler(w http.ResponseWriter, r *http.Request) error {
	TouchHuman()
	var payload struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&payload); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"ok": "false", "error": "invalid JSON: " + err.Error()})
		return nil
	}
	url := payload.URL
	if url == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"ok": "false", "error": "missing 'url'"})
		return nil
	}
	if !isWebishURL(url) {
		sendJSON(w, http.StatusBadRequest, map[string]string{"ok": "false", "error": "url must start with http:// or https://"})
		return nil
	}
	if err := cdpNavigate(url); err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"ok": "false", "error": err.Error()})
		return nil
	}
	TouchActivity()
	sendJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "url": url})
	return nil
}

func isWebishURL(u string) bool {
	if len(u) >= 7 && (u[:7] == "http://" || u[:8] == "https://") {
		return true
	}
	// Allow scheme-less input by normalising to https.
	return false
}

// cdpWindowHandler reports the pinned page's title/url and current viewport.
func cdpWindowHandler(w http.ResponseWriter, r *http.Request) error {
	info, err := cdpWindowInfo()
	if err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return nil
	}
	sendJSON(w, http.StatusOK, info)
	return nil
}

// cdpScreenshotHandler serves a single JPEG of the current viewport.
func cdpScreenshotHandler(w http.ResponseWriter, r *http.Request) error {
	jpeg, err := cdpSingleScreenshot()
	if err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return nil
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(jpeg)))
	_, _ = w.Write(jpeg)
	return nil
}

// ─── dispatch helpers ──────────────────────────────────────────────────────
// The legacy handlers live in proxy.go / khadyota.go and proxy to the browser
// container's stream server. These helpers branch to the CDP plane when it is
// enabled, logging a fallback so a misbehaving CDP path is visible in the API
// logs without breaking the human view.

func cdpServeOrFallback(fn func(http.ResponseWriter, *http.Request) error, fallback func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cdpViewEnabled() {
			fallback(w, r)
			return
		}
		if err := fn(w, r); err != nil {
			log.Printf("vatayana: cdp handler %s %s failed (%v) — falling back to container pipeline", r.Method, r.URL.Path, err)
			fallback(w, r)
		}
	}
}
