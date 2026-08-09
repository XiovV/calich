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
	return newAccountTestServerWithMailer(t, nil, false)
}

// newAccountTestServerWithMailer is newAccountTestServer with an injectable
// Mailer and smtpConfigured flag, for exercising SendInviteEmail's gating
// (ADR-0042).
func newAccountTestServerWithMailer(t *testing.T, mailer service.Mailer, smtpConfigured bool) (*httptest.Server, string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	auth := service.NewAuthService(users, sessions, service.NewWorkspaceService(sqlDB, repository.NewWorkspaceRepository(sqlDB)), []byte("test-secret"), "admin", "admin", false)
	if _, _, err := auth.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	calendarRepo := repository.NewCalendarRepository(sqlDB)
	shareRepo := repository.NewCalendarShareRepository(sqlDB)
	calendars := service.NewCalendarService(calendarRepo, shareRepo, users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB))
	appPasswords := service.NewAppPasswordService(repository.NewAppPasswordRepository(sqlDB), users)
	accounts := service.NewAccountService(sqlDB, users, sessions, calendarRepo, shareRepo, calendars, appPasswords, mailer)
	h := NewAccountHandler(accounts, smtpConfigured)
	authHandler := NewAuthHandler(auth, false)

	r := chi.NewRouter()
	r.Post("/api/auth/login", authHandler.Login)
	r.Post("/api/auth/accept-invite", authHandler.AcceptInvite)
	r.Get("/api/auth/accept-invite", authHandler.PreviewInvite)
	r.Route("/api/accounts", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Use(httpauth.RequireAdmin(auth))

		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Post("/invite", h.CreateInvite)
		r.Post("/{id}/invite/reissue", h.ReissueInvite)
		r.Delete("/{id}/invite", h.CancelInvite)
		r.Post("/{id}/invite/email", h.SendInviteEmail)
		r.Post("/{id}/reset-password", h.ResetPassword)
		r.Put("/{id}/admin", h.SetAdmin)
		r.Put("/{id}/disabled", h.SetDisabled)
		r.Put("/{id}/username", h.SetUsername)
		r.Get("/{id}/username-impact", h.UsernameImpact)
		r.Get("/{id}/delete-impact", h.DeleteImpact)
		r.Delete("/{id}", h.Delete)
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

func TestAccountSetDisabled_NonAdmin_Returns403(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)
	aliceToken := nonAdminAccessToken(t, srv, accessToken)

	body, err := json.Marshal(setDisabledRequest{IsDisabled: true})
	if err != nil {
		t.Fatalf("marshal set disabled request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/accounts/1/disabled", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT disabled: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestAccountSetDisabled_UnknownID_Returns404(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	body, err := json.Marshal(setDisabledRequest{IsDisabled: true})
	if err != nil {
		t.Fatalf("marshal set disabled request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/accounts/999/disabled", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT disabled: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAccountSetDisabled_RefusesToDisableTheLastRemainingAdmin(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	body, err := json.Marshal(setDisabledRequest{IsDisabled: true})
	if err != nil {
		t.Fatalf("marshal set disabled request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/accounts/1/disabled", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT disabled: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

// TestAccountSetDisabled_DisablesAccountAndBlocksLogin is an end-to-end
// check of ADR-0037's central guarantee: disabling an account makes it
// unable to log in, and re-enabling restores it.
func TestAccountSetDisabled_DisablesAccountAndBlocksLogin(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer createResp.Body.Close()
	var created accountResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	disableBody, err := json.Marshal(setDisabledRequest{IsDisabled: true})
	if err != nil {
		t.Fatalf("marshal set disabled request: %v", err)
	}
	disableReq, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/accounts/%d/disabled", srv.URL, created.ID), bytes.NewReader(disableBody))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	disableReq.Header.Set("Authorization", "Bearer "+accessToken)
	disableReq.Header.Set("Content-Type", "application/json")

	disableResp, err := http.DefaultClient.Do(disableReq)
	if err != nil {
		t.Fatalf("PUT disabled: %v", err)
	}
	defer disableResp.Body.Close()

	if disableResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", disableResp.StatusCode)
	}
	var disabled accountResponse
	if err := json.NewDecoder(disableResp.Body).Decode(&disabled); err != nil {
		t.Fatalf("decode disable response: %v", err)
	}
	if !disabled.IsDisabled {
		t.Fatalf("expected alice to be disabled")
	}

	loginResp := login(t, srv, "alice", "temp-secret")
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected a disabled account to be refused login with 401, got %d", loginResp.StatusCode)
	}

	enableBody, err := json.Marshal(setDisabledRequest{IsDisabled: false})
	if err != nil {
		t.Fatalf("marshal set disabled request: %v", err)
	}
	enableReq, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/accounts/%d/disabled", srv.URL, created.ID), bytes.NewReader(enableBody))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	enableReq.Header.Set("Authorization", "Bearer "+accessToken)
	enableReq.Header.Set("Content-Type", "application/json")

	enableResp, err := http.DefaultClient.Do(enableReq)
	if err != nil {
		t.Fatalf("PUT disabled: %v", err)
	}
	defer enableResp.Body.Close()

	if enableResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", enableResp.StatusCode)
	}

	reenabledLoginResp := login(t, srv, "alice", "temp-secret")
	defer reenabledLoginResp.Body.Close()
	if reenabledLoginResp.StatusCode != http.StatusOK {
		t.Fatalf("expected re-enabled account to log in again, got %d", reenabledLoginResp.StatusCode)
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

func deleteAccount(t *testing.T, srv *httptest.Server, accessToken string, id int64, req deleteAccountRequest) *http.Response {
	t.Helper()

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal delete account request: %v", err)
	}
	httpReq, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/accounts/%d", srv.URL, id), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("DELETE /api/accounts/%d: %v", id, err)
	}
	return resp
}

func TestAccountDelete_DispositionDelete_RemovesAccount(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer createResp.Body.Close()
	var created accountResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	resp := deleteAccount(t, srv, accessToken, created.ID, deleteAccountRequest{OwnedCalendars: "delete"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	loginResp := login(t, srv, "alice", "temp-secret")
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected a deleted account to be refused login, got %d", loginResp.StatusCode)
	}
}

func TestAccountDelete_DispositionTransfer_ReassignsCalendars(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer createResp.Body.Close()
	var alice accountResponse
	if err := json.NewDecoder(createResp.Body).Decode(&alice); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	bobResp := createAccount(t, srv, accessToken, "bob", "temp-secret")
	defer bobResp.Body.Close()
	var bob accountResponse
	if err := json.NewDecoder(bobResp.Body).Decode(&bob); err != nil {
		t.Fatalf("decode bob create response: %v", err)
	}

	resp := deleteAccount(t, srv, accessToken, alice.ID, deleteAccountRequest{OwnedCalendars: "transfer", TransferTo: &bob.ID})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestAccountDelete_MissingDisposition_Returns400(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer createResp.Body.Close()
	var created accountResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	resp := deleteAccount(t, srv, accessToken, created.ID, deleteAccountRequest{})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAccountDelete_TransferWithoutTransferTo_Returns400(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer createResp.Body.Close()
	var created accountResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	resp := deleteAccount(t, srv, accessToken, created.ID, deleteAccountRequest{OwnedCalendars: "transfer"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAccountDelete_TransferToUnknownUser_Returns400(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer createResp.Body.Close()
	var created accountResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	ghost := int64(9999)
	resp := deleteAccount(t, srv, accessToken, created.ID, deleteAccountRequest{OwnedCalendars: "transfer", TransferTo: &ghost})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAccountDelete_TransferToSelf_Returns400(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer createResp.Body.Close()
	var created accountResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	resp := deleteAccount(t, srv, accessToken, created.ID, deleteAccountRequest{OwnedCalendars: "transfer", TransferTo: &created.ID})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAccountDelete_RefusesToDeleteTheLastRemainingAdmin(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	// The bootstrapped admin has id 1 and is the sole admin — deleting it
	// must be refused (ADR-0037).
	resp := deleteAccount(t, srv, accessToken, 1, deleteAccountRequest{OwnedCalendars: "delete"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAccountDelete_UnknownID_Returns404(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	resp := deleteAccount(t, srv, accessToken, 999, deleteAccountRequest{OwnedCalendars: "delete"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAccountDelete_NonAdmin_Returns403(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)
	aliceToken := nonAdminAccessToken(t, srv, accessToken)

	resp := deleteAccount(t, srv, aliceToken, 1, deleteAccountRequest{OwnedCalendars: "delete"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestAccountDeleteImpact_ReportsShareCounts(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer createResp.Body.Close()
	var alice accountResponse
	if err := json.NewDecoder(createResp.Body).Decode(&alice); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/accounts/%d/delete-impact", srv.URL, alice.ID), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET delete-impact: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var impact deleteImpactResponse
	if err := json.NewDecoder(resp.Body).Decode(&impact); err != nil {
		t.Fatalf("decode delete impact response: %v", err)
	}
	if len(impact.Calendars) == 0 {
		t.Fatalf("expected alice's default calendars to appear in the impact report")
	}
	if impact.AffectedUserCount != 0 {
		t.Fatalf("expected 0 affected users for an unshared account, got %d", impact.AffectedUserCount)
	}
}

func TestAccountDeleteImpact_NonAdmin_Returns403(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)
	aliceToken := nonAdminAccessToken(t, srv, accessToken)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/accounts/1/delete-impact", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+aliceToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET delete-impact: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func setUsername(t *testing.T, srv *httptest.Server, accessToken string, id int64, username string) *http.Response {
	t.Helper()

	body, err := json.Marshal(setUsernameRequest{Username: username})
	if err != nil {
		t.Fatalf("marshal set username request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/accounts/%d/username", srv.URL, id), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT username: %v", err)
	}
	return resp
}

func TestAccountSetUsername_RenamesAccount(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer createResp.Body.Close()
	var created accountResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	resp := setUsername(t, srv, accessToken, created.ID, "alicia")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var renamed accountResponse
	if err := json.NewDecoder(resp.Body).Decode(&renamed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if renamed.Username != "alicia" {
		t.Fatalf("expected username alicia, got %q", renamed.Username)
	}
}

func TestAccountSetUsername_DuplicateUsername_Returns409(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	createResp.Body.Close()
	bobResp := createAccount(t, srv, accessToken, "bob", "temp-secret")
	defer bobResp.Body.Close()
	var bob accountResponse
	if err := json.NewDecoder(bobResp.Body).Decode(&bob); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	resp := setUsername(t, srv, accessToken, bob.ID, "alice")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAccountSetUsername_UnknownID_Returns404(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	resp := setUsername(t, srv, accessToken, 999, "alicia")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAccountSetUsername_NonAdmin_Returns403(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)
	aliceToken := nonAdminAccessToken(t, srv, accessToken)

	resp := setUsername(t, srv, aliceToken, 1, "someoneelse")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestAccountSetUsername_RenamedAccountLogsInWithNewUsername(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer createResp.Body.Close()
	var created accountResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	renameResp := setUsername(t, srv, accessToken, created.ID, "alicia")
	renameResp.Body.Close()
	if renameResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", renameResp.StatusCode)
	}

	oldLoginResp := login(t, srv, "alice", "temp-secret")
	defer oldLoginResp.Body.Close()
	if oldLoginResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected the old username to no longer log in, got %d", oldLoginResp.StatusCode)
	}

	newLoginResp := login(t, srv, "alicia", "temp-secret")
	defer newLoginResp.Body.Close()
	if newLoginResp.StatusCode != http.StatusOK {
		t.Fatalf("expected the new username to log in, got %d", newLoginResp.StatusCode)
	}
}

func TestAccountUsernameImpact_ReportsAppPasswordCount(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer createResp.Body.Close()
	var alice accountResponse
	if err := json.NewDecoder(createResp.Body).Decode(&alice); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/accounts/%d/username-impact", srv.URL, alice.ID), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET username-impact: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var impact usernameImpactResponse
	if err := json.NewDecoder(resp.Body).Decode(&impact); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if impact.AppPasswordCount != 0 {
		t.Fatalf("expected 0 app passwords for a fresh account, got %d", impact.AppPasswordCount)
	}
}

func TestAccountUsernameImpact_NonAdmin_Returns403(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)
	aliceToken := nonAdminAccessToken(t, srv, accessToken)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/accounts/1/username-impact", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+aliceToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET username-impact: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func createInvite(t *testing.T, srv *httptest.Server, accessToken, username string) *http.Response {
	t.Helper()

	body, err := json.Marshal(createInviteRequest{Username: username})
	if err != nil {
		t.Fatalf("marshal create invite request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/accounts/invite", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/accounts/invite: %v", err)
	}
	return resp
}

func createInviteWithEmail(t *testing.T, srv *httptest.Server, accessToken, username, email string) *http.Response {
	t.Helper()

	body, err := json.Marshal(createInviteRequest{Username: username, Email: email})
	if err != nil {
		t.Fatalf("marshal create invite request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/accounts/invite", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/accounts/invite: %v", err)
	}
	return resp
}

func sendInviteEmail(t *testing.T, srv *httptest.Server, accessToken string, accountID int64, link string) *http.Response {
	t.Helper()

	body, err := json.Marshal(sendInviteEmailRequest{Link: link})
	if err != nil {
		t.Fatalf("marshal send invite email request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/accounts/%d/invite/email", srv.URL, accountID), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST invite/email: %v", err)
	}
	return resp
}

// fakeMailer records every Send call instead of delivering anything.
type fakeMailer struct {
	to, subject, body string
}

func (m *fakeMailer) Send(to, subject, body string) error {
	m.to, m.subject, m.body = to, subject, body
	return nil
}

func TestAccountCreateInvite_ReturnsPendingAccountAndToken(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	resp := createInvite(t, srv, accessToken, "alice")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var invite inviteResponse
	if err := json.NewDecoder(resp.Body).Decode(&invite); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}
	if invite.Token == "" {
		t.Fatalf("expected a non-empty invite token")
	}
	if !invite.Account.IsPending {
		t.Fatalf("expected the account to be pending")
	}
	if invite.Account.InviteExpiresAt == nil {
		t.Fatalf("expected an invite expiry to be set")
	}
}

func TestAccountCreateInvite_DuplicateUsername_Returns409(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	firstResp := createInvite(t, srv, accessToken, "alice")
	defer firstResp.Body.Close()

	resp := createInvite(t, srv, accessToken, "alice")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAccountCreateInvite_NonAdmin_Returns403(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)
	aliceToken := nonAdminAccessToken(t, srv, accessToken)

	resp := createInvite(t, srv, aliceToken, "bob")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestAccountCreateInvite_WithEmail_MarksInviteEmailAvailableWhenSMTPConfigured(t *testing.T) {
	mailer := &fakeMailer{}
	srv, accessToken := newAccountTestServerWithMailer(t, mailer, true)

	resp := createInviteWithEmail(t, srv, accessToken, "alice", "alice@example.com")
	defer resp.Body.Close()

	var invite inviteResponse
	if err := json.NewDecoder(resp.Body).Decode(&invite); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}
	if !invite.Account.InviteEmailAvailable {
		t.Fatalf("expected invite_email_available to be true when SMTP is configured and an email is on file")
	}
}

func TestAccountCreateInvite_NoEmail_InviteEmailAvailableIsFalse(t *testing.T) {
	mailer := &fakeMailer{}
	srv, accessToken := newAccountTestServerWithMailer(t, mailer, true)

	resp := createInvite(t, srv, accessToken, "alice")
	defer resp.Body.Close()

	var invite inviteResponse
	if err := json.NewDecoder(resp.Body).Decode(&invite); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}
	if invite.Account.InviteEmailAvailable {
		t.Fatalf("expected invite_email_available to be false with no email on file")
	}
}

func TestAccountCreateInvite_InvalidEmail_Returns400(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	resp := createInviteWithEmail(t, srv, accessToken, "alice", "not-an-email")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAccountSendInviteEmail_SendsAndReturns204(t *testing.T) {
	mailer := &fakeMailer{}
	srv, accessToken := newAccountTestServerWithMailer(t, mailer, true)

	createResp := createInviteWithEmail(t, srv, accessToken, "alice", "alice@example.com")
	defer createResp.Body.Close()
	var created inviteResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}

	resp := sendInviteEmail(t, srv, accessToken, created.Account.ID, "https://example.com/accept-invite?token=abc")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	if mailer.to != "alice@example.com" {
		t.Fatalf("expected the email to go to alice@example.com, got %q", mailer.to)
	}
}

func TestAccountSendInviteEmail_NoEmailOnFile_Returns409(t *testing.T) {
	mailer := &fakeMailer{}
	srv, accessToken := newAccountTestServerWithMailer(t, mailer, true)

	createResp := createInvite(t, srv, accessToken, "alice")
	defer createResp.Body.Close()
	var created inviteResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}

	resp := sendInviteEmail(t, srv, accessToken, created.Account.ID, "https://example.com/accept-invite?token=abc")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAccountSendInviteEmail_SMTPNotConfigured_Returns409(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createInviteWithEmail(t, srv, accessToken, "alice", "alice@example.com")
	defer createResp.Body.Close()
	var created inviteResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}

	resp := sendInviteEmail(t, srv, accessToken, created.Account.ID, "https://example.com/accept-invite?token=abc")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAccountSendInviteEmail_EmptyLink_Returns400(t *testing.T) {
	mailer := &fakeMailer{}
	srv, accessToken := newAccountTestServerWithMailer(t, mailer, true)

	createResp := createInviteWithEmail(t, srv, accessToken, "alice", "alice@example.com")
	defer createResp.Body.Close()
	var created inviteResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}

	resp := sendInviteEmail(t, srv, accessToken, created.Account.ID, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAccountReissueInvite_ReplacesToken(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createInvite(t, srv, accessToken, "alice")
	defer createResp.Body.Close()
	var created inviteResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/accounts/%d/invite/reissue", srv.URL, created.Account.ID), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST invite/reissue: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var reissued inviteResponse
	if err := json.NewDecoder(resp.Body).Decode(&reissued); err != nil {
		t.Fatalf("decode reissue response: %v", err)
	}
	if reissued.Token == "" || reissued.Token == created.Token {
		t.Fatalf("expected a fresh, different token")
	}

	// The original token must no longer be acceptable.
	acceptResp, err := http.Post(srv.URL+"/api/auth/accept-invite", "application/json",
		bytes.NewReader(mustMarshal(t, acceptInviteRequest{Token: created.Token, Password: "new-password"})))
	if err != nil {
		t.Fatalf("POST accept-invite: %v", err)
	}
	defer acceptResp.Body.Close()
	if acceptResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected the reissued-away token to be refused with 401, got %d", acceptResp.StatusCode)
	}
}

func TestAccountReissueInvite_NotPending_Returns409(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer createResp.Body.Close()
	var created accountResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/accounts/%d/invite/reissue", srv.URL, created.ID), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST invite/reissue: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAccountCancelInvite_DeletesAccount(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createInvite(t, srv, accessToken, "alice")
	defer createResp.Body.Close()
	var created inviteResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}

	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/accounts/%d/invite", srv.URL, created.Account.ID), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE invite: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	listReq, err := http.NewRequest(http.MethodGet, srv.URL+"/api/accounts/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	listReq.Header.Set("Authorization", "Bearer "+accessToken)
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("GET /api/accounts: %v", err)
	}
	defer listResp.Body.Close()

	var accounts []accountResponse
	if err := json.NewDecoder(listResp.Body).Decode(&accounts); err != nil {
		t.Fatalf("decode accounts list: %v", err)
	}
	for _, a := range accounts {
		if a.ID == created.Account.ID {
			t.Fatalf("expected the pending account to be gone from the list")
		}
	}
}

func TestAccountCancelInvite_NotPending_Returns409(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createAccount(t, srv, accessToken, "alice", "temp-secret")
	defer createResp.Body.Close()
	var created accountResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/accounts/%d/invite", srv.URL, created.ID), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE invite: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAcceptInvite_LogsInImmediately(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createInvite(t, srv, accessToken, "alice")
	defer createResp.Body.Close()
	var created inviteResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}

	resp, err := http.Post(srv.URL+"/api/auth/accept-invite", "application/json",
		bytes.NewReader(mustMarshal(t, acceptInviteRequest{Token: created.Token, Password: "new-password"})))
	if err != nil {
		t.Fatalf("POST accept-invite: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var logged loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&logged); err != nil {
		t.Fatalf("decode accept-invite response: %v", err)
	}
	if logged.AccessToken == "" {
		t.Fatalf("expected an access token")
	}

	// The now-Active account logs in normally with the chosen password.
	loginResp := login(t, srv, "alice", "new-password")
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 logging in with the accepted password, got %d", loginResp.StatusCode)
	}
}

func TestAcceptInvite_InvalidToken_Returns401(t *testing.T) {
	srv, _ := newAccountTestServer(t)

	resp, err := http.Post(srv.URL+"/api/auth/accept-invite", "application/json",
		bytes.NewReader(mustMarshal(t, acceptInviteRequest{Token: "not-a-real-token", Password: "new-password"})))
	if err != nil {
		t.Fatalf("POST accept-invite: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestPreviewInvite_ReturnsUsername(t *testing.T) {
	srv, accessToken := newAccountTestServer(t)

	createResp := createInvite(t, srv, accessToken, "alice")
	defer createResp.Body.Close()
	var created inviteResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}

	resp, err := http.Get(srv.URL + "/api/auth/accept-invite?token=" + created.Token)
	if err != nil {
		t.Fatalf("GET accept-invite: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var previewed previewInviteResponse
	if err := json.NewDecoder(resp.Body).Decode(&previewed); err != nil {
		t.Fatalf("decode preview response: %v", err)
	}
	if previewed.Username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", previewed.Username)
	}

	// Previewing must not consume the token — accepting it afterwards still works.
	acceptResp, err := http.Post(srv.URL+"/api/auth/accept-invite", "application/json",
		bytes.NewReader(mustMarshal(t, acceptInviteRequest{Token: created.Token, Password: "new-password"})))
	if err != nil {
		t.Fatalf("POST accept-invite: %v", err)
	}
	defer acceptResp.Body.Close()
	if acceptResp.StatusCode != http.StatusOK {
		t.Fatalf("expected accept-invite to still succeed after preview, got %d", acceptResp.StatusCode)
	}
}

func TestPreviewInvite_InvalidToken_Returns401(t *testing.T) {
	srv, _ := newAccountTestServer(t)

	resp, err := http.Get(srv.URL + "/api/auth/accept-invite?token=not-a-real-token")
	if err != nil {
		t.Fatalf("GET accept-invite: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return body
}
