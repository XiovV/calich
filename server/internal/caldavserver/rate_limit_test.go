package caldavserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/apptest"
	"github.com/XiovV/calich/server/internal/httpauth"
)

// newRateLimitedCalDAVServer is newTestCalDAVEnv's own setup, trimmed to
// what #240/ADR-0070's tests need, over a graph whose auth rate limit
// ceilings are lowered so a handful of requests can trip them.
func newRateLimitedCalDAVServer(t *testing.T, maxAuthPerEmail, maxAuthPerIP int) (srv *httptest.Server, appPasswordSecret, userEmail string) {
	t.Helper()

	cfg := apptest.Config(t)
	cfg.AuthRateLimitPerEmail = maxAuthPerEmail
	cfg.AuthRateLimitPerIP = maxAuthPerIP
	g := newTestGraphWithConfig(t, cfg)

	user, _, err := g.Auth.Bootstrap(context.Background())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	created, err := g.AppPasswords.Create(context.Background(), user.ID, "Test device")
	if err != nil {
		t.Fatalf("create app password: %v", err)
	}

	backend := NewBackend(g.Calendars, g.Events, g.Attachments, testMaxAttachmentSize, testMaxAttachmentsPerEvent)
	handler := NewHTTPHandler(backend)

	r := chi.NewRouter()
	r.Route(pathPrefix, func(r chi.Router) {
		r.Use(httpauth.RequireCalDAVAuth(g.AppPasswords, g.RateLimiter))
		r.Handle("/", handler)
		r.Handle("/*", handler)
	})

	testSrv := httptest.NewServer(r)
	t.Cleanup(testSrv.Close)

	return testSrv, created.Secret, user.Email
}

func TestRequireCalDAVAuth_RateLimit_RefusesOnceEmailCeilingReached(t *testing.T) {
	srv, _, userEmail := newRateLimitedCalDAVServer(t, 2, 100)

	// Two wrong-password attempts stay under the ceiling of 2, and are
	// refused as ordinary bad credentials (401), not as rate limited.
	for i := 0; i < 2; i++ {
		resp := propfind(t, srv, pathPrefix+"/", userEmail, "wrong-secret", "0", "")
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i, resp.StatusCode)
		}
	}

	// The 3rd attempt is refused for hitting the ceiling instead of for a
	// bad password.
	resp := propfind(t, srv, pathPrefix+"/", userEmail, "wrong-secret", "0", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 once the ceiling is reached, got %d", resp.StatusCode)
	}
}

func TestRequireCalDAVAuth_RateLimit_RefusesOnceIPCeilingReachedAcrossDifferentUsernames(t *testing.T) {
	srv, _, _ := newRateLimitedCalDAVServer(t, 100, 2)

	// Two failed attempts against distinct (nonexistent) usernames from the
	// same IP stay under the IP ceiling of 2.
	for i, email := range []string{"nobody-1@example.com", "nobody-2@example.com"} {
		resp := propfind(t, srv, pathPrefix+"/", email, "whatever", "0", "")
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i, resp.StatusCode)
		}
	}

	// A 3rd distinct username from the same IP trips the IP ceiling, even
	// though none of the three usernames individually reached its own
	// (much higher) email ceiling.
	resp := propfind(t, srv, pathPrefix+"/", "nobody-3@example.com", "whatever", "0", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 once the ip ceiling is reached, got %d", resp.StatusCode)
	}
}

func TestRequireCalDAVAuth_RateLimit_SuccessResetsEmailCeiling(t *testing.T) {
	srv, appPasswordSecret, userEmail := newRateLimitedCalDAVServer(t, 2, 100)

	// One failed attempt, then a successful one — the success should clear
	// the failure count rather than leaving it at 1/2.
	resp := propfind(t, srv, pathPrefix+"/", userEmail, "wrong-secret", "0", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for the wrong password, got %d", resp.StatusCode)
	}

	resp = propfind(t, srv, pathPrefix+"/", userEmail, appPasswordSecret, "0", "")
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusTooManyRequests {
		t.Fatalf("expected the correct app password to authenticate, got %d", resp.StatusCode)
	}

	// Two more failed attempts still stay under the ceiling of 2 — if the
	// success above hadn't cleared the bucket, this second attempt would
	// already be the 3rd failure recorded and would 429.
	for i := 0; i < 2; i++ {
		resp := propfind(t, srv, pathPrefix+"/", userEmail, "wrong-secret", "0", "")
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("post-success attempt %d: expected 401 (not yet rate limited), got %d", i, resp.StatusCode)
		}
	}
}
