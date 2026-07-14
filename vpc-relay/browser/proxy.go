package browser

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
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

func StreamHandler(w http.ResponseWriter, r *http.Request) {
	running, err := containerState()
	if err != nil || !running {
		http.Error(w, `{"error":"browser_not_running"}`, http.StatusServiceUnavailable)
		return
	}
	r.URL.Path = "/stream"
	getProxy().ServeHTTP(w, r)
}

func InputHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	running, err := containerState()
	if err != nil || !running {
		http.Error(w, `{"error":"browser_not_running"}`, http.StatusServiceUnavailable)
		return
	}
	r.URL.Path = "/input"
	getProxy().ServeHTTP(w, r)
}
