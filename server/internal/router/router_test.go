package router

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emersion/go-webdav/caldav"

	"github.com/XiovV/calendar/server/internal/caldavserver"
	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/handlers"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	authService := service.NewAuthService(users, sessions, []byte("test-secret"), "", "")
	if _, _, err := authService.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	authHandler := handlers.NewAuthHandler(authService, false)
	calendarService := service.NewCalendarService(repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB), users)
	eventService := service.NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendarService)
	calendarHandler := handlers.NewCalendarHandler(calendarService, eventService, service.NewImportService(eventService, calendarService), service.NewSubscribeService(eventService, calendarService, 0))
	eventHandler := handlers.NewEventHandler(eventService)
	notificationHandler := handlers.NewNotificationHandler(service.NewNotificationService(repository.NewNotificationRepository(sqlDB)))
	appPasswordService := service.NewAppPasswordService(repository.NewAppPasswordRepository(sqlDB), users)
	appPasswordHandler := handlers.NewAppPasswordHandler(appPasswordService)
	accountHandler := handlers.NewAccountHandler(service.NewAccountService(users, sessions, calendarService))
	calDAVHandler := &caldav.Handler{Backend: caldavserver.NewBackend(calendarService, eventService), Prefix: "/dav"}

	r, err := New(slog.New(slog.NewTextHandler(io.Discard, nil)), authHandler, calendarHandler, eventHandler, notificationHandler, appPasswordHandler, accountHandler, calDAVHandler, authService, authService, appPasswordService, authService)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func TestRouter_APIRouteIsNotShadowedBySPACatchAll(t *testing.T) {
	r := newTestRouter(t)

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /api/health to return 200, got %d", resp.StatusCode)
	}
}

func TestRouter_UnknownClientRouteFallsBackToSPA(t *testing.T) {
	r := newTestRouter(t)

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/some/client/route")
	if err != nil {
		t.Fatalf("GET /some/client/route: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (index.html fallback), got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(body) == 0 {
		t.Fatalf("expected non-empty index.html body")
	}
}
