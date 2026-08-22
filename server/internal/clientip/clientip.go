// Package clientip extracts the caller's address for AuthRateLimiter's
// per-IP bucket (#240, ADR-0070).
package clientip

import (
	"net"
	"net/http"
)

// From returns r's client address, stripped of its port. It reads only
// RemoteAddr — never X-Forwarded-For or X-Real-IP, both attacker-controlled
// on any request that reaches this server directly, and trusting either
// without a "which proxies are mine" allowlist would let an attacker set a
// fresh value per request and walk straight through the per-IP ceiling
// (ADR-0070). The documented consequence: behind a reverse proxy that
// doesn't preserve the original client address some other way, every
// request's IP collapses to the proxy's own, coarsening the per-IP bucket
// to bound the proxy's total traffic rather than any individual client
// behind it.
func From(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
