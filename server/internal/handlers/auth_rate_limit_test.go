package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/apptest"
)

// newRateLimitedAuthTestServer builds a Login/Register test server (no
// bootstrap account, so each test seeds or registers what it needs) over a
// graph whose auth/register rate limit ceilings are lowered enough for a
// handful of requests to trip them (#240, ADR-0070).
func newRateLimitedAuthTestServer(t *testing.T, maxAuthPerEmail, maxAuthPerIP, maxRegisterPerIP int, enableSignups bool) *httptest.Server {
	t.Helper()

	cfg := apptest.Config(t)
	cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword = "", "", ""
	cfg.AuthRateLimitPerEmail = maxAuthPerEmail
	cfg.AuthRateLimitPerIP = maxAuthPerIP
	cfg.RegisterRateLimitPerIP = maxRegisterPerIP
	cfg.EnableSignups = enableSignups
	g := newTestGraphWithConfig(t, cfg)

	h := NewAuthHandler(g.Auth, g.RateLimiter, false, false, false, true)

	r := chi.NewRouter()
	r.Post("/api/auth/login", h.Login)
	r.Post("/api/auth/register", h.Register)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func register(t *testing.T, srv *httptest.Server, name, email, password string) *http.Response {
	t.Helper()

	body, err := json.Marshal(registerRequest{Name: name, Email: email, Password: password})
	if err != nil {
		t.Fatalf("marshal register request: %v", err)
	}

	resp, err := http.Post(srv.URL+"/api/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/auth/register: %v", err)
	}
	return resp
}

func decodeErrorCode(t *testing.T, resp *http.Response) string {
	t.Helper()

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return body.Error.Code
}

func TestLogin_RateLimit_AttemptsBelowCeilingAreOrdinaryFailures(t *testing.T) {
	srv := newRateLimitedAuthTestServer(t, 3, 100, 100, true)

	seedUser(t, srv, "alice@example.com", "hunter22")

	for i := 0; i < 2; i++ {
		resp := login(t, srv, "alice@example.com", "wrong-password")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i, resp.StatusCode)
		}
		if code := decodeErrorCode(t, resp); code != "invalid_credentials" {
			t.Fatalf("attempt %d: expected invalid_credentials, got %q", i, code)
		}
	}
}

func TestLogin_RateLimit_RefusesOnceEmailCeilingReached(t *testing.T) {
	srv := newRateLimitedAuthTestServer(t, 2, 100, 100, true)

	seedUser(t, srv, "alice@example.com", "hunter22")

	for i := 0; i < 2; i++ {
		resp := login(t, srv, "alice@example.com", "wrong-password")
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i, resp.StatusCode)
		}
	}

	resp := login(t, srv, "alice@example.com", "wrong-password")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 once the ceiling is reached, got %d", resp.StatusCode)
	}
	if code := decodeErrorCode(t, resp); code != "rate_limited" {
		t.Fatalf("expected rate_limited, got %q", code)
	}
}

// TestLogin_RateLimit_LooksIdenticalForUnknownEmail pins ADR-0047's
// enumeration posture: a 429 must not distinguish a real account from one
// that doesn't exist, so an attacker can't use the rate limiter itself as an
// oracle for which emails are registered.
func TestLogin_RateLimit_LooksIdenticalForUnknownEmail(t *testing.T) {
	srv := newRateLimitedAuthTestServer(t, 2, 100, 100, true)

	seedUser(t, srv, "alice@example.com", "hunter22")

	realResp, unknownResp := tripEmailCeiling(t, srv, "alice@example.com"), tripEmailCeiling(t, srv, "nobody@example.com")
	defer realResp.resp.Body.Close()
	defer unknownResp.resp.Body.Close()

	if realResp.resp.StatusCode != unknownResp.resp.StatusCode {
		t.Fatalf("expected the same status for a real and an unknown email, got %d vs %d", realResp.resp.StatusCode, unknownResp.resp.StatusCode)
	}
	if realResp.code != unknownResp.code {
		t.Fatalf("expected the same error code for a real and an unknown email, got %q vs %q", realResp.code, unknownResp.code)
	}
}

type rateLimitProbe struct {
	resp *http.Response
	code string
}

// tripEmailCeiling fails login against email twice (the ceiling
// newRateLimitedAuthTestServer(t, 2, ...) sets up) and returns the 3rd,
// rate-limited response.
func tripEmailCeiling(t *testing.T, srv *httptest.Server, email string) rateLimitProbe {
	t.Helper()

	for i := 0; i < 2; i++ {
		resp := login(t, srv, email, "wrong-password")
		resp.Body.Close()
	}

	resp := login(t, srv, email, "wrong-password")
	return rateLimitProbe{resp: resp, code: decodeErrorCode(t, resp)}
}

func TestLogin_RateLimit_SuccessResetsEmailCeiling(t *testing.T) {
	srv := newRateLimitedAuthTestServer(t, 2, 100, 100, true)

	seedUser(t, srv, "alice@example.com", "hunter22")

	// One failed attempt, then a successful one.
	resp := login(t, srv, "alice@example.com", "wrong-password")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for the wrong password, got %d", resp.StatusCode)
	}

	resp = login(t, srv, "alice@example.com", "hunter22")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for the correct password, got %d", resp.StatusCode)
	}

	// Two more failed attempts still stay under the ceiling of 2 — if the
	// success above hadn't cleared the bucket, the 2nd of these would
	// already be the 3rd failure recorded and would 429.
	for i := 0; i < 2; i++ {
		resp := login(t, srv, "alice@example.com", "wrong-password")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("post-success attempt %d: expected 401 (not yet rate limited), got %d", i, resp.StatusCode)
		}
	}
}

func TestLogin_RateLimit_IPCeilingAppliesAcrossDifferentEmails(t *testing.T) {
	srv := newRateLimitedAuthTestServer(t, 100, 2, 100, true)

	// Two failed attempts against distinct, nonexistent emails from the
	// same test server's IP stay under the IP ceiling of 2.
	for i, email := range []string{"nobody-1@example.com", "nobody-2@example.com"} {
		resp := login(t, srv, email, "whatever")
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i, resp.StatusCode)
		}
	}

	// A 3rd distinct email trips the shared IP ceiling.
	resp := login(t, srv, "nobody-3@example.com", "whatever")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 once the ip ceiling is reached, got %d", resp.StatusCode)
	}
}

func TestRegister_RateLimit_AttemptsBelowCeilingSucceed(t *testing.T) {
	srv := newRateLimitedAuthTestServer(t, 100, 100, 2, true)

	resp := register(t, srv, "Alice", "alice@example.com", "hunter22")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRegister_RateLimit_RefusesOnceIPCeilingReached(t *testing.T) {
	srv := newRateLimitedAuthTestServer(t, 100, 100, 2, false)

	if resp := register(t, srv, "Alice", "alice@example.com", "hunter22"); resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("expected the 1st registration (creating the instance's first account) to succeed, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	// Signups are disabled and there's already an account, so this 2nd call
	// is refused — but it still counts against the ceiling (RecordRegisterAttempt
	// runs regardless of outcome).
	if resp := register(t, srv, "Bob", "bob@example.com", "hunter22"); resp.StatusCode != http.StatusForbidden {
		resp.Body.Close()
		t.Fatalf("expected the 2nd registration to be refused as self-registration disabled, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// The 3rd call is refused for hitting the ceiling instead.
	resp := register(t, srv, "Carol", "carol@example.com", "hunter22")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 once the ceiling is reached, got %d", resp.StatusCode)
	}
	if code := decodeErrorCode(t, resp); code != "rate_limited" {
		t.Fatalf("expected rate_limited, got %q", code)
	}
}

// TestRegister_RateLimit_AppliesIndependentlyOfEnableSignups covers the
// acceptance criterion that Register is throttled "independently of
// ENABLE_SIGNUPS" (#240): even once signups are already refusing every call
// on their own grounds, the endpoint's own call frequency is still bounded
// — the 3rd call here 429s rather than repeating the same 403
// self-registration-disabled response forever.
func TestRegister_RateLimit_AppliesIndependentlyOfEnableSignups(t *testing.T) {
	srv := newRateLimitedAuthTestServer(t, 100, 100, 3, false)

	// Seed the instance's one account directly via Register itself (always
	// allowed for the first account, ADR-0044), so every subsequent call
	// below is refused by ENABLE_SIGNUPS being false rather than by minting
	// a fresh first account each time.
	seedResp := register(t, srv, "Admin", "admin@example.com", "hunter22")
	seedResp.Body.Close()
	if seedResp.StatusCode != http.StatusOK {
		t.Fatalf("expected the seed registration to succeed, got %d", seedResp.StatusCode)
	}

	for i, email := range []string{"a@example.com", "b@example.com"} {
		resp := register(t, srv, "Someone", email, "hunter22")
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("attempt %d: expected 403 self-registration-disabled, got %d", i, resp.StatusCode)
		}
	}

	resp := register(t, srv, "Someone", "c@example.com", "hunter22")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected the rate limiter to still bite once its own ceiling is reached, even though ENABLE_SIGNUPS was already refusing every call, got %d", resp.StatusCode)
	}
}

// seedUser registers a User through the handler itself (rather than the
// repository directly), so it also counts against Register's own rate
// limiter the way a real signup would.
func seedUser(t *testing.T, srv *httptest.Server, email, password string) {
	t.Helper()

	resp := register(t, srv, "Test User", email, password)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed user %s: expected 200, got %d", email, resp.StatusCode)
	}
}
