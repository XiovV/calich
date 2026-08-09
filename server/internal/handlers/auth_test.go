package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

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
	auth := service.NewAuthService(users, sessions, service.NewWorkspaceService(sqlDB, repository.NewWorkspaceRepository(sqlDB), repository.NewWorkspaceInviteRepository(sqlDB)), repository.NewWorkspaceInviteRepository(sqlDB), []byte("test-secret"), "", "", false)

	// A User requiring a password change (ADR-0037's must_change_password
	// gate) is seeded directly rather than via Bootstrap, which no longer
	// produces one — ADR-0044 retired Bootstrap's fixed admin/admin fallback,
	// the only path that used to.
	mustSeedUserRequiringPasswordChange(t, users, "admin", "admin")

	h := NewAuthHandler(auth, smtpConfigured)

	r := chi.NewRouter()
	r.Post("/api/auth/login", h.Login)
	r.Post("/api/auth/refresh", h.Refresh)
	r.Post("/api/auth/logout", h.Logout)
	r.With(httpauth.RequireAuth(auth)).Post("/api/auth/change-password", h.ChangePassword)
	r.With(httpauth.RequireAuth(auth), httpauth.RequireActiveUser(auth), httpauth.RequireEnabledUser(auth)).Get("/api/auth/me", h.Me)
	r.With(httpauth.RequireAuth(auth), httpauth.RequireActiveUser(auth), httpauth.RequireEnabledUser(auth)).Put("/api/auth/email", h.UpdateEmail)
	r.With(httpauth.RequireAuth(auth), httpauth.RequireActiveUser(auth), httpauth.RequireEnabledUser(auth)).Put("/api/auth/username", h.UpdateUsername)
	r.With(httpauth.RequireAuth(auth), httpauth.RequireActiveUser(auth), httpauth.RequireEnabledUser(auth)).Put("/api/auth/synced-device-reminders", h.UpdateSyncedDeviceReminders)
	r.With(httpauth.RequireAuth(auth), httpauth.RequireActiveUser(auth), httpauth.RequireEnabledUser(auth)).Patch("/api/auth/preferences", h.UpdatePreferences)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// mustSeedUserRequiringPasswordChange inserts a User directly via the
// repository with must_change_password set, standing in for what Bootstrap
// used to produce unconditionally via its now-retired fixed admin/admin
// fallback (ADR-0044) — a User forced to change a temporary password, the
// shape most of this file's fixture-only setup still needs.
func mustSeedUserRequiringPasswordChange(t *testing.T, users *repository.UserRepository, username, password string) repository.User {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	user, err := users.Create(context.Background(), username, string(hash), true)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	return user
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

// mustLoginAccessToken decodes a login response body for its access token.
// The response's Body must not have been read yet.
func mustLoginAccessToken(t *testing.T, loginResp *http.Response) string {
	t.Helper()

	var loggedIn loginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&loggedIn); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	return loggedIn.AccessToken
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

// TestLogin_DisabledAccount_SucceedsButMeIsBlocked covers ADR-0044: a
// Disabled User can still log in (there's no instance-wide Admin left to
// reactivate them for), but every route except the self-service
// account-lifecycle ones is closed off — /api/auth/me included.
func TestLogin_DisabledAccount_SucceedsButMeIsBlocked(t *testing.T) {
	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	auth := service.NewAuthService(users, sessions, service.NewWorkspaceService(sqlDB, repository.NewWorkspaceRepository(sqlDB), repository.NewWorkspaceInviteRepository(sqlDB)), repository.NewWorkspaceInviteRepository(sqlDB), []byte("test-secret"), "", "", false)

	hash, err := bcrypt.GenerateFromPassword([]byte("hunter2"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user, err := users.Create(context.Background(), "alice", string(hash), false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := users.SetDisabled(context.Background(), user.ID, true); err != nil {
		t.Fatalf("disable user: %v", err)
	}

	h := NewAuthHandler(auth, false)
	r := chi.NewRouter()
	r.Post("/api/auth/login", h.Login)
	r.With(httpauth.RequireAuth(auth), httpauth.RequireActiveUser(auth), httpauth.RequireEnabledUser(auth)).Get("/api/auth/me", h.Me)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	loginResp := login(t, srv, "alice", "hunter2")
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("expected login to still succeed for a disabled account, got %d", loginResp.StatusCode)
	}
	var logged loginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&logged); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if !logged.IsDisabled {
		t.Fatalf("expected the login response to report is_disabled")
	}

	meReq, err := http.NewRequest(http.MethodGet, srv.URL+"/api/auth/me", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	meReq.Header.Set("Authorization", "Bearer "+logged.AccessToken)
	meResp, err := http.DefaultClient.Do(meReq)
	if err != nil {
		t.Fatalf("GET /api/auth/me: %v", err)
	}
	defer meResp.Body.Close()
	if meResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for a disabled account, got %d", meResp.StatusCode)
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
	if changeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from change-password, got %d", changeResp.StatusCode)
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
	if changeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from change-password, got %d", changeResp.StatusCode)
	}

	var changed changePasswordResponse
	if err := json.NewDecoder(changeResp.Body).Decode(&changed); err != nil {
		t.Fatalf("decode change-password response: %v", err)
	}

	return changed.AccessToken
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

func TestUpdateUsername_RenamesAndLoginWorksWithTheNewUsername(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updateUsernameRequest{Username: "newadmin"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/auth/username", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/auth/username: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.Username != "newadmin" {
		t.Fatalf("expected username newadmin, got %q", me.Username)
	}

	oldLoginResp := login(t, srv, "admin", "a-new-password")
	defer oldLoginResp.Body.Close()
	if oldLoginResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected the old username to no longer log in, got %d", oldLoginResp.StatusCode)
	}

	newLoginResp := login(t, srv, "newadmin", "a-new-password")
	defer newLoginResp.Body.Close()
	if newLoginResp.StatusCode != http.StatusOK {
		t.Fatalf("expected the new username to log in, got %d", newLoginResp.StatusCode)
	}
}

func TestUpdateUsername_RejectsAColon(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updateUsernameRequest{Username: "ad:min"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/auth/username", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/auth/username: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUpdateUsername_DuplicateUsername_Returns409(t *testing.T) {
	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	auth := service.NewAuthService(users, sessions, service.NewWorkspaceService(sqlDB, repository.NewWorkspaceRepository(sqlDB), repository.NewWorkspaceInviteRepository(sqlDB)), repository.NewWorkspaceInviteRepository(sqlDB), []byte("test-secret"), "admin", "admin", false)
	bootstrapUser, _, err := auth.Bootstrap(context.Background())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := users.Create(context.Background(), "bob", "hash", false); err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if _, err := auth.ChangePassword(context.Background(), bootstrapUser.ID, "admin", "a-new-password"); err != nil {
		t.Fatalf("change password: %v", err)
	}

	h := NewAuthHandler(auth, false)
	r := chi.NewRouter()
	r.Post("/api/auth/login", h.Login)
	r.With(httpauth.RequireAuth(auth), httpauth.RequireActiveUser(auth)).Put("/api/auth/username", h.UpdateUsername)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	loginResp := login(t, srv, "admin", "a-new-password")
	defer loginResp.Body.Close()
	accessToken := mustLoginAccessToken(t, loginResp)

	body, _ := json.Marshal(updateUsernameRequest{Username: "bob"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/auth/username", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/auth/username: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
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

func TestMe_WeekStart_DefaultsToMonday(t *testing.T) {
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
	if me.WeekStart != 1 {
		t.Fatalf("expected week_start to default to 1 (Monday), got %d", me.WeekStart)
	}
}

func TestUpdatePreferences_SetsWeekStart(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updatePreferencesRequest{WeekStart: intPtr(0)})
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// week_start: 0 is Sunday, not "unset" — it must actually be stored.
	if me.WeekStart != 0 {
		t.Fatalf("expected week_start to be stored as 0 (Sunday), got %d", me.WeekStart)
	}
}

func TestUpdatePreferences_OmittedFieldIsLeftUntouched(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.WeekStart != 1 {
		t.Fatalf("expected week_start to stay at its default of 1, got %d", me.WeekStart)
	}
}

func TestUpdatePreferences_RejectsWeekStartOutOfRange(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updatePreferencesRequest{WeekStart: intPtr(7)})
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/auth/me", nil)
	req2.Header.Set("Authorization", "Bearer "+accessToken)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("GET /api/auth/me: %v", err)
	}
	defer resp2.Body.Close()

	var me meResponse
	if err := json.NewDecoder(resp2.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.WeekStart != 1 {
		t.Fatalf("expected an out-of-range week_start to store nothing, got %d", me.WeekStart)
	}
}

func TestUpdatePreferences_SetsDefaultView(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updatePreferencesRequest{DefaultView: strPtr("month")})
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.DefaultView != "month" {
		t.Fatalf("expected default_view to be stored as \"month\", got %q", me.DefaultView)
	}
}

func TestUpdatePreferences_RejectsInvalidDefaultView(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updatePreferencesRequest{DefaultView: strPtr("fortnight")})
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/auth/me", nil)
	req2.Header.Set("Authorization", "Bearer "+accessToken)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("GET /api/auth/me: %v", err)
	}
	defer resp2.Body.Close()

	var me meResponse
	if err := json.NewDecoder(resp2.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.DefaultView != "week" {
		t.Fatalf("expected an invalid default_view to store nothing, got %q", me.DefaultView)
	}
}

func TestMe_TimeFormat_DefaultsTo24h(t *testing.T) {
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
	if me.TimeFormat != "24h" {
		t.Fatalf("expected time_format to default to \"24h\", got %q", me.TimeFormat)
	}
}

func TestUpdatePreferences_SetsTimeFormat(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updatePreferencesRequest{TimeFormat: strPtr("12h")})
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.TimeFormat != "12h" {
		t.Fatalf("expected time_format to be stored as \"12h\", got %q", me.TimeFormat)
	}
}

func TestUpdatePreferences_RejectsInvalidTimeFormat(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updatePreferencesRequest{TimeFormat: strPtr("36h")})
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/auth/me", nil)
	req2.Header.Set("Authorization", "Bearer "+accessToken)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("GET /api/auth/me: %v", err)
	}
	defer resp2.Body.Close()

	var me meResponse
	if err := json.NewDecoder(resp2.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.TimeFormat != "24h" {
		t.Fatalf("expected an invalid time_format to store nothing, got %q", me.TimeFormat)
	}
}

func TestUpdatePreferences_SetsWorkingHours(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updatePreferencesRequest{
		WorkingHoursStart: intPtr(9),
		WorkingHoursEnd:   intPtr(17),
	})
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.WorkingHoursStart == nil || *me.WorkingHoursStart != 9 {
		t.Fatalf("expected working_hours_start to be stored as 9, got %+v", me.WorkingHoursStart)
	}
	if me.WorkingHoursEnd == nil || *me.WorkingHoursEnd != 17 {
		t.Fatalf("expected working_hours_end to be stored as 17, got %+v", me.WorkingHoursEnd)
	}
}

func TestUpdatePreferences_SetsWorkingHoursToMinuteOfDayPrecision(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updatePreferencesRequest{
		WorkingHoursStart: intPtr(510),  // 08:30
		WorkingHoursEnd:   intPtr(1020), // 17:00
	})
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.WorkingHoursStart == nil || *me.WorkingHoursStart != 510 {
		t.Fatalf("expected working_hours_start to be stored as 510, got %+v", me.WorkingHoursStart)
	}
	if me.WorkingHoursEnd == nil || *me.WorkingHoursEnd != 1020 {
		t.Fatalf("expected working_hours_end to be stored as 1020, got %+v", me.WorkingHoursEnd)
	}
}

func TestUpdatePreferences_RejectsWorkingHoursOutOfMinuteOfDayRange(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updatePreferencesRequest{
		WorkingHoursStart: intPtr(0),
		WorkingHoursEnd:   intPtr(1440),
	})
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUpdatePreferences_ClearsWorkingHoursWithBothNull(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	setBody, _ := json.Marshal(updatePreferencesRequest{
		WorkingHoursStart: intPtr(9),
		WorkingHoursEnd:   intPtr(17),
	})
	setReq, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader(setBody))
	setReq.Header.Set("Authorization", "Bearer "+accessToken)
	setReq.Header.Set("Content-Type", "application/json")
	setResp, err := http.DefaultClient.Do(setReq)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences (set): %v", err)
	}
	setResp.Body.Close()

	clearReq, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader([]byte(`{"working_hours_start": null, "working_hours_end": null}`)))
	clearReq.Header.Set("Authorization", "Bearer "+accessToken)
	clearReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(clearReq)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences (clear): %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.WorkingHoursStart != nil || me.WorkingHoursEnd != nil {
		t.Fatalf("expected working hours to be cleared, got start=%+v end=%+v", me.WorkingHoursStart, me.WorkingHoursEnd)
	}
}

func TestUpdatePreferences_RejectsWorkingHoursStartNotBeforeEnd(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updatePreferencesRequest{
		WorkingHoursStart: intPtr(17),
		WorkingHoursEnd:   intPtr(9),
	})
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/auth/me", nil)
	req2.Header.Set("Authorization", "Bearer "+accessToken)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("GET /api/auth/me: %v", err)
	}
	defer resp2.Body.Close()

	var me meResponse
	if err := json.NewDecoder(resp2.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.WorkingHoursStart != nil || me.WorkingHoursEnd != nil {
		t.Fatalf("expected start >= end to store nothing, got start=%+v end=%+v", me.WorkingHoursStart, me.WorkingHoursEnd)
	}
}

func TestUpdatePreferences_RejectsWorkingHoursOneBoundNull(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader([]byte(`{"working_hours_start": 9, "working_hours_end": null}`)))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/auth/me", nil)
	req2.Header.Set("Authorization", "Bearer "+accessToken)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("GET /api/auth/me: %v", err)
	}
	defer resp2.Body.Close()

	var me meResponse
	if err := json.NewDecoder(resp2.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.WorkingHoursStart != nil || me.WorkingHoursEnd != nil {
		t.Fatalf("expected one bound set / other null to store nothing, got start=%+v end=%+v", me.WorkingHoursStart, me.WorkingHoursEnd)
	}
}

func TestUpdatePreferences_OmittedWorkingHoursFieldsAreLeftUntouched(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	setBody, _ := json.Marshal(updatePreferencesRequest{
		WorkingHoursStart: intPtr(9),
		WorkingHoursEnd:   intPtr(17),
	})
	setReq, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader(setBody))
	setReq.Header.Set("Authorization", "Bearer "+accessToken)
	setReq.Header.Set("Content-Type", "application/json")
	setResp, err := http.DefaultClient.Do(setReq)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences (set): %v", err)
	}
	setResp.Body.Close()

	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader([]byte(`{"week_start": 0}`)))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences: %v", err)
	}
	defer resp.Body.Close()

	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.WorkingHoursStart == nil || *me.WorkingHoursStart != 9 {
		t.Fatalf("expected working_hours_start to remain 9, got %+v", me.WorkingHoursStart)
	}
	if me.WorkingHoursEnd == nil || *me.WorkingHoursEnd != 17 {
		t.Fatalf("expected working_hours_end to remain 17, got %+v", me.WorkingHoursEnd)
	}
}

func TestUpdatePreferences_RequiresAuthentication(t *testing.T) {
	srv := newAuthTestServer(t)

	body, _ := json.Marshal(updatePreferencesRequest{WeekStart: intPtr(0)})
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/auth/preferences", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/auth/preferences: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func intPtr(v int) *int { return &v }

func strPtr(v string) *string { return &v }

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

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// TestChangePassword_ReissuesSessionInsteadOfOnlyClearingIt pins the fix for
// #123: changing your password must not log you out of the tab that made the
// request. The response has to carry a fresh refresh_token cookie and access
// token, and that new refresh token has to actually work.
func TestChangePassword_ReissuesSessionInsteadOfOnlyClearingIt(t *testing.T) {
	srv := newAuthTestServer(t)

	loginResp := login(t, srv, "admin", "admin")
	defer loginResp.Body.Close()
	oldCookie := refreshCookieFrom(t, loginResp)

	resp := changePassword(t, srv, mustLoginAccessToken(t, loginResp), "admin", "a-new-password")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body changePasswordResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.AccessToken == "" {
		t.Fatalf("expected a non-empty access token")
	}

	newCookie := refreshCookieFrom(t, resp)
	if newCookie.Value == oldCookie.Value {
		t.Fatalf("expected a freshly issued refresh_token cookie, got the pre-change one back")
	}

	refreshReq, err := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/refresh", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	refreshReq.AddCookie(newCookie)

	refreshResp, err := http.DefaultClient.Do(refreshReq)
	if err != nil {
		t.Fatalf("POST /api/auth/refresh: %v", err)
	}
	defer refreshResp.Body.Close()

	if refreshResp.StatusCode != http.StatusOK {
		t.Fatalf("expected the newly issued refresh_token cookie to work, got %d", refreshResp.StatusCode)
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

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
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
	if firstChange.StatusCode != http.StatusOK {
		t.Fatalf("expected first change-password to succeed with 200, got %d", firstChange.StatusCode)
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
