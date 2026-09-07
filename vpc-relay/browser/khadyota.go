package browser

import (
	"log"
	"net/http"
	"net/url"
)

// Kāraka-facing handlers (Khadyota spirit): let a kāraka read the web as
// markdown-ish text and drive the visible Firefox without parsing pixels.

// FetchHandler — GET /api/web/browser/fetch?url=https://...
// Proxies to the container /fetch endpoint which returns
// { title, text, links, final_url, status } for any page.
func FetchHandler(w http.ResponseWriter, r *http.Request) {
	running, err := containerState()
	if err != nil || !running {
		http.Error(w, `{"error":"browser_not_running"}`, http.StatusServiceUnavailable)
		return
	}
	target := r.URL.Query().Get("url")
	if target == "" {
		http.Error(w, `{"error":"missing 'url' query param"}`, http.StatusBadRequest)
		return
	}
	r.URL.Path = "/fetch"
	r.URL.RawQuery = "url=" + url.QueryEscape(target)
	getProxy().ServeHTTP(w, r)
}

// NavigateHandler — POST /api/web/browser/navigate  { "url": "https://..." }
// Drives the shared Chrome to the given URL. Prefers a CDP Page.navigate
// (instant, no address-bar typing); falls back to the container's xdotool
// navigation on the legacy pipeline.
func NavigateHandler(w http.ResponseWriter, r *http.Request) {
	if !browserRunningCheck(w, r) {
		return
	}
	if cdpViewEnabled() {
		if err := cdpNavigateHandler(w, r); err == nil {
			return
		} else {
			log.Printf("vatayana: cdp navigate unavailable (%v) — using container pipeline", err)
		}
	}
	r.URL.Path = "/navigate"
	r.URL.RawQuery = ""
	getProxy().ServeHTTP(w, r)
}

// WindowHandler — GET /api/web/browser/window
// Returns the current page title/url — "what am I looking at".
func WindowHandler(w http.ResponseWriter, r *http.Request) {
	if !browserRunningCheck(w, r) {
		return
	}
	if cdpViewEnabled() {
		if err := cdpWindowHandler(w, r); err == nil {
			return
		} else {
			log.Printf("vatayana: cdp window unavailable (%v) — using container pipeline", err)
		}
	}
	r.URL.Path = "/window"
	r.URL.RawQuery = ""
	getProxy().ServeHTTP(w, r)
}

// ResizeHandler — POST /api/web/browser/resize  { "width": 1400, "height": 800 }
// Sets the remote viewport to the dashboard frame size. On the CDP plane this
// is an Emulation.setDeviceMetricsOverride (no window wrangling, no drift); on
// the legacy plane it sizes the shared Chrome window.
func ResizeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !browserRunningCheck(w, r) {
		return
	}
	if cdpViewEnabled() {
		if err := cdpResizeHandler(w, r); err == nil {
			return
		} else {
			log.Printf("vatayana: cdp resize unavailable (%v) — using container pipeline", err)
		}
	}
	r.URL.Path = "/resize"
	r.URL.RawQuery = ""
	getProxy().ServeHTTP(w, r)
}
