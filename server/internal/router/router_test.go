package router

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emersion/go-webdav/caldav"

	"github.com/XiovV/calendar/server/internal/attachmentstore"
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
	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspaceService := service.NewWorkspaceService(sqlDB, workspaceRepo, repository.NewWorkspaceInviteRepository(sqlDB), repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB))
	calendarRepo := repository.NewCalendarRepository(sqlDB)
	shareRepo := repository.NewCalendarShareRepository(sqlDB)
	calendarService := service.NewCalendarService(calendarRepo, shareRepo, users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	authService := service.NewAuthService(users, sessions, workspaceService, repository.NewWorkspaceInviteRepository(sqlDB), calendarService, []byte("test-secret"), "admin", "admin@example.com", "admin", false)
	if _, _, err := authService.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	authHandler := handlers.NewAuthHandler(authService, false)
	workspaceHandler := handlers.NewWorkspaceHandler(workspaceService)
	eventService := service.NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendarService, users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo, repository.NewGroupRepository(sqlDB))
	attachmentStore := attachmentstore.New(t.TempDir())
	calendarHandler := handlers.NewCalendarHandler(calendarService, eventService, service.NewImportService(eventService, calendarService, attachmentStore, 25<<20, 10), service.NewSubscribeService(eventService, calendarService, 0), attachmentStore)
	eventHandler := handlers.NewEventHandler(eventService, attachmentStore)
	attachmentService := service.NewAttachmentService(repository.NewAttachmentRepository(sqlDB), repository.NewEventRepository(sqlDB), calendarService, eventService, attachmentStore, 10)
	attachmentHandler := handlers.NewAttachmentHandler(attachmentService, 25<<20)
	notificationHandler := handlers.NewNotificationHandler(service.NewNotificationService(repository.NewNotificationRepository(sqlDB)))
	appPasswordService := service.NewAppPasswordService(repository.NewAppPasswordRepository(sqlDB), users)
	appPasswordHandler := handlers.NewAppPasswordHandler(appPasswordService)
	accountHandler := handlers.NewAccountHandler(service.NewAccountService(sqlDB, users, sessions, calendarRepo, shareRepo, workspaceRepo, workspaceService))
	userHandler := handlers.NewUserHandler(service.NewUserService(users))
	groupHandler := handlers.NewGroupHandler(service.NewGroupService(repository.NewGroupRepository(sqlDB), workspaceRepo))
	calDAVHandler := &caldav.Handler{Backend: caldavserver.NewBackend(calendarService, eventService, attachmentService, 25<<20, 10), Prefix: "/dav"}

	r, err := New(slog.New(slog.NewTextHandler(io.Discard, nil)), authHandler, calendarHandler, eventHandler, attachmentHandler, notificationHandler, appPasswordHandler, accountHandler, userHandler, workspaceHandler, groupHandler, calDAVHandler, authService, authService, appPasswordService, authService, workspaceService)
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
