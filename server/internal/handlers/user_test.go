package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/apptest"
	"github.com/XiovV/calich/server/internal/httpauth"
)

// userDirectoryTestServer is #113's REST fixture: three Users — the caller,
// an enabled peer, and a Disabled one — against a router carrying the
// directory route.
type userDirectoryTestServer struct {
	baseURL     string
	callerToken string
}

func newUserDirectoryTestServer(t *testing.T) userDirectoryTestServer {
	t.Helper()

	cfg := apptest.Config(t)
	cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword = "caller", "caller@example.com", "hunter2"
	cfg.EnableSignups = true
	g := newTestGraphWithConfig(t, cfg)

	users := g.UserRepo
	auth := g.Auth
	ctx := context.Background()
	if _, _, err := auth.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	accounts := g.Accounts

	if _, err := auth.Register(ctx, "bob", "bob@example.com", "temp-password"); err != nil {
		t.Fatalf("register bob: %v", err)
	}
	if _, err := auth.Register(ctx, "ghost", "ghost@example.com", "temp-password"); err != nil {
		t.Fatalf("register ghost: %v", err)
	}
	ghost, err := users.GetByEmail(ctx, "ghost@example.com")
	if err != nil {
		t.Fatalf("get ghost: %v", err)
	}
	if _, err := accounts.SetDisabled(ctx, ghost.ID, true); err != nil {
		t.Fatalf("disable ghost: %v", err)
	}

	userHandler := NewUserHandler(g.Users)

	r := chi.NewRouter()
	r.Route("/api/users", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Get("/", userHandler.Directory)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	callerLogin, err := auth.Login(ctx, "caller@example.com", "hunter2")
	if err != nil {
		t.Fatalf("caller login: %v", err)
	}

	return userDirectoryTestServer{baseURL: srv.URL, callerToken: callerLogin.AccessToken}
}

func TestUserHandler_Directory(t *testing.T) {
	s := newUserDirectoryTestServer(t)

	resp, err := authenticatedGet(s.baseURL+"/api/users/", s.callerToken)
	if err != nil {
		t.Fatalf("directory: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var users []userDirectoryResponse
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Excludes the caller and the disabled user, leaving only "bob".
	if len(users) != 1 || users[0].Name != "bob" {
		t.Fatalf("unexpected directory: %+v", users)
	}
}

func TestUserHandler_Directory_RequiresAuth(t *testing.T) {
	s := newUserDirectoryTestServer(t)

	resp, err := http.Get(s.baseURL + "/api/users/")
	if err != nil {
		t.Fatalf("directory: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
