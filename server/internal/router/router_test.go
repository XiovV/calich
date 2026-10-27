package router

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/XiovV/calich/server/internal/app"
	"github.com/XiovV/calich/server/internal/apptest"
)

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()

	// The whole graph, handlers included, over an in-memory database (#214,
	// ADR-0065). This package is the one that can take it: internal/app
	// doesn't import router, so router's own in-package tests can import app.
	a, err := app.NewInMemory(apptest.Config(t))
	if err != nil {
		t.Fatalf("build app: %v", err)
	}
	t.Cleanup(func() { a.Close() })

	if _, _, err := a.Auth.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	r, err := New(slog.New(slog.NewTextHandler(io.Discard, nil)), a.AuthHandler, a.CalendarHandler, a.EventHandler, a.AttachmentHandler, a.NotificationHandler, a.AppPasswordHandler, a.AccountHandler, a.UserHandler, a.WorkspaceHandler, a.GroupHandler, a.CalendarSetHandler, a.TaskListHandler, a.TaskHandler, a.ConnectionHandler, a.AvailabilityScheduleHandler, a.BookingLinkHandler, a.PublicBookingHandler, a.CalDAVHandler, a.Auth, a.Auth, a.AppPasswords, a.RateLimiter, a.Auth, a.Workspaces)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func TestRouter_APIRouteIsNotShadowedBySPACatchAll(t *testing.T) {
	r := newTestRouter(t)

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /api/health to return 200, got %d", resp.StatusCode)
	}
}

func TestRouter_UnknownClientRouteFallsBackToSPA(t *testing.T) {
	r := newTestRouter(t)

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/some/client/route")
	if err != nil {
		t.Fatalf("GET /some/client/route: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (index.html fallback), got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(body) == 0 {
		t.Fatalf("expected non-empty index.html body")
	}
}

// /api/version must be reachable with no credentials at all (#256,
// ADR-0072), and must not be swallowed by the SPA catch-all the way any
// unknown path is. Both halves matter: the badge is fetched before the
// shell knows anything, and index.html would answer 200 here too, so the
// body is what tells the two apart.
func TestRouter_VersionIsPublicAndNotShadowedBySPACatchAll(t *testing.T) {
	r := newTestRouter(t)

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/version")
	if err != nil {
		t.Fatalf("GET /api/version: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /api/version to return 200 unauthenticated, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("expected a JSON body from the handler, not the SPA fallback: %v", err)
	}
	if body["version"] == "" {
		t.Fatalf("expected a non-empty version label, got %#v", body)
	}
}

// macOS Calendar can end up polling the server root as its principal: a
// PROPFIND or OPTIONS on "/" must be sent into CalDAV discovery rather than
// answered with index.html, which the client reports as "The request to the
// server failed". GET / still serves the SPA.
func TestRouter_DAVRequestOnRootRedirectsToWellKnown(t *testing.T) {
	r := newTestRouter(t)

	srv := httptest.NewServer(r)
	defer srv.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	for _, method := range []string{"PROPFIND", "REPORT", http.MethodOptions} {
		req, err := http.NewRequest(method, srv.URL+"/", nil)
		if err != nil {
			t.Fatalf("build %s /: %v", method, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s /: %v", method, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusPermanentRedirect {
			t.Fatalf("%s /: expected 308, got %d", method, resp.StatusCode)
		}
		if got := resp.Header.Get("Location"); got != "/.well-known/caldav" {
			t.Fatalf("%s /: expected Location /.well-known/caldav, got %q", method, got)
		}
	}

	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /: expected 200 (SPA), got %d", resp.StatusCode)
	}
}

// Following the root redirect with no credentials must land on the CalDAV
// auth challenge, not the SPA — that 401 is what makes a native client send
// its App password and rediscover its principal.
func TestRouter_RootPropfindReachesCalDAVAuthChallenge(t *testing.T) {
	r := newTestRouter(t)

	srv := httptest.NewServer(r)
	defer srv.Close()

	req, err := http.NewRequest("PROPFIND", srv.URL+"/", nil)
	if err != nil {
		t.Fatalf("build PROPFIND /: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PROPFIND /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 from /.well-known/caldav, got %d", resp.StatusCode)
	}
	if resp.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("expected a WWW-Authenticate challenge")
	}
}
