package sandbox

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
		target, _ := url.Parse(sandboxHTTPURL())
		rp = httputil.NewSingleHostReverseProxy(target)

		originalDirector := rp.Director
		rp.Director = func(req *http.Request) {
			originalDirector(req)
			req.Host = target.Host
			path := req.URL.Path
			path = strings.TrimPrefix(path, "/api/web/sandbox")
			if strings.HasPrefix(path, "-") {
				path = "/" + path[1:]
			}
			if path == "" {
				path = "/"
			}
			req.URL.Path = path
		}
	})
	return rp
}
