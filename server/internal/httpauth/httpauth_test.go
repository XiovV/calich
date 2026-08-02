package httpauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
