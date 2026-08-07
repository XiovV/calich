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

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

func newAccountTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	auth := service.NewAuthService(users, sessions, []byte("test-secret"), "", "")
	if _, _, err := auth.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	calendars := service.NewCalendarService(repository.NewCalendarRepository(sqlDB))
	accounts := service.NewAccountService(users, sessions, calendars)
	h := NewAccountHandler(accounts)
	authHandler := NewAuthHandler(auth, false)

	r := chi.NewRouter()
	r.Post("/api/auth/login", authHandler.Login)
	r.Route("/api/accounts", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Use(httpauth.RequireAdmin(auth))

		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Post("/{id}/reset-password", h.ResetPassword)
		r.Put("/{id}/admin", h.SetAdmin)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	loginResp := login(t, srv, "admin", "admin")
	defer loginResp.Body.Close()
	var loggedIn loginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&loggedIn); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	return srv, loggedIn.AccessToken
}

func createAccount(t *testing.T, srv *httptest.Server, accessToken, username, password string) *http.Response {
	t.Helper()

	body, err := json.Marshal(createAccountRequest{Username: username, Password: password})
	if err != nil {
		t.Fatalf("marshal create account request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/accounts/", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/accounts: %v", err)
	}
	return resp
}

func TestAccountCreate_ReturnsNewAccountRequiringPasswordChange(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	resp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var created accountResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if created.Username != "alice" {
		t.Fatalf("expected username alice, got %q", created.Username)
	}
	if !created.MustChangePassword {
		t.Fatalf("expected the new account to require a password change")
	}
	if created.IsAdmin {
		t.Fatalf("expected the new account to not be an admin")
	}
}

func TestAccountCreate_RejectsEmptyUsername(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	resp := createAccount(t, srv, accessToken, "", "temp-secret")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAccountCreate_RejectsEmptyPassword(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	resp := createAccount(t, srv, accessToken, "alice", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAccountCreate_DuplicateUsername_Returns409(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	firstResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	firstResp.Body.Close()

	resp := createAccount(t, srv, accessToken, "alice", "another-secret")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAccountCreate_NoToken_Returns401(t *testing.T) {
	srv, _ := newAccountTestServer(t)

	resp, err := http.Post(srv.URL+"/api/accounts/", "application/json", bytes.NewReader([]byte(`{"username":"alice","password":"temp-secret"}`)))
	if err != nil {
		t.Fatalf("POST /api/accounts: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAccountCreate_NonAdmin_Returns403(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer createResp.Body.Close()

	// alice isn't an admin, so her token must be refused by every
	// account-management endpoint (ADR-0037) — even though she's a valid,
	// authenticated user.
	aliceLoginResp := login(t, srv, "alice", "temp-secret")
	defer aliceLoginResp.Body.Close()
	var aliceLoggedIn loginResponse
	if err := json.NewDecoder(aliceLoginResp.Body).Decode(&aliceLoggedIn); err != nil {
		t.Fatalf("decode alice login response: %v", err)
	}

	resp := createAccount(t, srv, aliceLoggedIn.AccessToken, "bob", "temp-secret")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// nonAdminAccessToken creates a non-admin account and returns an access
// token for it, for asserting every account-management endpoint refuses a
// non-Admin caller (ADR-0037).
func nonAdminAccessToken(t *testing.T, srv *httptest.Server, adminAccessToken string) string {
	t.Helper()

	createResp := createAccount(t, srv, adminAccessToken, "alice", "temp-secret")
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating alice, got %d", createResp.StatusCode)
	}

	loginResp := login(t, srv, "alice", "temp-secret")
	defer loginResp.Body.Close()
	var loggedIn loginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&loggedIn); err != nil {
		t.Fatalf("decode alice login response: %v", err)
	}
	return loggedIn.AccessToken
}

func TestAccountList_NonAdmin_Returns403(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)
	aliceToken := nonAdminAccessToken(t, srv, accessToken)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/accounts/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+aliceToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/accounts: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestAccountResetPassword_NonAdmin_Returns403(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)
	aliceToken := nonAdminAccessToken(t, srv, accessToken)

	body, err := json.Marshal(resetPasswordRequest{Password: "new-temp-secret"})
	if err != nil {
		t.Fatalf("marshal reset password request: %v", err)
	}
	// Targeting id 1 (the bootstrapped admin) — the point is that alice, a
	// non-admin, must be refused before the target id is even consulted.
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/accounts/1/reset-password", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST reset-password: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestAccountSetAdmin_NonAdmin_Returns403(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)
	aliceToken := nonAdminAccessToken(t, srv, accessToken)

	body, err := json.Marshal(setAdminRequest{IsAdmin: true})
	if err != nil {
		t.Fatalf("marshal set admin request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/accounts/1/admin", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT admin: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestAccountResetPassword_UnknownID_Returns404(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	body, err := json.Marshal(resetPasswordRequest{Password: "new-temp-secret"})
	if err != nil {
		t.Fatalf("marshal reset password request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/accounts/999/reset-password", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST reset-password: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAccountSetAdmin_UnknownID_Returns404(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	body, err := json.Marshal(setAdminRequest{IsAdmin: true})
	if err != nil {
		t.Fatalf("marshal set admin request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/accounts/999/admin", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT admin: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAccountList_ReturnsEveryAccount(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	createResp.Body.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/accounts/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/accounts: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var list []accountResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 accounts (admin + alice), got %d", len(list))
	}
}

func TestAccountResetPassword_ForcesPasswordChangeAndLetsThemLogInWithTheNewOne(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer createResp.Body.Close()
	var created accountResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	body, err := json.Marshal(resetPasswordRequest{Password: "new-temp-secret"})
	if err != nil {
		t.Fatalf("marshal reset password request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/accounts/%d/reset-password", srv.URL, created.ID), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST reset-password: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var updated accountResponse
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode reset response: %v", err)
	}
	if !updated.MustChangePassword {
		t.Fatalf("expected the reset account to require a password change again")
	}

	loginResp := login(t, srv, "alice", "new-temp-secret")
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("expected login with the new temporary password to succeed, got %d", loginResp.StatusCode)
	}
}

func TestAccountSetAdmin_GrantsAndRevoke(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer createResp.Body.Close()
	var created accountResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	grantBody, err := json.Marshal(setAdminRequest{IsAdmin: true})
	if err != nil {
		t.Fatalf("marshal set admin request: %v", err)
	}
	grantReq, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/accounts/%d/admin", srv.URL, created.ID), bytes.NewReader(grantBody))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	grantReq.Header.Set("Authorization", "Bearer "+accessToken)
	grantReq.Header.Set("Content-Type", "application/json")

	grantResp, err := http.DefaultClient.Do(grantReq)
	if err != nil {
		t.Fatalf("PUT admin: %v", err)
	}
	defer grantResp.Body.Close()

	if grantResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", grantResp.StatusCode)
	}
	var granted accountResponse
	if err := json.NewDecoder(grantResp.Body).Decode(&granted); err != nil {
		t.Fatalf("decode grant response: %v", err)
	}
	if !granted.IsAdmin {
		t.Fatalf("expected alice to be an admin after granting")
	}
}

func TestAccountSetAdmin_RefusesToDemoteTheLastRemainingAdmin(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	// The bootstrapped admin has id 1 and is the sole admin — demoting it
	// must be refused (ADR-0037).
	body, err := json.Marshal(setAdminRequest{IsAdmin: false})
	if err != nil {
		t.Fatalf("marshal set admin request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/accounts/1/admin", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT admin: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}
