package karaka

// SSRF guard for agent-reachable fetch/navigation tools.
// An LLM planner (or a poisoned web page it read) could otherwise aim the
// node at cloud metadata (169.254.169.254), the docker network, or loopback.
// Every agent-facing "go fetch this URL" entry point must call GuardPublicURL
// first. Defense in depth: scheme allowlist + hostname blocklist + DNS
// resolution checked against private/loopback/link-local ranges.

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

var blockedHostnames = map[string]bool{
	"localhost":            true,
	"metadata":             true,
	"metadata.google.internal": true,
	"host.docker.internal": true,
}

var blockedPrefixes = []string{
	"gafam-",   // our own sidecars (browser, sandbox, qwen…)
	"watchtower",
}

var blockedCIDRs = []string{
	"127.0.0.0/8",     // loopback
	"10.0.0.0/8",      // private
	"172.16.0.0/12",   // private
	"192.168.0.0/16",  // private
	"169.254.0.0/16",  // link-local (cloud metadata lives here)
	"100.64.0.0/10",   // CGNAT
	"0.0.0.0/8",
	"::1/128",         // v6 loopback
	"fc00::/7",        // v6 unique local
	"fe80::/10",       // v6 link-local
}

var parsedBlockedCIDRs []*net.IPNet

func init() {
	for _, c := range blockedCIDRs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			parsedBlockedCIDRs = append(parsedBlockedCIDRs, n)
		}
	}
}

func ipBlocked(ip net.IP) bool {
	for _, n := range parsedBlockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// AllowPrivateURLs disables the guard — tests only (local httptest servers).
var AllowPrivateURLs = false

// GuardPublicURL returns nil if the URL is safe for an agent to fetch, an
// error otherwise. It resolves the hostname and rejects the URL if ANY
// resolved IP is in a blocked range.
func GuardPublicURL(rawURL string) error {
	if AllowPrivateURLs {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed (http/https only)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing host")
	}
	h := strings.ToLower(host)
	if blockedHostnames[h] {
		return fmt.Errorf("host %q is blocked", host)
	}
	for _, p := range blockedPrefixes {
		if strings.HasPrefix(h, p) {
			return fmt.Errorf("host %q is blocked (internal)", host)
		}
	}
	// Literal IP?
	if ip := net.ParseIP(h); ip != nil {
		if ipBlocked(ip) {
			return fmt.Errorf("address %s is in a blocked range", h)
		}
		return nil
	}
	// DNS resolution — reject if any answer is internal.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", h)
	if err != nil {
		return fmt.Errorf("DNS resolution failed for %q: %w", host, err)
	}
	for _, ip := range ips {
		if ipBlocked(ip) {
			return fmt.Errorf("host %q resolves to a blocked address (%s)", host, ip)
		}
	}
	return nil
}
