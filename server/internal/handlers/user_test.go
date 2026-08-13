package handlers

import (
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

// userDirectoryTestServer is #113's REST fixture: three Users — the caller,
// an enabled peer, and a Disabled one — against a router carrying the
// directory route.
type userDirectoryTestServer struct {
	baseURL     string
	callerToken string
}

func newUserDirectoryTestServer(t *testing.T) userDirectoryTestServer {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspaceSvc := service.NewWorkspaceService(sqlDB, workspaceRepo, repository.NewWorkspaceInviteRepository(sqlDB), repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB))
	calendarRepo := repository.NewCalendarRepository(sqlDB)
	shareRepo := repository.NewCalendarShareRepository(sqlDB)
	calendars := service.NewCalendarService(calendarRepo, shareRepo, users, repository.NewEventReminderRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	auth := service.NewAuthService(users, sessions, workspaceSvc, repository.NewWorkspaceInviteRepository(sqlDB), calendars, repository.NewAttendeeRepository(sqlDB), []byte("test-secret"), "caller", "caller@example.com", "hunter2", true)
	ctx := context.Background()
	if _, _, err := auth.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	accounts := service.NewAccountService(sqlDB, users, sessions, calendarRepo, shareRepo, workspaceRepo, workspaceSvc)

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

	userHandler := NewUserHandler(service.NewUserService(users))

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
