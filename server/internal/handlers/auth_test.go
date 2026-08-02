package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

func newAuthTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	auth := service.NewAuthService(users, sessions, []byte("test-secret"), "", "")

	if err := auth.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	h := NewAuthHandler(auth)

	r := chi.NewRouter()
	r.Post("/api/auth/login", h.Login)
	r.With(httpauth.RequireAuth(auth)).Get("/api/auth/me", h.Me)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func login(t *testing.T, srv *httptest.Server, username, password string) *http.Response {
	t.Helper()

	body, err := json.Marshal(loginRequest{Username: username, Password: password})
	if err != nil {
		t.Fatalf("marshal login request: %v", err)
	}

	resp, err := http.Post(srv.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/auth/login: %v", err)
	}
	return resp
}

func TestLogin_Success_SetsRefreshCookieAndReturnsAccessToken(t *testing.T) {
	srv := newAuthTestServer(t)

	resp := login(t, srv, "admin", "admin")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.AccessToken == "" {
		t.Fatalf("expected a non-empty access token")
	}
	if !body.MustChangePassword {
		t.Fatalf("expected must_change_password to be true for the default bootstrap user")
	}

	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "refresh_token" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatalf("expected a refresh_token cookie to be set")
	}
	if !cookie.HttpOnly {
		t.Fatalf("expected refresh_token cookie to be HttpOnly")
	}
	if !cookie.Secure {
		t.Fatalf("expected refresh_token cookie to be Secure")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected refresh_token cookie SameSite=Lax, got %v", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Fatalf("expected refresh_token cookie Path=/, got %q", cookie.Path)
	}
}

func TestLogin_InvalidCredentials_Returns401(t *testing.T) {
	srv := newAuthTestServer(t)

	resp := login(t, srv, "admin", "wrong-password")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestMe_NoToken_Returns401(t *testing.T) {
	srv := newAuthTestServer(t)

	resp, err := http.Get(srv.URL + "/api/auth/me")
	if err != nil {
		t.Fatalf("GET /api/auth/me: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestMe_ValidToken_ReturnsUser(t *testing.T) {
	srv := newAuthTestServer(t)

	loginResp := login(t, srv, "admin", "admin")
	defer loginResp.Body.Close()

	var loggedIn loginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&loggedIn); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/auth/me", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+loggedIn.AccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/auth/me: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.Username != "admin" {
		t.Fatalf("expected username 'admin', got %q", me.Username)
	}
	if !me.MustChangePassword {
		t.Fatalf("expected must_change_password to be true")
	}
}
