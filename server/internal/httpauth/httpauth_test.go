package httpauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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

func TestRequireCalDAVAuth_MissingCredentials_Returns401(t *testing.T) {
	handler := RequireCalDAVAuth(fakeCalDAVAuthenticator{userID: 1})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := RequireCalDAVAuth(fakeCalDAVAuthenticator{err: errors.New("bad credentials")})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/dav/", nil)
	req.SetBasicAuth("admin", "wrong-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireCalDAVAuth_ValidCredentials_SetsUserIDInContext(t *testing.T) {
	var gotUserID int64
	var gotOK bool

	handler := RequireCalDAVAuth(fakeCalDAVAuthenticator{userID: 7})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

type fakeAdminChecker struct {
	isAdmin bool
	err     error
}

func (f fakeAdminChecker) IsAdmin(ctx context.Context, userID int64) (bool, error) {
	return f.isAdmin, f.err
}

func TestRequireAdmin_NoUserIDInContext_Returns401(t *testing.T) {
	handler := RequireAdmin(fakeAdminChecker{isAdmin: true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireAdmin_NonAdmin_Returns403(t *testing.T) {
	handler := RequireAdmin(fakeAdminChecker{isAdmin: false})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestRequireAdmin_Admin_CallsNext(t *testing.T) {
	called := false
	handler := RequireAdmin(fakeAdminChecker{isAdmin: true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
