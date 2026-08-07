// subscribe_guard.go blocks Subscription URLs that resolve to a private,
// loopback, link-local, or otherwise non-public address (#97, ADR-0032): a
// single-user instance has no privilege boundary for SSRF to cross, but the
// day a second User can add a Subscription, an unguarded fetch becomes a
// window into the operator's network. The guard runs at dial time, inside
// http.Transport.DialContext, rather than by inspecting the URL's hostname
// once up front, for two reasons: it sees the address actually being
// connected to (not a hostname a DNS answer could point anywhere), and
// http.Client opens a fresh connection — so calls DialContext again — for
// every redirect hop, which is the only way to satisfy "re-checked after
// every redirect" without hand-rolling redirect handling.
package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
)

// subscribeHTTPClient is the default client fetchICS/fetchICSConditional use
// in production: its Transport dials through a guarded DialContext built by
// newGuardedDialContext, so every connection it opens — the initial request
// and every redirect hop — is checked. Safe for concurrent use, so it's
// shared across every SubscribeService that doesn't override it via
// WithHTTPClient.
var subscribeHTTPClient = &http.Client{
	Transport: &http.Transport{
		DialContext: newGuardedDialContext(lookupIPs, isPublicAddr),
	},
}

// lookupIPs adapts net.DefaultResolver.LookupIPAddr's []net.IPAddr to the
// []net.IP newGuardedDialContext's lookup parameter takes.
func lookupIPs(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, len(addrs))
	for i, a := range addrs {
		ips[i] = a.IP
	}
	return ips, nil
}

// newGuardedDialContext builds an http.Transport.DialContext that resolves
// addr's host via lookup, refuses to connect if none of the resolved
// addresses satisfy allow, and otherwise dials the first one that does,
// directly by IP — never by re-resolving the hostname a second time — so
// nothing can swap in an unchecked address between the check and the
// connect (DNS rebinding). lookup and allow are parameters rather than
// hardcoded so tests can exercise the redirect-recheck mechanism itself
// (does DialContext really run again per hop, does the checked IP survive
// to the actual connect) without depending on real DNS or the public
// Internet; production wires them to lookupIPs and isPublicAddr.
func newGuardedDialContext(lookup func(ctx context.Context, host string) ([]net.IP, error), allow func(net.IP) bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		ips, err := lookup(ctx, host)
		if err != nil {
			return nil, err
		}

		var dialer net.Dialer
		var lastErr error
		for _, ip := range ips {
			if !allow(ip) {
				lastErr = fmt.Errorf("%w: %s resolves to %s", ErrSubscribeURLBlocked, host, ip)
				continue
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("no addresses found for %s", host)
		}
		return nil, lastErr
	}
}

// cgnatBlock is RFC 6598's Shared Address Space (100.64.0.0/10), carrier-
// grade NAT's internal range — not covered by net.IP's own IsPrivate, which
// only knows RFC 1918/RFC 4193.
var cgnatBlock = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// isPublicAddr reports whether ip is safe for this instance to connect to on
// a User's behalf: not loopback, not private (RFC 1918 / RFC 4193), not
// link-local (unicast or multicast), not a multicast or unspecified
// address, and not carrier-grade NAT space. Everything else — the public
// Internet — is allowed, including addresses a household LAN feed might
// deliberately want (ADR-0032 named that as the cost of blocking).
func isPublicAddr(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	switch {
	case ip.IsLoopback(),
		ip.IsPrivate(),
		ip.IsLinkLocalUnicast(),
		ip.IsLinkLocalMulticast(),
		ip.IsInterfaceLocalMulticast(),
		ip.IsMulticast(),
		ip.IsUnspecified(),
		cgnatBlock.Contains(ip):
		return false
	}
	return true
}
