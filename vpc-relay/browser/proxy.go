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

		originalDirector := rp.Director
		rp.Director = func(req *http.Request) {
			originalDirector(req)
			req.Host = target.Host
			req.URL.Path = strings.TrimPrefix(req.URL.Path, "/browser")
			if req.URL.Path == "" {
				req.URL.Path = "/"
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
