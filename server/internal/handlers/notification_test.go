package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/apptest"
	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/repository"
)

func newNotificationTestServer(t *testing.T) (baseURL, accessToken string, userID int64, notifications *repository.NotificationRepository, eventID string) {
	t.Helper()

	cfg := apptest.Config(t)
	cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword = "alice", "alice@example.com", "hunter2"
	g := newTestGraphWithConfig(t, cfg)

	workspaceSvc := g.Workspaces
	auth := g.Auth
	user, _, err := auth.Bootstrap(context.Background())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	loginResult, err := auth.Login(context.Background(), "alice@example.com", "hunter2")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	userWorkspaces, err := workspaceSvc.ListForUser(context.Background(), user.ID)
	if err != nil || len(userWorkspaces) == 0 {
		t.Fatalf("list user workspaces: %v", err)
	}

	calendars := g.CalendarRepo
	cal, err := calendars.Create(context.Background(), user.ID, userWorkspaces[0].ID, "cal-1", repository.CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	events := g.EventRepo
	start, _ := time.Parse(time.RFC3339, "2026-01-01T09:00:00Z")
	end, _ := time.Parse(time.RFC3339, "2026-01-01T10:00:00Z")
	if _, err := events.Create(context.Background(), "evt-1", &user.ID, repository.EventFields{CalendarID: cal.ID, Title: "Standup", Start: start, End: end}, 0); err != nil {
		t.Fatalf("create event: %v", err)
	}

	notifications = g.NotificationRepo
	notificationHandler := NewNotificationHandler(g.Notifications)

	r := chi.NewRouter()
	r.Route("/api/notifications", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Get("/", notificationHandler.List)
		r.Post("/seen", notificationHandler.MarkSeen)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return srv.URL, loginResult.AccessToken, user.ID, notifications, "evt-1"
}

func TestNotificationHandler_ListReturnsRecentNotificationsNewestFirst(t *testing.T) {
	baseURL, accessToken, userID, notifications, eventID := newNotificationTestServer(t)
	ctx := context.Background()

	occurrenceStart := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	firedAt := time.Date(2026, 1, 1, 8, 50, 0, 0, time.UTC)
	if _, err := notifications.Insert(ctx, userID, eventID, occurrenceStart, "Standup", firedAt); err != nil {
		t.Fatalf("insert notification: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/notifications/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/notifications: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got []notificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 notification, got %+v", got)
	}
	if got[0].Title != "Standup" || got[0].EventID != eventID || got[0].Seen {
		t.Fatalf("unexpected notification: %+v", got[0])
	}
	if got[0].Kind != "reminder" || got[0].OccurrenceStart == nil {
		t.Fatalf("expected a reminder notification with an occurrenceStart, got %+v", got[0])
	}
}

// TestNotificationHandler_ListOmitsOccurrenceStartForInviteNotifications
// covers ADR-0061: an invite Notification's occurrenceStart is absent on
// the wire, and kind distinguishes it from a fired-Reminder entry.
func TestNotificationHandler_ListOmitsOccurrenceStartForInviteNotifications(t *testing.T) {
	baseURL, accessToken, userID, notifications, eventID := newNotificationTestServer(t)
	ctx := context.Background()

	invitedAt := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	if _, err := notifications.InsertInvite(ctx, userID, eventID, "Standup", invitedAt); err != nil {
		t.Fatalf("insert invite notification: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/notifications/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/notifications: %v", err)
	}
	defer resp.Body.Close()

	var got []notificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 notification, got %+v", got)
	}
	if got[0].Kind != "invite" {
		t.Fatalf("expected kind %q, got %q", "invite", got[0].Kind)
	}
	if got[0].OccurrenceStart != nil {
		t.Fatalf("expected a nil occurrenceStart for an invite notification, got %v", *got[0].OccurrenceStart)
	}
}

func TestNotificationHandler_ListRequiresAuth(t *testing.T) {
	baseURL, _, _, _, _ := newNotificationTestServer(t)

	resp, err := http.Get(baseURL + "/api/notifications/")
	if err != nil {
		t.Fatalf("GET /api/notifications: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestNotificationHandler_MarkSeenClearsUnseenNotifications(t *testing.T) {
	baseURL, accessToken, userID, notifications, eventID := newNotificationTestServer(t)
	ctx := context.Background()

	occurrenceStart := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	firedAt := time.Date(2026, 1, 1, 8, 50, 0, 0, time.UTC)
	if _, err := notifications.Insert(ctx, userID, eventID, occurrenceStart, "Standup", firedAt); err != nil {
		t.Fatalf("insert notification: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/notifications/seen", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/notifications/seen: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	list, err := notifications.ListRecentByUser(ctx, userID, 10)
	if err != nil {
		t.Fatalf("list recent: %v", err)
	}
	if len(list) != 1 || !list[0].Seen {
		t.Fatalf("expected the notification to be marked seen, got %+v", list)
	}
}
