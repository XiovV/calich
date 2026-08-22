package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/httpauth"
)

func newAppPasswordTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	g := newTestGraph(t)

	auth := g.Auth
	if _, _, err := auth.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	appPasswords := g.AppPasswords
	h := NewAppPasswordHandler(appPasswords)
	authHandler := NewAuthHandler(auth, g.RateLimiter, false, false, true)

	r := chi.NewRouter()
	r.Post("/api/auth/login", authHandler.Login)
	r.Route("/api/app-passwords", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Delete("/{id}", h.Revoke)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	loginResp := login(t, srv, "admin@example.com", "admin")
	defer loginResp.Body.Close()
	var loggedIn loginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&loggedIn); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	return srv, loggedIn.AccessToken
}

func createAppPassword(t *testing.T, srv *httptest.Server, accessToken, label string) *http.Response {
	t.Helper()

	body, err := json.Marshal(createAppPasswordRequest{Label: label})
	if err != nil {
		t.Fatalf("marshal create app password request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/app-passwords/", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/app-passwords: %v", err)
	}
	return resp
}

func TestAppPasswordCreate_ReturnsSecretOnce(t *testing.T) {
	srv, accessToken := newAppPasswordTestServer(t)

	resp := createAppPassword(t, srv, accessToken, "iPhone")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var created createAppPasswordResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if created.Secret == "" {
		t.Fatalf("expected a non-empty secret on creation")
	}
	if created.Label != "iPhone" {
		t.Fatalf("expected label %q, got %q", "iPhone", created.Label)
	}
}

func TestAppPasswordCreate_RejectsEmptyLabel(t *testing.T) {
	srv, accessToken := newAppPasswordTestServer(t)

	resp := createAppPassword(t, srv, accessToken, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAppPasswordCreate_NoToken_Returns401(t *testing.T) {
	srv, _ := newAppPasswordTestServer(t)

	resp, err := http.Post(srv.URL+"/api/app-passwords/", "application/json", bytes.NewReader([]byte(`{"label":"iPhone"}`)))
	if err != nil {
		t.Fatalf("POST /api/app-passwords: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAppPasswordList_NeverIncludesSecret(t *testing.T) {
	srv, accessToken := newAppPasswordTestServer(t)

	createResp := createAppPassword(t, srv, accessToken, "iPhone")
	defer createResp.Body.Close()
	var created createAppPasswordResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/app-passwords/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/app-passwords: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := readAllString(resp)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if len(body) == 0 {
		t.Fatalf("expected a non-empty response body")
	}

	var list []appPasswordResponse
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 app password, got %d", len(list))
	}
	if list[0].ID != created.ID {
		t.Fatalf("expected app password id %d, got %d", created.ID, list[0].ID)
	}
	if !bytes.Contains([]byte(body), []byte(`"label"`)) {
		t.Fatalf("expected the response to include label")
	}
	if bytes.Contains([]byte(body), []byte(created.Secret)) {
		t.Fatalf("expected the list response to never include the plaintext secret")
	}
}

func TestAppPasswordRevoke_RemovesIt(t *testing.T) {
	srv, accessToken := newAppPasswordTestServer(t)

	createResp := createAppPassword(t, srv, accessToken, "iPhone")
	defer createResp.Body.Close()
	var created createAppPasswordResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/app-passwords/%d", srv.URL, created.ID), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/app-passwords/%d: %v", created.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	listReq, err := http.NewRequest(http.MethodGet, srv.URL+"/api/app-passwords/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	listReq.Header.Set("Authorization", "Bearer "+accessToken)

	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("GET /api/app-passwords: %v", err)
	}
	defer listResp.Body.Close()

	var list []appPasswordResponse
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no app passwords after revoke, got %d", len(list))
	}
}

func TestAppPasswordRevoke_LeavesWebLoginUnaffected(t *testing.T) {
	srv, accessToken := newAppPasswordTestServer(t)

	createResp := createAppPassword(t, srv, accessToken, "iPhone")
	defer createResp.Body.Close()
	var created createAppPasswordResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/app-passwords/%d", srv.URL, created.ID), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/app-passwords/%d: %v", created.ID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	// The web login is a separate credential (the account password, checked via
	// AuthService.Login) — revoking an app password must not touch it.
	loginResp := login(t, srv, "admin@example.com", "admin")
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("expected the web login to still succeed after revoking an app password, got %d", loginResp.StatusCode)
	}
}

func TestAppPasswordRevoke_NotFound_Returns404(t *testing.T) {
	srv, accessToken := newAppPasswordTestServer(t)

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/app-passwords/999", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/app-passwords/999: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func readAllString(resp *http.Response) (string, error) {
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return "", err
	}
	return buf.String(), nil
}
