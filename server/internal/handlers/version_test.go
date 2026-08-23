package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/XiovV/calich/server/internal/version"
)

func TestVersion(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()

	Version(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}
}

// The handler must echo whatever the linker wrote into version.Version,
// unmodified — the label is opaque, never parsed or normalised (ADR-0072).
// This is what catches an ldflag pointing at the wrong symbol path: inject
// a value here and it has to come back out.
func TestVersion_EchoesTheInjectedLabelVerbatim(t *testing.T) {
	original := version.Version
	t.Cleanup(func() { version.Version = original })

	// Not valid semver, and not `v`-prefixed, precisely because nothing is
	// entitled to care.
	version.Version = "v1.2.3-rc.1+build.7"

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()

	Version(rec, req)

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body["version"] != "v1.2.3-rc.1+build.7" {
		t.Fatalf("expected the injected label back verbatim, got %q", body["version"])
	}
}

// An uninjected build — every `go run`, and `make build-backend` with no
// VERSION set — reports "dev" rather than an empty string, so the badge is
// visible during development and a broken wiring is noticed immediately.
func TestVersion_DefaultsToDev(t *testing.T) {
	if version.Version != "dev" {
		t.Fatalf("expected the uninjected default to be \"dev\", got %q", version.Version)
	}
}
