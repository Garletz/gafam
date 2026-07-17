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

// stripTokenQuery removes the relay session token from the query string before
// forwarding to the sandbox container, keeping the other params (path, depth...).
func stripTokenQuery(raw string) string {
	if raw == "" {
		return ""
	}
	q, err := url.ParseQuery(raw)
	if err != nil {
		return ""
	}
	q.Del("token")
	return q.Encode()
}

func getProxy() *httputil.ReverseProxy {
	proxyOnce.Do(func() {
		target, _ := url.Parse(sandboxHTTPURL())
		rp = httputil.NewSingleHostReverseProxy(target)

		originalDirector := rp.Director
		rp.Director = func(req *http.Request) {
			originalDirector(req)
			req.Host = target.Host
			path := req.URL.Path
			switch {
			case path == "/api/web/sandbox-exec" || path == "/api/web/sandbox/exec":
				// Legacy one-shot exec.
				req.URL.Path = "/exec"
				req.URL.RawQuery = ""
			case path == "/api/web/sandbox/tree":
				// Filesystem tree (JSON or ASCII — agent "vision" of the fs).
				req.URL.Path = "/tree"
				req.URL.RawQuery = stripTokenQuery(req.URL.RawQuery)
			case strings.HasPrefix(path, "/api/web/sandbox/shell/"):
				// Persistent shell sessions (web terminal + Kāraka sandbox.shell).
				req.URL.Path = strings.TrimPrefix(path, "/api/web/sandbox")
				req.URL.RawQuery = ""
			default:
				path = strings.TrimPrefix(path, "/api/web/sandbox")
				if strings.HasPrefix(path, "-") {
					path = "/" + path[1:]
				}
				if path == "" {
					path = "/"
				}
				req.URL.Path = path
				// Sandbox HTTP server matches exact paths — never forward ?token=
				req.URL.RawQuery = ""
			}
		}
	})
	return rp
}
