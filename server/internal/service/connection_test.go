package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/XiovV/calich/server/internal/repository"
)

// fakeGoogleServer stands in for Google's token and userinfo endpoints
// (#285's testing decisions: a real local test server serving canned
// Provider JSON, not a mocked fetcher). refreshToken/email/scope are what
// the token/userinfo responses carry; tokenStatus/userinfoStatus let a test
// force a non-200 (a denied consent, a broken exchange); verifiedEmail
// overrides the userinfo response's verified_email, nil meaning true.
type fakeGoogleServer struct {
	*httptest.Server
	refreshToken, email, scope  string
	tokenStatus, userinfoStatus int
	verifiedEmail               *bool
}

func newFakeGoogleServer(t *testing.T) *fakeGoogleServer {
	t.Helper()

	f := &fakeGoogleServer{refreshToken: "1/fake-refresh-token", email: "someone@gmail.com", scope: "openid email https://www.googleapis.com/auth/calendar.events https://www.googleapis.com/auth/calendar.calendarlist.readonly"}

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if f.tokenStatus != 0 && f.tokenStatus != http.StatusOK {
			w.WriteHeader(f.tokenStatus)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse token request form: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "fake-access-token",
			"refresh_token": f.refreshToken,
			"scope":         f.scope,
			"expires_in":    3600,
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if f.userinfoStatus != 0 && f.userinfoStatus != http.StatusOK {
			w.WriteHeader(f.userinfoStatus)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fake-access-token" {
			t.Fatalf("expected userinfo request to carry the exchanged access token, got %q", r.Header.Get("Authorization"))
		}
		verifiedEmail := true
		if f.verifiedEmail != nil {
			verifiedEmail = *f.verifiedEmail
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"email": f.email, "verified_email": verifiedEmail})
	})

	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func newTestConnectionService(t *testing.T, google *fakeGoogleServer) (*ConnectionService, *AuthService, int64) {
	t.Helper()

	g := newTestGraph(t)

	user, err := g.UserRepo.Create(context.Background(), "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	connections := repository.NewConnectionRepository(g.DB)
	svc := NewConnectionService(connections, g.Auth, "test-client-id", "test-client-secret", "test-encryption-key", true,
		withGoogleHTTPClient(google.Client()),
		withGoogleEndpoints(google.URL+"/authorize", google.URL+"/token", google.URL+"/userinfo"),
	)

	return svc, g.Auth, user.ID
}

func TestConnectionService_Connect_BuildsAuthorizeURLCarryingASignedState(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, auth, userID := newTestConnectionService(t, google)

	authorizeURL, err := svc.Connect(userID, "https://calendar.example.com/api/connections/google/callback")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	q := parsed.Query()

	if got := q.Get("client_id"); got != "test-client-id" {
		t.Fatalf("expected client_id %q, got %q", "test-client-id", got)
	}
	if got := q.Get("redirect_uri"); got != "https://calendar.example.com/api/connections/google/callback" {
		t.Fatalf("expected redirect_uri to round-trip unchanged, got %q", got)
	}
	if got := q.Get("access_type"); got != "offline" {
		t.Fatalf("expected access_type=offline, got %q", got)
	}
	if got := q.Get("prompt"); got != "consent" {
		t.Fatalf("expected prompt=consent, got %q", got)
	}

	scopes := strings.Fields(q.Get("scope"))
	for _, want := range []string{"https://www.googleapis.com/auth/calendar.events", "https://www.googleapis.com/auth/calendar.calendarlist.readonly"} {
		if !slices.Contains(scopes, want) {
			t.Fatalf("expected scope to include %q, got %q", want, scopes)
		}
	}
	// The broad scope permits creating/deleting calendars and editing ACLs
	// (ADR-0050) — asking for it would overstate what this app does.
	if slices.Contains(scopes, "https://www.googleapis.com/auth/calendar") {
		t.Fatalf("expected the broad calendar scope never to be requested, got %q", scopes)
	}

	state := q.Get("state")
	if state == "" {
		t.Fatalf("expected a non-empty state")
	}
	stateUserID, err := auth.ParseConnectState(state)
	if err != nil {
		t.Fatalf("parse connect state: %v", err)
	}
	if stateUserID != userID {
		t.Fatalf("expected state to carry user id %d, got %d", userID, stateUserID)
	}
}

func TestConnectionService_Connect_RefusesWhenGoogleNotConfigured(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, _, userID := newTestConnectionService(t, google)
	svc.configured = false

	if _, err := svc.Connect(userID, "https://calendar.example.com/callback"); err != ErrGoogleNotConfigured {
		t.Fatalf("expected ErrGoogleNotConfigured, got %v", err)
	}
}

func TestConnectionService_Callback_CreatesConnection(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, auth, userID := newTestConnectionService(t, google)
	ctx := context.Background()

	redirectURI := "https://calendar.example.com/api/connections/google/callback"
	state, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}

	connection, err := svc.Callback(ctx, "auth-code", state, redirectURI)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}

	if connection.UserID != userID {
		t.Fatalf("expected connection for user %d, got %d", userID, connection.UserID)
	}
	if connection.Provider != repository.ProviderGoogle {
		t.Fatalf("expected provider %q, got %q", repository.ProviderGoogle, connection.Provider)
	}
	if connection.AccountEmail != "someone@gmail.com" {
		t.Fatalf("expected account email %q, got %q", "someone@gmail.com", connection.AccountEmail)
	}
	if connection.Status != repository.ConnectionStatusLive {
		t.Fatalf("expected status %q, got %q", repository.ConnectionStatusLive, connection.Status)
	}
	// The refresh token is never stored in the clear (#285, ADR-0052).
	if connection.RefreshToken == "1/fake-refresh-token" {
		t.Fatalf("expected the stored refresh token to be encrypted, got the raw value")
	}

	list, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected exactly one connection, got %d", len(list))
	}
}

func TestConnectionService_Callback_ReconnectingSameAccountReusesTheRow(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, auth, userID := newTestConnectionService(t, google)
	ctx := context.Background()
	redirectURI := "https://calendar.example.com/api/connections/google/callback"

	state1, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}
	first, err := svc.Callback(ctx, "auth-code-1", state1, redirectURI)
	if err != nil {
		t.Fatalf("first callback: %v", err)
	}

	google.refreshToken = "1/fake-refresh-token-2"
	state2, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}
	second, err := svc.Callback(ctx, "auth-code-2", state2, redirectURI)
	if err != nil {
		t.Fatalf("second callback: %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("expected reconnect to reuse connection id %d, got %d", first.ID, second.ID)
	}

	list, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected exactly one connection after reconnecting, got %d", len(list))
	}
}

func TestConnectionService_Callback_RefusesDisabledAccount(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, auth, userID := newTestConnectionService(t, google)
	ctx := context.Background()

	state, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}

	// The account is disabled after Connect issued the state but before
	// Callback runs — the window a User spends on Google's own consent
	// screen (#285).
	if _, err := auth.users.SetDisabled(ctx, userID, true); err != nil {
		t.Fatalf("disable user: %v", err)
	}

	if _, err := svc.Callback(ctx, "auth-code", state, "https://calendar.example.com/callback"); err != ErrConnectAccountNotActive {
		t.Fatalf("expected ErrConnectAccountNotActive, got %v", err)
	}
}

func TestConnectionService_Callback_RefusesAccountThatMustChangePassword(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, auth, _ := newTestConnectionService(t, google)
	ctx := context.Background()

	user, err := auth.users.Create(ctx, "user-b", "user-b@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	state, err := auth.IssueConnectState(user.ID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}

	if _, err := svc.Callback(ctx, "auth-code", state, "https://calendar.example.com/callback"); err != ErrConnectAccountNotActive {
		t.Fatalf("expected ErrConnectAccountNotActive, got %v", err)
	}
}

// TestConnectionService_Callback_RefusesInsufficientScopes covers Google's
// granular-consent screen: a User can approve sign-in while denying Calendar
// access specifically, and the token exchange still succeeds with a
// refresh token either way (#285) — Callback must notice before ever
// showing the Connection as live.
func TestConnectionService_Callback_RefusesInsufficientScopes(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.scope = "openid email" // calendar scopes denied
	svc, auth, userID := newTestConnectionService(t, google)

	state, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}

	if _, err := svc.Callback(context.Background(), "auth-code", state, "https://calendar.example.com/callback"); err == nil {
		t.Fatalf("expected insufficient granted scopes to fail")
	}
}

func TestConnectionService_Callback_RefusesUnverifiedEmail(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.verifiedEmail = boolPtr(false)
	svc, auth, userID := newTestConnectionService(t, google)

	state, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}

	if _, err := svc.Callback(context.Background(), "auth-code", state, "https://calendar.example.com/callback"); err == nil {
		t.Fatalf("expected an unverified google email to fail")
	}
}

func boolPtr(v bool) *bool { return &v }

func TestConnectionService_Callback_InvalidStateFails(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, _, _ := newTestConnectionService(t, google)

	if _, err := svc.Callback(context.Background(), "auth-code", "not-a-real-state", "https://calendar.example.com/callback"); err == nil {
		t.Fatalf("expected an invalid state to fail")
	}
}

func TestConnectionService_Callback_TokenExchangeFailureIsSurfaced(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.tokenStatus = http.StatusBadRequest
	svc, auth, userID := newTestConnectionService(t, google)

	state, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}

	if _, err := svc.Callback(context.Background(), "auth-code", state, "https://calendar.example.com/callback"); err == nil {
		t.Fatalf("expected a token exchange failure to be surfaced")
	}
}

func TestConnectionService_Disconnect_RemovesConnection(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, auth, userID := newTestConnectionService(t, google)
	ctx := context.Background()

	state, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}
	connection, err := svc.Callback(ctx, "auth-code", state, "https://calendar.example.com/callback")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}

	if err := svc.Disconnect(ctx, userID, connection.ID); err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	list, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no connections after disconnect, got %d", len(list))
	}
}

func TestConnectionService_Disconnect_NotFound(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, _, userID := newTestConnectionService(t, google)

	if err := svc.Disconnect(context.Background(), userID, 999); err != ErrConnectionNotFound {
		t.Fatalf("expected ErrConnectionNotFound, got %v", err)
	}
}
