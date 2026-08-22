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
	"golang.org/x/crypto/bcrypt"

	"github.com/XiovV/calich/server/internal/apptest"
	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/repository"
)

func newAuthTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newAuthTestServerWithMail(t, false, false)
}

func newAuthTestServerWithSMTP(t *testing.T, smtpConfigured bool) *httptest.Server {
	t.Helper()
	return newAuthTestServerWithMail(t, smtpConfigured, false)
}

func newAuthTestServerWithMail(t *testing.T, smtpConfigured, imapConfigured bool) *httptest.Server {
	t.Helper()
	return newAuthTestServerWithCookieSecure(t, smtpConfigured, imapConfigured, true)
}

func newAuthTestServerWithCookieSecure(t *testing.T, smtpConfigured, imapConfigured, cookieSecure bool) *httptest.Server {
	t.Helper()

	// No bootstrap account: these tests seed or register the Users they need.
	cfg := apptest.Config(t)
	cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword = "", "", ""
	g := newTestGraphWithConfig(t, cfg)

	users := g.UserRepo
	auth := g.Auth

	// A User requiring a password change (ADR-0037's must_change_password
	// gate) is seeded directly rather than via Bootstrap, which no longer
	// produces one — ADR-0044 retired Bootstrap's fixed admin/admin fallback,
	// the only path that used to.
	mustSeedUserRequiringPasswordChange(t, users, "admin", "admin")

	h := NewAuthHandler(auth, smtpConfigured, imapConfigured, cookieSecure)

	r := chi.NewRouter()
	r.Get("/api/auth/setup-status", h.SetupStatus)
	r.Post("/api/auth/login", h.Login)
	r.Post("/api/auth/refresh", h.Refresh)
	r.Post("/api/auth/logout", h.Logout)
	r.With(httpauth.RequireAuth(auth)).Post("/api/auth/change-password", h.ChangePassword)
	r.With(httpauth.RequireAuth(auth), httpauth.RequireActiveUser(auth), httpauth.RequireEnabledUser(auth)).Get("/api/auth/me", h.Me)
	r.With(httpauth.RequireAuth(auth), httpauth.RequireActiveUser(auth), httpauth.RequireEnabledUser(auth)).Put("/api/auth/email", h.UpdateEmail)
	r.With(httpauth.RequireAuth(auth), httpauth.RequireActiveUser(auth), httpauth.RequireEnabledUser(auth)).Put("/api/auth/name", h.UpdateName)
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

	user, err := users.Create(context.Background(), username, username+"@example.com", string(hash), true)
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

func login(t *testing.T, srv *httptest.Server, email, password string) *http.Response {
	t.Helper()

	body, err := json.Marshal(loginRequest{Email: email, Password: password})
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

	resp := login(t, srv, "admin@example.com", "admin")
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

// TestRefreshCookie_SecureFollowsCookieSecureSetting covers #239: a
// self-hoster on a plain-HTTP LAN deployment can turn Secure off via
// COOKIE_SECURE, and setRefreshCookie/clearRefreshCookie must agree on the
// setting — a cookie set under one value has to be clearable under the same
// one, or logout leaves it behind.
func TestRefreshCookie_SecureFollowsCookieSecureSetting(t *testing.T) {
	for _, cookieSecure := range []bool{true, false} {
		t.Run(fmt.Sprintf("cookieSecure=%v", cookieSecure), func(t *testing.T) {
			srv := newAuthTestServerWithCookieSecure(t, false, false, cookieSecure)

			loginResp := login(t, srv, "admin@example.com", "admin")
			defer loginResp.Body.Close()
			loginCookie := refreshCookieFrom(t, loginResp)
			if loginCookie.Secure != cookieSecure {
				t.Fatalf("expected login's refresh_token cookie Secure=%v, got %v", cookieSecure, loginCookie.Secure)
			}
			if !loginCookie.HttpOnly {
				t.Fatalf("expected refresh_token cookie to stay HttpOnly regardless of COOKIE_SECURE")
			}
			if loginCookie.SameSite != http.SameSiteLaxMode {
				t.Fatalf("expected refresh_token cookie to stay SameSite=Lax regardless of COOKIE_SECURE")
			}

			logoutReq, err := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/logout", nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			logoutReq.AddCookie(loginCookie)

			logoutResp, err := http.DefaultClient.Do(logoutReq)
			if err != nil {
				t.Fatalf("POST /api/auth/logout: %v", err)
			}
			defer logoutResp.Body.Close()

			clearedCookie := refreshCookieFrom(t, logoutResp)
			if clearedCookie.Secure != cookieSecure {
				t.Fatalf("expected logout's cleared refresh_token cookie Secure=%v to match the setting the cookie was set under, got %v", cookieSecure, clearedCookie.Secure)
			}
		})
	}
}

func TestLogin_InvalidCredentials_Returns401(t *testing.T) {
	srv := newAuthTestServer(t)

	resp := login(t, srv, "admin@example.com", "wrong-password")
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
	// No bootstrap account: these tests seed or register the Users they need.
	cfg := apptest.Config(t)
	cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword = "", "", ""
	g := newTestGraphWithConfig(t, cfg)

	users := g.UserRepo
	auth := g.Auth

	hash, err := bcrypt.GenerateFromPassword([]byte("hunter2"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user, err := users.Create(context.Background(), "alice", "alice@example.com", string(hash), false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := users.SetDisabled(context.Background(), user.ID, true); err != nil {
		t.Fatalf("disable user: %v", err)
	}

	h := NewAuthHandler(auth, false, false, true)
	r := chi.NewRouter()
	r.Post("/api/auth/login", h.Login)
	r.With(httpauth.RequireAuth(auth), httpauth.RequireActiveUser(auth), httpauth.RequireEnabledUser(auth)).Get("/api/auth/me", h.Me)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	loginResp := login(t, srv, "alice@example.com", "hunter2")
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

	loginResp := login(t, srv, "admin@example.com", "admin")
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

	loginResp := login(t, srv, "admin@example.com", "admin")
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
	if me.Name != "admin" {
		t.Fatalf("expected name 'admin', got %q", me.Name)
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

	loginResp := login(t, srv, "admin@example.com", "admin")
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

// TestMe_EmailReminderChannelAvailable_FalseWithoutSMTP covers ADR-0047's
// side effect on ADR-0021: every account always has an email now, so
// channel availability collapses to just whether SMTP is configured.
func TestMe_EmailReminderChannelAvailable_FalseWithoutSMTP(t *testing.T) {
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
	if me.Email != "admin@example.com" {
		t.Fatalf("expected the bootstrap account's email, got %+v", me.Email)
	}
	if me.EmailReminderChannelAvailable {
		t.Fatalf("expected the Email Channel to be unavailable without SMTP configured")
	}
}

func TestMe_EmailReminderChannelAvailable_TrueWithSMTPConfigured(t *testing.T) {
	srv := newAuthTestServerWithSMTP(t, true)
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
	if !me.EmailReminderChannelAvailable {
		t.Fatalf("expected the Email Channel to be available once SMTP is configured")
	}
}

// TestMe_InvitationRepliesConfigured_FalseWithoutIMAP covers ADR-0059's
// "an instance can send Invitations with no IMAP configured" — Settings
// needs this to disclose that a Response never comes back, independent of
// whether SMTP is configured.
func TestMe_InvitationRepliesConfigured_FalseWithoutIMAP(t *testing.T) {
	srv := newAuthTestServerWithMail(t, true, false)
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
	if me.InvitationRepliesConfigured {
		t.Fatalf("expected invitation replies to be unconfigured without IMAP configured")
	}
}

func TestMe_InvitationRepliesConfigured_TrueWithIMAPConfigured(t *testing.T) {
	srv := newAuthTestServerWithMail(t, true, true)
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
	if !me.InvitationRepliesConfigured {
		t.Fatalf("expected invitation replies to be configured once IMAP is configured")
	}
}

func TestUpdateEmail_ChangesTheLoginIdentifier(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updateEmailRequest{Email: "new-admin@example.com"})
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
	if me.Email != "new-admin@example.com" {
		t.Fatalf("expected email to be updated, got %+v", me.Email)
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

// TestUpdateName_RenamesButLoginStillNeedsTheOriginalEmail covers ADR-0047:
// Name is a display-only rename — it never affects the login identifier, so
// signing in still requires the account's Email, before and after the
// rename.
func TestUpdateName_RenamesButLoginStillNeedsTheOriginalEmail(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updateNameRequest{Name: "New Admin"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/auth/name", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/auth/name: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if me.Name != "New Admin" {
		t.Fatalf("expected name 'New Admin', got %q", me.Name)
	}

	loginResp := login(t, srv, "admin@example.com", "a-new-password")
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("expected login with the unchanged email to still work, got %d", loginResp.StatusCode)
	}
}

// TestUpdateName_AcceptsSpaces covers ADR-0047: Name is a display label, not
// an identifier, so unlike the old username it must accept a space —
// "Jane Smith" is a valid name.
func TestUpdateName_AcceptsSpaces(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updateNameRequest{Name: "Jane Smith"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/auth/name", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/auth/name: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestUpdateName_RejectsEmptyName(t *testing.T) {
	srv := newAuthTestServer(t)
	accessToken := authenticatedAccessToken(t, srv)

	body, _ := json.Marshal(updateNameRequest{Name: "   "})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/auth/name", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/auth/name: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestUpdateName_DuplicateNameIsAllowed covers ADR-0047: two accounts may
// share a display Name — only Email is unique.
func TestUpdateName_DuplicateNameIsAllowed(t *testing.T) {
	g := newTestGraph(t)

	users := g.UserRepo
	auth := g.Auth
	bootstrapUser, _, err := auth.Bootstrap(context.Background())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := users.Create(context.Background(), "bob", "bob@example.com", "hash", false); err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if _, err := auth.ChangePassword(context.Background(), bootstrapUser.ID, "admin", "a-new-password"); err != nil {
		t.Fatalf("change password: %v", err)
	}

	h := NewAuthHandler(auth, false, false, true)
	r := chi.NewRouter()
	r.Post("/api/auth/login", h.Login)
	r.With(httpauth.RequireAuth(auth), httpauth.RequireActiveUser(auth)).Put("/api/auth/name", h.UpdateName)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	loginResp := login(t, srv, "admin@example.com", "a-new-password")
	defer loginResp.Body.Close()
	accessToken := mustLoginAccessToken(t, loginResp)

	body, _ := json.Marshal(updateNameRequest{Name: "bob"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/auth/name", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/auth/name: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 — Name isn't unique, so sharing bob's name is allowed, got %d", resp.StatusCode)
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

	loginResp := login(t, srv, "admin@example.com", "admin")
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

	loginResp := login(t, srv, "admin@example.com", "admin")
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

	loginResp := login(t, srv, "admin@example.com", "admin")
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

	loginResp := login(t, srv, "admin@example.com", "admin")
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

	loginResp := login(t, srv, "admin@example.com", "admin")
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

	loginResp := login(t, srv, "admin@example.com", "admin")
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

func mustGetSetupStatus(t *testing.T, srv *httptest.Server) setupStatusResponse {
	t.Helper()

	resp, err := http.Get(srv.URL + "/api/auth/setup-status")
	if err != nil {
		t.Fatalf("GET /api/auth/setup-status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body setupStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode setup-status response: %v", err)
	}
	return body
}

// TestSetupStatus_AccountExistsAndSignupsDisabled covers #235: this is what
// a default self-host looks like once bootstrapped — ENABLE_SIGNUPS unset,
// one account already created — the shape the frontend uses to hide
// registration.
func TestSetupStatus_AccountExistsAndSignupsDisabled(t *testing.T) {
	srv := newAuthTestServer(t)

	body := mustGetSetupStatus(t, srv)
	if !body.HasAccounts {
		t.Fatalf("expected has_accounts true")
	}
	if body.SignupsEnabled {
		t.Fatalf("expected signups_enabled false")
	}
}

// TestSetupStatus_AccountExistsAndSignupsEnabled covers #235's "signups
// enabled" acceptance criterion: the link and form are back once
// ENABLE_SIGNUPS is true, even with accounts already on the instance.
func TestSetupStatus_AccountExistsAndSignupsEnabled(t *testing.T) {
	cfg := apptest.Config(t)
	cfg.EnableSignups = true
	g := newTestGraphWithConfig(t, cfg)
	if _, _, err := g.Auth.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	h := NewAuthHandler(g.Auth, false, false, true)
	r := chi.NewRouter()
	r.Get("/api/auth/setup-status", h.SetupStatus)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	body := mustGetSetupStatus(t, srv)
	if !body.HasAccounts {
		t.Fatalf("expected has_accounts true (apptest.Config seeds a bootstrap account)")
	}
	if !body.SignupsEnabled {
		t.Fatalf("expected signups_enabled true")
	}
}

// TestSetupStatus_NoAccountsYet covers the fresh-instance case: no accounts
// at all, so signups_enabled is irrelevant — RegisterPage always shows the
// bootstrap form regardless of it (ADR-0044, #235).
func TestSetupStatus_NoAccountsYet(t *testing.T) {
	cfg := apptest.Config(t)
	cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword = "", "", ""
	g := newTestGraphWithConfig(t, cfg)

	h := NewAuthHandler(g.Auth, false, false, true)
	r := chi.NewRouter()
	r.Get("/api/auth/setup-status", h.SetupStatus)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	body := mustGetSetupStatus(t, srv)
	if body.HasAccounts {
		t.Fatalf("expected has_accounts false")
	}
}
