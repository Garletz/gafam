package browser

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	proxyOnce sync.Once
	rp        *httputil.ReverseProxy
)

func getProxy() *httputil.ReverseProxy {
	proxyOnce.Do(func() {
		target, _ := url.Parse(browserBaseURL())
		rp = httputil.NewSingleHostReverseProxy(target)
		// Flush immediately so JPEG stream frames reach the client without buffering.
		rp.FlushInterval = -1

		originalDirector := rp.Director
		rp.Director = func(req *http.Request) {
			originalDirector(req)
			req.Host = target.Host
			path := req.URL.Path
			switch {
			case strings.HasSuffix(path, "/stream") || path == "/stream":
				req.URL.Path = "/stream"
				req.URL.RawQuery = ""
			case strings.HasSuffix(path, "/input") || path == "/input":
				req.URL.Path = "/input"
				req.URL.RawQuery = ""
			case strings.HasSuffix(path, "/status") || path == "/status":
				req.URL.Path = "/status"
				req.URL.RawQuery = ""
			case strings.HasSuffix(path, "/screenshot") || path == "/screenshot":
				req.URL.Path = "/screenshot"
				req.URL.RawQuery = ""
			case strings.HasSuffix(path, "/resize") || path == "/resize":
				req.URL.Path = "/resize"
				req.URL.RawQuery = ""
			default:
				req.URL.Path = strings.TrimPrefix(path, "/browser")
				if req.URL.Path == "" {
					req.URL.Path = "/"
				}
			}
		}
	})
	return rp
}

func ProxyHandler(w http.ResponseWriter, r *http.Request) {
	running, err := containerState()
	if err != nil || !running {
		http.Error(w, `{"error":"browser_not_running"}`, http.StatusServiceUnavailable)
		return
	}
	getProxy().ServeHTTP(w, r)
}

func browserRunningCheck(w http.ResponseWriter, r *http.Request) bool {
	running, err := containerState()
	if err != nil || !running {
		http.Error(w, `{"error":"browser_not_running"}`, http.StatusServiceUnavailable)
		return false
	}
	return true
}

// keepBrowserAlive re-touches the idle-reaper activity stamp for as long as a
// stream is open, so a long passive viewing session (no clicks/keys) is never
// killed mid-watch. The returned stop func must be called when the request ends.
func keepBrowserAlive(r *http.Request) func() {
	done := make(chan struct{})
	ctx := r.Context()
	go func() {
		t := time.NewTicker(25 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				TouchActivity()
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() { close(done) }
}

// StreamHandler serves the human-facing JPEG stream. It prefers the CDP view
// plane (viewport capture, drift-free) and falls back to the container's
// ffmpeg/xdotool pipeline when CDP is unreachable or the stream cannot start.
func StreamHandler(w http.ResponseWriter, r *http.Request) {
	if !browserRunningCheck(w, r) {
		return
	}
	if cdpViewEnabled() {
		// cdpStreamHandler returns an error only when nothing was written yet,
		// so a clean fallback to the legacy path is always possible.
		if err := cdpStreamHandler(w, r); err == nil {
			return
		} else {
			log.Printf("vatayana: cdp stream unavailable (%v) — using container pipeline", err)
		}
	}
	// Legacy path: an open stream keeps the browser alive against the reaper.
	stopAlive := keepBrowserAlive(r)
	defer stopAlive()
	r.URL.Path = "/stream"
	getProxy().ServeHTTP(w, r)
}

// InputHandler dispatches one remote-control input event. CDP viewport events
// land on the same coordinate space as the CDP stream; when the CDP plane is
// off, events go to the container's xdotool endpoint instead.
func InputHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if !browserRunningCheck(w, r) {
		return
	}
	if cdpViewEnabled() {
		if err := cdpInputHandler(w, r); err == nil {
			return
		} else {
			log.Printf("vatayana: cdp input unavailable (%v) — using container pipeline", err)
		}
	}
	// Legacy path: the human is driving — agent browser tools briefly yield.
	TouchHuman()
	r.URL.Path = "/input"
	getProxy().ServeHTTP(w, r)
}
