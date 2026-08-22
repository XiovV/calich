package clientip

import (
	"net/http"
	"testing"
)

func TestFrom_StripsPort(t *testing.T) {
	r := &http.Request{RemoteAddr: "203.0.113.5:54321"}
	if got := From(r); got != "203.0.113.5" {
		t.Fatalf("expected 203.0.113.5, got %q", got)
	}
}

func TestFrom_StripsPortForIPv6(t *testing.T) {
	r := &http.Request{RemoteAddr: "[2001:db8::1]:54321"}
	if got := From(r); got != "2001:db8::1" {
		t.Fatalf("expected 2001:db8::1, got %q", got)
	}
}

func TestFrom_FallsBackToRawRemoteAddrWithoutAPort(t *testing.T) {
	r := &http.Request{RemoteAddr: "not-a-host-port"}
	if got := From(r); got != "not-a-host-port" {
		t.Fatalf("expected the raw RemoteAddr as a fallback, got %q", got)
	}
}

func TestFrom_IgnoresForwardedHeaders(t *testing.T) {
	r := &http.Request{RemoteAddr: "203.0.113.5:54321", Header: http.Header{}}
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.Header.Set("X-Real-IP", "5.6.7.8")

	if got := From(r); got != "203.0.113.5" {
		t.Fatalf("expected RemoteAddr to win over forwarded headers, got %q", got)
	}
}
