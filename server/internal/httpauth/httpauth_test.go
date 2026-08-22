package httpauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/XiovV/calich/server/internal/repository"
)

type fakeCalDAVAuthenticator struct {
	userID int64
	err    error
}

func (f fakeCalDAVAuthenticator) Authenticate(ctx context.Context, username, password string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.userID, nil
}

// fakeCalDAVRateLimiter is CalDAVRateLimiter's test double — allow always
// permits, and checkErr lets a test simulate the ceiling already being
// tripped without a real repository behind it.
type fakeCalDAVRateLimiter struct {
	checkErr        error
	recordFailCalls []string
	clearCalls      []string
}

func (f *fakeCalDAVRateLimiter) CheckAuth(ctx context.Context, email, ip string) error {
	return f.checkErr
}

func (f *fakeCalDAVRateLimiter) RecordAuthFailure(ctx context.Context, email, ip string) error {
	f.recordFailCalls = append(f.recordFailCalls, email)
	return nil
}

func (f *fakeCalDAVRateLimiter) ClearAuthFailures(ctx context.Context, email string) error {
	f.clearCalls = append(f.clearCalls, email)
	return nil
}

func TestRequireCalDAVAuth_MissingCredentials_Returns401(t *testing.T) {
	handler := RequireCalDAVAuth(fakeCalDAVAuthenticator{userID: 1}, &fakeCalDAVRateLimiter{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/dav/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("expected a WWW-Authenticate header to prompt the client for Basic credentials")
	}
}

func TestRequireCalDAVAuth_InvalidCredentials_Returns401(t *testing.T) {
	limiter := &fakeCalDAVRateLimiter{}
	handler := RequireCalDAVAuth(fakeCalDAVAuthenticator{err: errors.New("bad credentials")}, limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/dav/", nil)
	req.SetBasicAuth("admin", "wrong-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if len(limiter.recordFailCalls) != 1 || limiter.recordFailCalls[0] != "admin" {
		t.Fatalf("expected the failure to be recorded against %q, got %v", "admin", limiter.recordFailCalls)
	}
}

func TestRequireCalDAVAuth_ValidCredentials_SetsUserIDInContext(t *testing.T) {
	var gotUserID int64
	var gotOK bool

	limiter := &fakeCalDAVRateLimiter{}
	handler := RequireCalDAVAuth(fakeCalDAVAuthenticator{userID: 7}, limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID, gotOK = UserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/dav/", nil)
	req.SetBasicAuth("admin", "an-app-password-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !gotOK {
		t.Fatalf("expected user id to be set in context")
	}
	if gotUserID != 7 {
		t.Fatalf("expected user id 7, got %d", gotUserID)
	}
	if len(limiter.clearCalls) != 1 || limiter.clearCalls[0] != "admin" {
		t.Fatalf("expected a successful auth to clear failures for %q, got %v", "admin", limiter.clearCalls)
	}
}

func TestRequireCalDAVAuth_RateLimited_Returns429WithoutAttemptingAuth(t *testing.T) {
	authCalled := false
	auth := fakeCalDAVAuthenticatorFunc(func(ctx context.Context, username, password string) (int64, error) {
		authCalled = true
		return 1, nil
	})
	limiter := &fakeCalDAVRateLimiter{checkErr: repository.ErrRateLimited}

	handler := RequireCalDAVAuth(auth, limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/dav/", nil)
	req.SetBasicAuth("admin", "whatever")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if authCalled {
		t.Fatalf("expected CheckAuth's rejection to skip Authenticate (and its bcrypt cost) entirely")
	}
	if rec.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("expected no WWW-Authenticate challenge on a rate-limited response")
	}
}

// TestRequireCalDAVAuth_RateLimiterError_Returns500NotUnauthorized pins that
// CheckAuth's own failure to answer (a DB error, say) is distinguishable
// from a verdict about the credentials: it must not collapse into the same
// 401/WWW-Authenticate a bad password gets, which would tell a client to
// resend credentials when retrying can't help.
func TestRequireCalDAVAuth_RateLimiterError_Returns500NotUnauthorized(t *testing.T) {
	authCalled := false
	auth := fakeCalDAVAuthenticatorFunc(func(ctx context.Context, username, password string) (int64, error) {
		authCalled = true
		return 1, nil
	})
	limiter := &fakeCalDAVRateLimiter{checkErr: errors.New("db unavailable")}

	handler := RequireCalDAVAuth(auth, limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/dav/", nil)
	req.SetBasicAuth("admin", "whatever")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if authCalled {
		t.Fatalf("expected CheckAuth's own error to skip Authenticate entirely")
	}
	if rec.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("expected no WWW-Authenticate challenge on a 500 — the problem isn't the credentials")
	}
}

type fakeCalDAVAuthenticatorFunc func(ctx context.Context, username, password string) (int64, error)

func (f fakeCalDAVAuthenticatorFunc) Authenticate(ctx context.Context, username, password string) (int64, error) {
	return f(ctx, username, password)
}

type fakeAuthenticator struct {
	userID int64
	err    error
}

func (f fakeAuthenticator) Authenticate(ctx context.Context, accessToken string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.userID, nil
}

func TestRequireAuth_MissingHeader_Returns401(t *testing.T) {
	handler := RequireAuth(fakeAuthenticator{userID: 1})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireAuth_InvalidToken_Returns401(t *testing.T) {
	handler := RequireAuth(fakeAuthenticator{err: errors.New("bad token")})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer garbage")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireAuth_ValidToken_SetsUserIDInContext(t *testing.T) {
	var gotUserID int64
	var gotOK bool

	handler := RequireAuth(fakeAuthenticator{userID: 42})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID, gotOK = UserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !gotOK {
		t.Fatalf("expected user id to be set in context")
	}
	if gotUserID != 42 {
		t.Fatalf("expected user id 42, got %d", gotUserID)
	}
}

type fakeActiveUserChecker struct {
	mustChangePassword bool
	err                error
}

func (f fakeActiveUserChecker) MustChangePassword(ctx context.Context, userID int64) (bool, error) {
	return f.mustChangePassword, f.err
}

func TestRequireActiveUser_NoUserIDInContext_Returns401(t *testing.T) {
	handler := RequireActiveUser(fakeActiveUserChecker{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireActiveUser_MustChangePassword_Returns403(t *testing.T) {
	handler := RequireActiveUser(fakeActiveUserChecker{mustChangePassword: true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(withUserID(req.Context(), 1))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestRequireActiveUser_ActiveUser_CallsNext(t *testing.T) {
	called := false
	handler := RequireActiveUser(fakeActiveUserChecker{mustChangePassword: false})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(withUserID(req.Context(), 1))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !called {
		t.Fatalf("expected next handler to be called")
	}
}

type fakeDisabledChecker struct {
	isDisabled bool
	err        error
}

func (f fakeDisabledChecker) IsDisabled(ctx context.Context, userID int64) (bool, error) {
	return f.isDisabled, f.err
}

func TestRequireEnabledUser_NoUserIDInContext_Returns401(t *testing.T) {
	handler := RequireEnabledUser(fakeDisabledChecker{isDisabled: false})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireEnabledUser_Disabled_Returns403(t *testing.T) {
	handler := RequireEnabledUser(fakeDisabledChecker{isDisabled: true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(withUserID(req.Context(), 1))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestRequireEnabledUser_Enabled_CallsNext(t *testing.T) {
	called := false
	handler := RequireEnabledUser(fakeDisabledChecker{isDisabled: false})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(withUserID(req.Context(), 1))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !called {
		t.Fatalf("expected next handler to be called")
	}
}
