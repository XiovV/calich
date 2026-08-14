package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/apptest"
	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/service"
)

// defaultRemindersTestServer wires the Default reminders endpoints
// (ADR-0064) over real HTTP, plus direct access to the EventService so a
// test can read a Calendar's CTag the way CalDAV would without standing up
// the whole CalDAV server — CTag has no REST representation of its own.
type defaultRemindersTestServer struct {
	baseURL     string
	accessToken string
	userID      int64
	calendarID  string
	events      *service.EventService
}

func newDefaultRemindersTestServer(t *testing.T) defaultRemindersTestServer {
	t.Helper()
	ctx := context.Background()

	cfg := apptest.Config(t)
	cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword = "alice", "alice@example.com", "hunter2"
	g := newTestGraphWithConfig(t, cfg)

	workspaces := g.Workspaces
	calendars := g.Calendars
	auth := g.Auth
	bootstrapUser, _, err := auth.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	userWorkspaces, err := workspaces.ListForUser(ctx, bootstrapUser.ID)
	if err != nil {
		t.Fatalf("list workspaces: %v", err)
	}
	if len(userWorkspaces) != 1 {
		t.Fatalf("expected alice to belong to exactly one workspace, got %d", len(userWorkspaces))
	}

	login, err := auth.Login(ctx, "alice@example.com", "hunter2")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	cal, err := calendars.Create(ctx, bootstrapUser.ID, userWorkspaces[0].ID, "cal-1", service.CalendarWrite{Name: "Personal", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	events := g.Events
	if _, err := events.Create(ctx, bootstrapUser.ID, "evt-1", service.EventWrite{
		CalendarID: cal.ID, Title: "Standup",
		Start: mustParseTestTime(t, "2026-01-01T09:00:00Z"),
		End:   mustParseTestTime(t, "2026-01-01T10:00:00Z"),
	}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	calendarHandler := NewCalendarHandler(calendars, events, nil, nil, nil)

	r := chi.NewRouter()
	r.Route("/api/calendars", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Get("/{id}/default-reminders", calendarHandler.GetDefaultReminders)
		r.Put("/{id}/default-reminders", calendarHandler.SetDefaultReminders)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return defaultRemindersTestServer{
		baseURL:     srv.URL,
		accessToken: login.AccessToken,
		userID:      bootstrapUser.ID,
		calendarID:  cal.ID,
		events:      events,
	}
}

// TestCalendarHandler_SetDefaultReminders_BumpsCTag drives PUT
// /api/calendars/{id}/default-reminders over real HTTP and confirms the
// Calendar's CTag actually moves (ADR-0064, #213) — the handler wiring a
// service-level test of EventService.BumpCalendarChangeSeq alone can't
// prove, since a future refactor could drop the call from the handler
// without any service-level test noticing.
func TestCalendarHandler_SetDefaultReminders_BumpsCTag(t *testing.T) {
	s := newDefaultRemindersTestServer(t)
	ctx := context.Background()

	before, err := s.events.CalendarCTag(ctx, s.userID, s.calendarID)
	if err != nil {
		t.Fatalf("ctag before: %v", err)
	}

	body, err := json.Marshal(setDefaultRemindersRequest{
		AllDay:    false,
		Reminders: []reminderWire{{OffsetMinutes: 15, Channel: "notification"}},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPut, s.baseURL+"/api/calendars/"+s.calendarID+"/default-reminders", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	after, err := s.events.CalendarCTag(ctx, s.userID, s.calendarID)
	if err != nil {
		t.Fatalf("ctag after: %v", err)
	}
	if after <= before {
		t.Fatalf("expected ctag to increase after PUTting a default reminders change, before=%d after=%d", before, after)
	}
}
