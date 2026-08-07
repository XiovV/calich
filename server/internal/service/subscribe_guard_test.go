package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsPublicAddr(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{"IPv4 loopback", "127.0.0.1", false},
		{"IPv6 loopback", "::1", false},
		{"RFC1918 10/8", "10.0.0.1", false},
		{"RFC1918 172.16/12", "172.16.5.4", false},
		{"RFC1918 192.168/16", "192.168.1.1", false},
		{"IPv4 link-local", "169.254.1.1", false},
		{"IPv6 link-local", "fe80::1", false},
		{"IPv6 unique-local (RFC4193)", "fc00::1", false},
		{"CGNAT (RFC6598) start", "100.64.0.1", false},
		{"CGNAT (RFC6598) end", "100.127.255.254", false},
		{"unspecified IPv4", "0.0.0.0", false},
		{"unspecified IPv6", "::", false},
		{"IPv4 multicast", "224.0.0.1", false},
		{"IPv6 multicast", "ff02::1", false},
		{"just below CGNAT range", "100.63.255.255", true},
		{"just above CGNAT range", "100.128.0.0", true},
		{"public IPv4", "93.184.216.34", true},
		{"public IPv6", "2606:4700:4700::1111", true},
		{"IPv4-mapped IPv6 loopback", "::ffff:127.0.0.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("net.ParseIP(%q) failed", tt.ip)
			}
			if got := isPublicAddr(ip); got != tt.want {
				t.Errorf("isPublicAddr(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

// TestGuardedDialContext_BlocksDisallowedAddressWithoutDialing proves the
// guard refuses to connect when every resolved address fails allow, and
// that the error is ErrSubscribeURLBlocked — the type classifyRefreshError
// and the API need to tell "blocked" apart from "unreachable" (#97).
func TestGuardedDialContext_BlocksDisallowedAddressWithoutDialing(t *testing.T) {
	dial := newGuardedDialContext(
		func(ctx context.Context, host string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		},
		func(ip net.IP) bool { return false },
	)

	_, err := dial(context.Background(), "tcp", "internal.example:80")
	if !errors.Is(err, ErrSubscribeURLBlocked) {
		t.Fatalf("expected ErrSubscribeURLBlocked, got %v", err)
	}
}

// TestGuardedDialContext_DialsTheCheckedAddress proves an allowed address is
// actually connected to — and specifically the address that was checked,
// not a fresh re-resolution of the hostname (which would reopen the DNS-
// rebinding gap the guard exists to close).
func TestGuardedDialContext_DialsTheCheckedAddress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			conn.Close()
		}
	}()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}

	dial := newGuardedDialContext(
		func(ctx context.Context, host string) ([]net.IP, error) {
			if host != "public.example" {
				t.Fatalf("expected lookup for public.example, got %q", host)
			}
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		},
		func(ip net.IP) bool { return true },
	)

	conn, err := dial(context.Background(), "tcp", "public.example:"+port)
	if err != nil {
		t.Fatalf("expected the allowed address to be dialed, got %v", err)
	}
	conn.Close()
}

// TestGuardedDialContext_RecheckedOnEveryRedirect is the acceptance
// criterion that a public URL can't redirect its way to an internal host
// (#97): the initial host resolves to an address allow accepts, the
// redirect target resolves to one it refuses, and the overall request must
// fail as ErrSubscribeURLBlocked without the redirect target ever being
// reached. The two servers are real, separately-listening loopback
// addresses — 127.0.0.1 and ::1, the two loopback addresses every system
// binds by default, no alias setup required — so a faked lookup can tell
// them apart by hostname while allow draws the public/private line between
// their two addresses, keeping this test hermetic (no real DNS, no real
// Internet) while still exercising the real dial mechanism end to end,
// redirect included.
func TestGuardedDialContext_RecheckedOnEveryRedirect(t *testing.T) {
	allowedIP := net.ParseIP("127.0.0.1")
	blockedIP := net.ParseIP("::1")

	blockedListener, err := net.Listen("tcp", "["+blockedIP.String()+"]:0")
	if err != nil {
		t.Fatalf("listen on %s: %v", blockedIP, err)
	}
	blockedServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the redirect target must never be reached once the guard blocks it")
	}))
	blockedServer.Listener = blockedListener
	blockedServer.Start()
	defer blockedServer.Close()

	_, blockedPort, err := net.SplitHostPort(blockedListener.Addr().String())
	if err != nil {
		t.Fatalf("split blocked listener addr: %v", err)
	}

	allowedListener, err := net.Listen("tcp", allowedIP.String()+":0")
	if err != nil {
		t.Fatalf("listen on %s: %v", allowedIP, err)
	}
	allowedServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://internal.example:"+blockedPort+"/actual.ics", http.StatusFound)
	}))
	allowedServer.Listener = allowedListener
	allowedServer.Start()
	defer allowedServer.Close()

	lookup := func(ctx context.Context, host string) ([]net.IP, error) {
		switch host {
		case "public.example":
			return []net.IP{allowedIP}, nil
		case "internal.example":
			return []net.IP{blockedIP}, nil
		default:
			t.Fatalf("unexpected lookup host %q", host)
			return nil, nil
		}
	}
	allow := func(ip net.IP) bool { return ip.Equal(allowedIP) }

	client := &http.Client{
		Transport: &http.Transport{DialContext: newGuardedDialContext(lookup, allow)},
	}

	requestURL := "http://public.example" + allowedServer.URL[len("http://"+allowedIP.String()):] + "/redirect.ics"
	_, err = client.Get(requestURL)
	if !errors.Is(err, ErrSubscribeURLBlocked) {
		t.Fatalf("expected ErrSubscribeURLBlocked from the redirect hop, got %v", err)
	}
}
