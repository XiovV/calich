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
	return newAuthTestServerWithSMTP(t, false)
}

func newAuthTestServerWithSMTP(t *testing.T, smtpConfigured bool) *httptest.Server {
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

	h := NewAuthHandler(auth, smtpConfigured)

	r := chi.NewRouter()
	r.Post("/api/auth/login", h.Login)
	r.Post("/api/auth/refresh", h.Refresh)
	r.Post("/api/auth/logout", h.Logout)
	r.With(httpauth.RequireAuth(auth)).Post("/api/auth/change-password", h.ChangePassword)
	r.With(httpauth.RequireAuth(auth), httpauth.RequireActiveUser(auth)).Get("/api/auth/me", h.Me)
	r.With(httpauth.RequireAuth(auth), httpauth.RequireActiveUser(auth)).Put("/api/auth/email", h.UpdateEmail)
	r.With(httpauth.RequireAuth(auth), httpauth.RequireActiveUser(auth)).Put("/api/auth/synced-device-reminders", h.UpdateSyncedDeviceReminders)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func changePassword(t *testing.T, srv *httptest.Server, accessToken, currentPassword, newPassword string) *http.Response {
	t.Helper()

	body, err := json.Marshal(changePasswordRequest{CurrentPassword: currentPassword, NewPassword: newPassword})
	if err != nil {
		t.Fatalf("marshal change password request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/change-password", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/auth/change-password: %v", err)
	}
	return resp
}

func refreshCookieFrom(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()

	for _, c := range resp.Cookies() {
		if c.Name == refreshCookieName {
			return c
		}
	}
	t.Fatalf("expected a refresh_token cookie in response")
	return nil
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

func TestMe_MustChangePassword_Returns403(t *testing.T) {
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

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for a user who still must change their password, got %d", resp.StatusCode)
	}
}

func TestMe_ValidToken_ReturnsUser_AfterPasswordChange(t *testing.T) {
	srv := newAuthTestServer(t)

	loginResp := login(t, srv, "admin", "admin")
	defer loginResp.Body.Close()
	var loggedIn loginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&loggedIn); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	changeResp := changePassword(t, srv, loggedIn.AccessToken, "admin", "a-new-password")
	defer changeResp.Body.Close()
	if changeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 from change-password, got %d", changeResp.StatusCode)
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
		t.Fatalf("expected 200 after password change, got %d", resp.StatusCode)
	}

	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.Username != "admin" {
		t.Fatalf("expected username 'admin', got %q", me.Username)
	}
	if me.MustChangePassword {
		t.Fatalf("expected must_change_password to be false after changing password")
	}
}

// authenticatedAccessToken logs in as the default bootstrap user, changes
// their password (required before anything else works, ADR-0010), and
// returns the resulting access token.
func authenticatedAccessToken(t *testing.T, srv *httptest.Server) string {
	t.Helper()

	loginResp := login(t, srv, "admin", "admin")
	defer loginResp.Body.Close()
	var loggedIn loginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&loggedIn); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	changeResp := changePassword(t, srv, loggedIn.AccessToken, "admin", "a-new-password")
	defer changeResp.Body.Close()
	if changeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 from change-password, got %d", changeResp.StatusCode)
	}

	return loggedIn.AccessToken
}

func TestMe_EmailReminderChannelAvailable_FalseWithNoEmailOrSMTP(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

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
	if me.Email != nil {
		t.Fatalf("expected no email set yet, got %+v", me.Email)
	}
	if me.EmailReminderChannelAvailable {
		t.Fatalf("expected the Email Channel to be unavailable with neither email nor SMTP configured")
	}
}

func TestUpdateEmail_SetsEmailAndReportsChannelAvailableOnceSMTPIsConfigured(t *testing.T) {
	srv := newAuthTestServerWithSMTP(t, true)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updateEmailRequest{Email: "admin@example.com"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/auth/email", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/auth/email: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.Email == nil || *me.Email != "admin@example.com" {
		t.Fatalf("expected email to be set, got %+v", me.Email)
	}
	if !me.EmailReminderChannelAvailable {
		t.Fatalf("expected the Email Channel to be available once email and SMTP are both configured")
	}
}

func TestUpdateEmail_RejectsAnInvalidAddress(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updateEmailRequest{Email: "not-an-email"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/auth/email", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/auth/email: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestMe_SyncedDeviceRemindersEnabled_DefaultsFalse(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

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
	if me.SyncedDeviceRemindersEnabled {
		t.Fatalf("expected synced device reminders to default off")
	}
}

func TestUpdateSyncedDeviceReminders_TogglesThePreference(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updateSyncedDeviceRemindersRequest{SyncedDeviceRemindersEnabled: true})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/auth/synced-device-reminders", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/auth/synced-device-reminders: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !me.SyncedDeviceRemindersEnabled {
		t.Fatalf("expected synced device reminders to be enabled")
	}
}

func TestUpdateSyncedDeviceReminders_RequiresAuthentication(t *testing.T) {
	srv := newAuthTestServer(t)

	body, _ := json.Marshal(updateSyncedDeviceRemindersRequest{SyncedDeviceRemindersEnabled: true})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/auth/synced-device-reminders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/auth/synced-device-reminders: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestChangePassword_AllowedEvenWhileMustChangePassword(t *testing.T) {
	// change-password itself must stay reachable for a user who is otherwise
	// blocked everywhere else — that's the whole point of this ticket.
	srv := newAuthTestServer(t)

	loginResp := login(t, srv, "admin", "admin")
	defer loginResp.Body.Close()
	var loggedIn loginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&loggedIn); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	resp := changePassword(t, srv, loggedIn.AccessToken, "admin", "a-new-password")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestChangePassword_SkipsCurrentPasswordCheckWhileMustChangePassword(t *testing.T) {
	srv := newAuthTestServer(t)

	loginResp := login(t, srv, "admin", "admin")
	defer loginResp.Body.Close()
	var loggedIn loginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&loggedIn); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	// The bootstrap default is a publicly documented value, so the forced
	// change doesn't need the (already-known) current password re-typed.
	resp := changePassword(t, srv, loggedIn.AccessToken, "this-is-not-the-current-password", "a-new-password")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestChangePassword_RequiresCurrentPasswordOnceAlreadyChanged(t *testing.T) {
	srv := newAuthTestServer(t)

	loginResp := login(t, srv, "admin", "admin")
	defer loginResp.Body.Close()
	var loggedIn loginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&loggedIn); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	firstChange := changePassword(t, srv, loggedIn.AccessToken, "admin", "first-new-password")
	defer firstChange.Body.Close()
	if firstChange.StatusCode != http.StatusNoContent {
		t.Fatalf("expected first change-password to succeed with 204, got %d", firstChange.StatusCode)
	}

	resp := changePassword(t, srv, loggedIn.AccessToken, "wrong-password", "second-new-password")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 once must_change_password is false, got %d", resp.StatusCode)
	}
}

func TestRefresh_ReturnsNewAccessTokenFromCookie(t *testing.T) {
	srv := newAuthTestServer(t)

	loginResp := login(t, srv, "admin", "admin")
	defer loginResp.Body.Close()
	cookie := refreshCookieFrom(t, loginResp)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/refresh", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.AddCookie(cookie)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/auth/refresh: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body refreshResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.AccessToken == "" {
		t.Fatalf("expected a non-empty access token")
	}
}

func TestRefresh_NoCookie_Returns401(t *testing.T) {
	srv := newAuthTestServer(t)

	resp, err := http.Post(srv.URL+"/api/auth/refresh", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/auth/refresh: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestLogout_ClearsCookieAndInvalidatesSession(t *testing.T) {
	srv := newAuthTestServer(t)

	loginResp := login(t, srv, "admin", "admin")
	defer loginResp.Body.Close()
	cookie := refreshCookieFrom(t, loginResp)

	logoutReq, err := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/logout", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	logoutReq.AddCookie(cookie)

	logoutResp, err := http.DefaultClient.Do(logoutReq)
	if err != nil {
		t.Fatalf("POST /api/auth/logout: %v", err)
	}
	defer logoutResp.Body.Close()

	if logoutResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", logoutResp.StatusCode)
	}

	cleared := refreshCookieFrom(t, logoutResp)
	if cleared.MaxAge >= 0 {
		t.Fatalf("expected logout to clear the refresh_token cookie (negative Max-Age), got %d", cleared.MaxAge)
	}

	// The old refresh token must no longer work after logout.
	refreshReq, err := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/refresh", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	refreshReq.AddCookie(cookie)

	refreshResp, err := http.DefaultClient.Do(refreshReq)
	if err != nil {
		t.Fatalf("POST /api/auth/refresh: %v", err)
	}
	defer refreshResp.Body.Close()

	if refreshResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 refreshing with a logged-out session, got %d", refreshResp.StatusCode)
	}
}
