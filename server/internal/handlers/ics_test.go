package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

// icsTestEnv wires both /api/events and /api/calendars (including their ICS
// routes) against one in-memory database, plus direct service access so
// tests can set up series/overrides without going through the JSON write
// endpoints.
type icsTestEnv struct {
	baseURL     string
	accessToken string
	calendarID  string
	events      *service.EventService
	calendars   *service.CalendarService
}

func newICSTestEnv(t *testing.T) icsTestEnv {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	auth := service.NewAuthService(users, sessions, []byte("test-secret"), "alice", "hunter2")
	if _, _, err := auth.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	loginResult, err := auth.Login(context.Background(), "alice", "hunter2")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	userID, err := auth.Authenticate(context.Background(), loginResult.AccessToken)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	calendarRepo := repository.NewCalendarRepository(sqlDB)
	calendars := service.NewCalendarService(calendarRepo)
	cal, err := calendars.Create(context.Background(), userID, "11111111-1111-1111-1111-111111111111", service.CalendarWrite{Name: "Personal", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	events := service.NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendars)
	eventHandler := NewEventHandler(events)
	calendarHandler := NewCalendarHandler(calendars, events, service.NewImportService(events, calendars), service.NewSubscribeService(events, calendars, 0))

	r := chi.NewRouter()
	r.Route("/api/events", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Get("/{id}/ics", eventHandler.ICS)
	})
	r.Route("/api/calendars", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Get("/ics", calendarHandler.ICSAll)
		r.Get("/{id}/ics", calendarHandler.ICS)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return icsTestEnv{baseURL: srv.URL, accessToken: loginResult.AccessToken, calendarID: cal.ID, events: events, calendars: calendars}
}

func (env icsTestEnv) get(t *testing.T, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, env.baseURL+path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+env.accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	return resp
}

func TestEventHandler_ICS_ScopeAll_DefaultsToWholeSeries(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := env.events.Create(ctx, 1, "evt-1", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY;BYDAY=TU",
	})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := start.AddDate(0, 0, 7)
	if _, err := env.events.Create(ctx, 1, "evt-1-override", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup (moved)", Start: recurrenceID.Add(2 * time.Hour), End: recurrenceID.Add(2*time.Hour + 30*time.Minute),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
	}); err != nil {
		t.Fatalf("create override: %v", err)
	}

	resp := env.get(t, "/api/events/"+master.ID+"/ics")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/calendar; charset=utf-8" {
		t.Fatalf("expected text/calendar content type, got %q", ct)
	}
	if resp.Header.Get("Content-Disposition") == "" {
		t.Fatalf("expected a Content-Disposition header")
	}

	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if countOccurrences(text, "BEGIN:VEVENT") != 2 {
		t.Fatalf("expected master + override VEVENTs, got:\n%s", text)
	}
	if !containsAll(text, "UID:evt-1", "SUMMARY:Standup (moved)") {
		t.Fatalf("expected both series members, got:\n%s", text)
	}
}

func TestEventHandler_ICS_ScopeAll_ResolvesViaOverrideID(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := env.events.Create(ctx, 1, "evt-1", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY;BYDAY=TU",
	})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := start.AddDate(0, 0, 7)
	override, err := env.events.Create(ctx, 1, "evt-1-override", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup (moved)", Start: recurrenceID.Add(2 * time.Hour), End: recurrenceID.Add(2*time.Hour + 30*time.Minute),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
	})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	resp := env.get(t, "/api/events/"+override.ID+"/ics")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if countOccurrences(string(body), "BEGIN:VEVENT") != 2 {
		t.Fatalf("expected the whole series even when addressed by the override's id, got:\n%s", body)
	}
}

func TestEventHandler_ICS_ScopeOccurrence_FlattensNonOverriddenOccurrence(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := env.events.Create(ctx, 1, "evt-1", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY;BYDAY=TU",
	})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	occurrenceStart := start.AddDate(0, 0, 7)
	resp := env.get(t, "/api/events/"+master.ID+"/ics?scope=occurrence&occurrenceStart="+occurrenceStart.Format(time.RFC3339))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if countOccurrences(text, "BEGIN:VEVENT") != 1 {
		t.Fatalf("expected exactly one flattened VEVENT, got:\n%s", text)
	}
	if containsAll(text, "UID:evt-1") {
		t.Fatalf("expected a fresh UID, not the series' own id, got:\n%s", text)
	}
	if containsAll(text, "RRULE") || containsAll(text, "RECURRENCE-ID") {
		t.Fatalf("expected no RRULE/RECURRENCE-ID on a flattened occurrence, got:\n%s", text)
	}
	if !containsAll(text, "DTSTART:20260609T090000") {
		t.Fatalf("expected DTSTART at the concrete occurrence start, got:\n%s", text)
	}
}

func TestEventHandler_ICS_ScopeOccurrence_SubstitutesOverrideFields(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := env.events.Create(ctx, 1, "evt-1", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY;BYDAY=TU",
	})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := start.AddDate(0, 0, 7)
	if _, err := env.events.Create(ctx, 1, "evt-1-override", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup (moved)", Start: recurrenceID.Add(2 * time.Hour), End: recurrenceID.Add(2*time.Hour + 30*time.Minute),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
	}); err != nil {
		t.Fatalf("create override: %v", err)
	}

	resp := env.get(t, "/api/events/"+master.ID+"/ics?scope=occurrence&occurrenceStart="+recurrenceID.Format(time.RFC3339))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !containsAll(text, "SUMMARY:Standup (moved)") {
		t.Fatalf("expected the override's own title, got:\n%s", text)
	}
	if countOccurrences(text, "BEGIN:VEVENT") != 1 {
		t.Fatalf("expected exactly one flattened VEVENT, got:\n%s", text)
	}
}

func TestEventHandler_ICS_ScopeOccurrence_InvalidOccurrenceStartIs404(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := env.events.Create(ctx, 1, "evt-1", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY;BYDAY=TU",
	})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	notAnOccurrence := start.Add(time.Hour)
	resp := env.get(t, "/api/events/"+master.ID+"/ics?scope=occurrence&occurrenceStart="+notAnOccurrence.Format(time.RFC3339))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestEventHandler_ICS_ScopeOccurrence_MissingOccurrenceStartIs400(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := env.events.Create(ctx, 1, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	resp := env.get(t, "/api/events/"+master.ID+"/ics?scope=occurrence")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestEventHandler_ICS_InvalidScopeIs400(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := env.events.Create(ctx, 1, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	resp := env.get(t, "/api/events/"+master.ID+"/ics?scope=bogus")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestEventHandler_ICS_RequiresAuth(t *testing.T) {
	env := newICSTestEnv(t)

	resp, err := http.Get(env.baseURL + "/api/events/evt-1/ics")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_ICS_EmitsNameColorAndEverySeries(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	if _, err := env.events.Create(ctx, 1, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create event: %v", err)
	}
	if _, err := env.events.Create(ctx, 1, "evt-2", service.EventWrite{CalendarID: env.calendarID, Title: "Retro", Start: start.Add(24 * time.Hour), End: end.Add(24 * time.Hour)}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	resp := env.get(t, "/api/calendars/"+env.calendarID+"/ics")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/calendar; charset=utf-8" {
		t.Fatalf("expected text/calendar content type, got %q", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !containsAll(text, "X-WR-CALNAME:Personal", "X-APPLE-CALENDAR-COLOR:#12809CFF") {
		t.Fatalf("expected name and color to survive export, got:\n%s", text)
	}
	if countOccurrences(text, "BEGIN:VEVENT") != 2 {
		t.Fatalf("expected both series, got:\n%s", text)
	}
}

func TestCalendarHandler_ICS_NotFound(t *testing.T) {
	env := newICSTestEnv(t)

	resp := env.get(t, "/api/calendars/does-not-exist/ics")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_ICSAll_ReturnsOneZipEntryPerCalendar(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	if _, err := env.events.Create(ctx, 1, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create event: %v", err)
	}
	secondCal, err := env.calendars.Create(ctx, 1, "22222222-2222-2222-2222-222222222222", service.CalendarWrite{Name: "Work", Color: "#E2483DFF"})
	if err != nil {
		t.Fatalf("create second calendar: %v", err)
	}
	if _, err := env.events.Create(ctx, 1, "evt-2", service.EventWrite{CalendarID: secondCal.ID, Title: "Retro", Start: start.Add(24 * time.Hour), End: end.Add(24 * time.Hour)}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	resp := env.get(t, "/api/calendars/ics")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("expected application/zip content type, got %q", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(zr.File))
	}

	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["Personal.ics"] || !names["Work.ics"] {
		t.Fatalf("expected entries named after each calendar, got %v", names)
	}
}

func TestCalendarHandler_ICSAll_RequiresAuth(t *testing.T) {
	env := newICSTestEnv(t)

	resp, err := http.Get(env.baseURL + "/api/calendars/ics")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func countOccurrences(s, substr string) int {
	return bytes.Count([]byte(s), []byte(substr))
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !bytes.Contains([]byte(s), []byte(sub)) {
			return false
		}
	}
	return true
}
