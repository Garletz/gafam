package browser

import (
	"net/http"
	"net/url"
)

// Agent-facing handlers (Khadyota spirit): let a kāraka read the web as
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
// Drives the visible Firefox window to the given URL (xdotool).
func NavigateHandler(w http.ResponseWriter, r *http.Request) {
	running, err := containerState()
	if err != nil || !running {
		http.Error(w, `{"error":"browser_not_running"}`, http.StatusServiceUnavailable)
		return
	}
	r.URL.Path = "/navigate"
	r.URL.RawQuery = ""
	getProxy().ServeHTTP(w, r)
}

// WindowHandler — GET /api/web/browser/window
// Returns the current window title — "what am I looking at".
func WindowHandler(w http.ResponseWriter, r *http.Request) {
	running, err := containerState()
	if err != nil || !running {
		http.Error(w, `{"error":"browser_not_running"}`, http.StatusServiceUnavailable)
		return
	}
	r.URL.Path = "/window"
	r.URL.RawQuery = ""
	getProxy().ServeHTTP(w, r)
}
