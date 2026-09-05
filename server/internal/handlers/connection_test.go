package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/apptest"
	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/service"
)

// fakeGoogleTestServer stands in for Google's token and userinfo endpoints
// (#285's testing decisions: a real local test server serving canned
// Provider JSON, not a mocked fetcher) — the handlers-package sibling of
// service's own fakeGoogleServer, kept separate rather than shared across
// packages for the same reason every other handler test builds its own
// fixture rather than importing another package's test helpers.
type fakeGoogleTestServer struct {
	*httptest.Server
	refreshToken, email string
}

func newFakeGoogleTestServer(t *testing.T) *fakeGoogleTestServer {
	t.Helper()

	f := &fakeGoogleTestServer{refreshToken: "1/fake-refresh-token", email: "someone@gmail.com"}

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "fake-access-token",
			"refresh_token": f.refreshToken,
			"scope":         "openid email https://www.googleapis.com/auth/calendar.events https://www.googleapis.com/auth/calendar.calendarlist.readonly",
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"email": f.email, "verified_email": true})
	})

	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func newConnectionTestServer(t *testing.T) (*httptest.Server, string, *fakeGoogleTestServer) {
	t.Helper()

	google := newFakeGoogleTestServer(t)

	cfg := apptest.GoogleConfig(t)
	g := newTestGraphWithConfig(t, cfg,
		service.WithGoogleHTTPClient(google.Client()),
		service.WithGoogleEndpoints(google.URL+"/authorize", google.URL+"/token", google.URL+"/userinfo"),
	)

	auth := g.Auth
	if _, _, err := auth.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	h := NewConnectionHandler(g.Connections)
	authHandler := NewAuthHandler(auth, g.RateLimiter, false, false, cfg.GoogleConfigured(), true)

	r := chi.NewRouter()
	r.Post("/api/auth/login", authHandler.Login)
	r.With(httpauth.RequireAuth(auth)).Get("/api/auth/me", authHandler.Me)
	r.Route("/api/connections", func(r chi.Router) {
		r.Get("/google/callback", h.Callback)
		r.Group(func(r chi.Router) {
			r.Use(httpauth.RequireAuth(auth))
			r.Get("/", h.List)
			r.Get("/google/connect", h.Connect)
			r.Delete("/{id}", h.Disconnect)
		})
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	loginResp := login(t, srv, "admin@example.com", "admin")
	defer loginResp.Body.Close()
	var loggedIn loginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&loggedIn); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	return srv, loggedIn.AccessToken, google
}

func TestConnectionHandler_Connect_ReturnsAuthorizeURL(t *testing.T) {
	srv, accessToken, _ := newConnectionTestServer(t)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/connections/google/connect", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/connections/google/connect: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body connectResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	parsed, err := url.Parse(body.URL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	if parsed.Query().Get("state") == "" {
		t.Fatalf("expected authorize url to carry a state parameter, got %q", body.URL)
	}
}

func TestConnectionHandler_Connect_RequiresAuth(t *testing.T) {
	srv, _, _ := newConnectionTestServer(t)

	resp, err := http.Get(srv.URL + "/api/connections/google/connect")
	if err != nil {
		t.Fatalf("GET /api/connections/google/connect: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.StatusCode)
	}
}

// TestConnectionHandler_Callback_RoundTripsIntoAConnection covers #285's
// "authorizing round-trips back into the app" acceptance criterion at the
// HTTP layer: Connect's own authorize URL carries a real state, and handing
// it straight back to Callback (as Google's redirect would) must produce a
// Connection and a redirect back into the SPA.
func TestConnectionHandler_Callback_RoundTripsIntoAConnection(t *testing.T) {
	srv, accessToken, _ := newConnectionTestServer(t)
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}

	connectReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/connections/google/connect", nil)
	connectReq.Header.Set("Authorization", "Bearer "+accessToken)
	connectResp, err := http.DefaultClient.Do(connectReq)
	if err != nil {
		t.Fatalf("GET /api/connections/google/connect: %v", err)
	}
	defer connectResp.Body.Close()
	var connected connectResponse
	if err := json.NewDecoder(connectResp.Body).Decode(&connected); err != nil {
		t.Fatalf("decode connect response: %v", err)
	}
	parsed, err := url.Parse(connected.URL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	state := parsed.Query().Get("state")

	callbackURL := srv.URL + "/api/connections/google/callback?code=fake-auth-code&state=" + url.QueryEscape(state)
	callbackResp, err := client.Get(callbackURL)
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer callbackResp.Body.Close()

	if callbackResp.StatusCode != http.StatusFound {
		t.Fatalf("expected a redirect (302), got %d", callbackResp.StatusCode)
	}
	location := callbackResp.Header.Get("Location")
	if location != "/settings/connections?connected=1" {
		t.Fatalf("expected redirect to /settings/connections?connected=1, got %q", location)
	}

	listReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/connections/", nil)
	listReq.Header.Set("Authorization", "Bearer "+accessToken)
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("GET /api/connections/: %v", err)
	}
	defer listResp.Body.Close()

	var connections []connectionResponse
	if err := json.NewDecoder(listResp.Body).Decode(&connections); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(connections) != 1 {
		t.Fatalf("expected exactly one connection, got %d", len(connections))
	}
	if connections[0].AccountEmail != "someone@gmail.com" {
		t.Fatalf("expected account email %q, got %q", "someone@gmail.com", connections[0].AccountEmail)
	}
	if connections[0].Status != "live" {
		t.Fatalf("expected status %q, got %q", "live", connections[0].Status)
	}
}

func TestConnectionHandler_Callback_InvalidStateRedirectsWithError(t *testing.T) {
	srv, _, _ := newConnectionTestServer(t)
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := client.Get(srv.URL + "/api/connections/google/callback?code=fake-auth-code&state=not-a-real-state")
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected a redirect (302), got %d", resp.StatusCode)
	}
	if location := resp.Header.Get("Location"); location != "/settings/connections?connect_error=failed" {
		t.Fatalf("expected an error redirect, got %q", location)
	}
}

func TestConnectionHandler_Callback_DeclinedConsentRedirectsWithError(t *testing.T) {
	srv, _, _ := newConnectionTestServer(t)
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := client.Get(srv.URL + "/api/connections/google/callback?error=access_denied")
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected a redirect (302), got %d", resp.StatusCode)
	}
	if location := resp.Header.Get("Location"); location != "/settings/connections?connect_error=declined" {
		t.Fatalf("expected a declined-consent redirect, got %q", location)
	}
}

func TestConnectionHandler_List_Empty(t *testing.T) {
	srv, accessToken, _ := newConnectionTestServer(t)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/connections/", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/connections/: %v", err)
	}
	defer resp.Body.Close()

	var connections []connectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&connections); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(connections) != 0 {
		t.Fatalf("expected no connections, got %d", len(connections))
	}
}

func TestConnectionHandler_Disconnect_RemovesConnection(t *testing.T) {
	srv, accessToken, _ := newConnectionTestServer(t)
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}

	connectReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/connections/google/connect", nil)
	connectReq.Header.Set("Authorization", "Bearer "+accessToken)
	connectResp, err := http.DefaultClient.Do(connectReq)
	if err != nil {
		t.Fatalf("GET /api/connections/google/connect: %v", err)
	}
	defer connectResp.Body.Close()
	var connected connectResponse
	if err := json.NewDecoder(connectResp.Body).Decode(&connected); err != nil {
		t.Fatalf("decode connect response: %v", err)
	}
	parsed, _ := url.Parse(connected.URL)
	state := parsed.Query().Get("state")

	callbackResp, err := client.Get(srv.URL + "/api/connections/google/callback?code=fake-auth-code&state=" + url.QueryEscape(state))
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	callbackResp.Body.Close()

	listReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/connections/", nil)
	listReq.Header.Set("Authorization", "Bearer "+accessToken)
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("GET /api/connections/: %v", err)
	}
	var connections []connectionResponse
	if err := json.NewDecoder(listResp.Body).Decode(&connections); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	listResp.Body.Close()
	if len(connections) != 1 {
		t.Fatalf("expected exactly one connection before disconnect, got %d", len(connections))
	}

	deleteReq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/connections/"+strconv.FormatInt(connections[0].ID, 10), nil)
	deleteReq.Header.Set("Authorization", "Bearer "+accessToken)
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("DELETE /api/connections/%d: %v", connections[0].ID, err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", deleteResp.StatusCode)
	}

	listReq2, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/connections/", nil)
	listReq2.Header.Set("Authorization", "Bearer "+accessToken)
	listResp2, err := http.DefaultClient.Do(listReq2)
	if err != nil {
		t.Fatalf("GET /api/connections/: %v", err)
	}
	defer listResp2.Body.Close()
	var afterDelete []connectionResponse
	if err := json.NewDecoder(listResp2.Body).Decode(&afterDelete); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(afterDelete) != 0 {
		t.Fatalf("expected no connections after disconnect, got %d", len(afterDelete))
	}
}

// TestMe_GoogleProviderAvailable_TrueWithGoogleConfigured and its
// FalseWithout sibling in auth_test.go cover ADR-0051's "the Provider is
// absent from the UI entirely when unconfigured" directly on the Me
// response, mirroring EmailReminderChannelAvailable's own SMTP-gated pair.
func TestMe_GoogleProviderAvailable_TrueWithGoogleConfigured(t *testing.T) {
	srv, accessToken, _ := newConnectionTestServer(t)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/auth/me: %v", err)
	}
	defer resp.Body.Close()

	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !me.GoogleProviderAvailable {
		t.Fatalf("expected the Google provider to be available once configured")
	}
}

func TestConnectionHandler_Disconnect_NotFound(t *testing.T) {
	srv, accessToken, _ := newConnectionTestServer(t)

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/connections/999", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/connections/999: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
}
