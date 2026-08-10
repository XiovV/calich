package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/attachmentstore"
	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/icalendar"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

// icsTestEnv wires both /api/events and /api/calendars (including their ICS
// routes) against one in-memory database, plus direct service access so
// tests can set up series/overrides without going through the JSON write
// endpoints.
type icsTestEnv struct {
	baseURL        string
	accessToken    string
	calendarID     string
	userID         int64
	workspaceID    int64
	events         *service.EventService
	calendars      *service.CalendarService
	attachments    *service.AttachmentService
	attachmentRepo *repository.AttachmentRepository
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
	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspaces := service.NewWorkspaceService(sqlDB, workspaceRepo, repository.NewWorkspaceInviteRepository(sqlDB), repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB))
	auth := service.NewAuthService(users, sessions, workspaces, repository.NewWorkspaceInviteRepository(sqlDB), []byte("test-secret"), "alice", "hunter2", false)
	bootstrapUser, _, err := auth.Bootstrap(context.Background())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	aliceWorkspaces, err := workspaces.ListForUser(context.Background(), bootstrapUser.ID)
	if err != nil {
		t.Fatalf("list workspaces: %v", err)
	}
	if len(aliceWorkspaces) != 1 {
		t.Fatalf("expected alice to belong to exactly one workspace, got %d", len(aliceWorkspaces))
	}
	workspaceID := aliceWorkspaces[0].ID

	loginResult, err := auth.Login(context.Background(), "alice", "hunter2")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	userID, err := auth.Authenticate(context.Background(), loginResult.AccessToken)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	calendarRepo := repository.NewCalendarRepository(sqlDB)
	calendars := service.NewCalendarService(calendarRepo, repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	cal, err := calendars.Create(context.Background(), userID, workspaceID, "11111111-1111-1111-1111-111111111111", service.CalendarWrite{Name: "Personal", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	attachmentRepo := repository.NewAttachmentRepository(sqlDB)
	events := service.NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendars, users, attachmentRepo, repository.NewAttendeeRepository(sqlDB), workspaceRepo)
	attachmentStore := attachmentstore.New(t.TempDir())
	attachments := service.NewAttachmentService(attachmentRepo, repository.NewEventRepository(sqlDB), calendars, events, attachmentStore, 10)
	eventHandler := NewEventHandler(events, attachmentStore)
	calendarHandler := NewCalendarHandler(calendars, events, service.NewImportService(events, calendars, attachmentStore, testMaxAttachmentSize, testMaxAttachmentsPerEvent), service.NewSubscribeService(events, calendars, 0), attachmentStore)

	r := chi.NewRouter()
	r.Route("/api/events", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Get("/{id}/ics", eventHandler.ICS)
		r.Get("/{id}/ics/oversized-attachments", eventHandler.ICSOversizedAttachments)
	})
	r.Route("/api/calendars", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Get("/ics", calendarHandler.ICSAll)
		r.Get("/ics/oversized-attachments", calendarHandler.ICSAllOversizedAttachments)
		r.Get("/{id}/ics", calendarHandler.ICS)
		r.Get("/{id}/ics/oversized-attachments", calendarHandler.ICSOversizedAttachments)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return icsTestEnv{baseURL: srv.URL, accessToken: loginResult.AccessToken, calendarID: cal.ID, userID: userID, workspaceID: workspaceID, events: events, calendars: calendars, attachments: attachments, attachmentRepo: attachmentRepo}
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

// A Subscribed Calendar offers no per-Calendar download (#90, ADR-0032): a
// frozen snapshot is the wrong artifact for something a Refresh will
// overwrite anyway.
func TestCalendarHandler_ICS_SubscribedCalendarIsForbidden(t *testing.T) {
	env := newICSTestEnv(t)

	sourceURL := "https://user:hunter2@example.com/feed.ics"
	subCalendar, err := env.calendars.Create(context.Background(), env.userID, env.workspaceID, "33333333-3333-3333-3333-333333333333", service.CalendarWrite{
		Name: "Feed", Color: "#123456FF", SourceURL: &sourceURL,
	})
	if err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}

	resp := env.get(t, "/api/calendars/"+subCalendar.ID+"/ics")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// Downloading an empty owned Calendar is a rejection, not a 500: go-ical's
// Encode refuses a childless VCALENDAR, and handing someone a file with
// nothing in it isn't useful anyway (#92).
func TestCalendarHandler_ICS_EmptyCalendar_ReturnsCalendarEmpty(t *testing.T) {
	env := newICSTestEnv(t)

	resp := env.get(t, "/api/calendars/"+env.calendarID+"/ics")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Code != "calendar_empty" {
		t.Fatalf("expected code %q, got %q", "calendar_empty", body.Error.Code)
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
	secondCal, err := env.calendars.Create(ctx, env.userID, env.workspaceID, "22222222-2222-2222-2222-222222222222", service.CalendarWrite{Name: "Work", Color: "#E2483DFF"})
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

// An empty owned Calendar must never break the whole export: it still gets
// its own .ics entry, carrying its name and color, and that entry is
// re-importable by this app's own importer reporting 0 events (#92).
func TestCalendarHandler_ICSAll_EmptyOwnedCalendar_StillGetsAnEntry(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	// env.calendarID ("Personal") is left with no events.
	secondCal, err := env.calendars.Create(ctx, env.userID, env.workspaceID, "22222222-2222-2222-2222-222222222222", service.CalendarWrite{Name: "Work", Color: "#E2483DFF"})
	if err != nil {
		t.Fatalf("create second calendar: %v", err)
	}
	if _, err := env.events.Create(ctx, 1, "evt-1", service.EventWrite{CalendarID: secondCal.ID, Title: "Retro", Start: start, End: end}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	resp := env.get(t, "/api/calendars/ics")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(zr.File))
	}

	var personalEntry *zip.File
	for _, f := range zr.File {
		if f.Name == "Personal.ics" {
			personalEntry = f
		}
	}
	if personalEntry == nil {
		t.Fatalf("expected a Personal.ics entry, got %v", zr.File)
	}

	rc, err := personalEntry.Open()
	if err != nil {
		t.Fatalf("open Personal.ics: %v", err)
	}
	defer rc.Close()
	entryBytes, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read Personal.ics: %v", err)
	}

	if !containsAll(string(entryBytes), "X-WR-CALNAME:Personal", "X-APPLE-CALENDAR-COLOR:#12809CFF") {
		t.Fatalf("expected name and color to survive export, got:\n%s", entryBytes)
	}
	if bytes.Contains(entryBytes, []byte("BEGIN:VEVENT")) {
		t.Fatalf("expected no VEVENTs, got:\n%s", entryBytes)
	}

	parsed, err := icalendar.ParseImportFile(bytes.NewReader(entryBytes), testMaxAttachmentSize, testMaxAttachmentsPerEvent)
	if err != nil {
		t.Fatalf("ParseImportFile on the empty entry: %v", err)
	}
	if len(parsed.Series) != 0 {
		t.Fatalf("expected 0 series, got %d", len(parsed.Series))
	}
	if parsed.CalendarName != "Personal" {
		t.Fatalf("expected calendar name %q, got %q", "Personal", parsed.CalendarName)
	}
}

// Bulk export contains no Subscribed Calendars (#90, ADR-0032): a frozen
// snapshot is the wrong artifact for a Subscription, whose Events a Refresh
// will overwrite anyway. Instead the archive lists its name and URL, with
// the URL's password masked (ADR-0032's "every surface that renders a
// Subscription URL must mask the password" carried to the export surface).
func TestCalendarHandler_ICSAll_ExcludesSubscribedCalendarsAndListsThemInsteadWithMaskedURL(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	if _, err := env.events.Create(ctx, 1, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	sourceURL := "https://user:hunter2@example.com/feed.ics"
	if _, err := env.calendars.Create(ctx, env.userID, env.workspaceID, "33333333-3333-3333-3333-333333333333", service.CalendarWrite{
		Name: "Feed", Color: "#123456FF", SourceURL: &sourceURL,
	}); err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}

	resp := env.get(t, "/api/calendars/ics")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}

	names := map[string]*zip.File{}
	for _, f := range zr.File {
		names[f.Name] = f
	}
	if _, ok := names["Feed.ics"]; ok {
		t.Fatalf("expected the subscribed calendar to have no .ics entry, got %v", names)
	}
	if _, ok := names["Personal.ics"]; !ok {
		t.Fatalf("expected the owned calendar's .ics entry, got %v", names)
	}

	listingFile, ok := names[subscriptionsZipEntry]
	if !ok {
		t.Fatalf("expected a %s entry listing subscriptions, got %v", subscriptionsZipEntry, names)
	}
	rc, err := listingFile.Open()
	if err != nil {
		t.Fatalf("open subscriptions entry: %v", err)
	}
	defer rc.Close()
	listing, _ := io.ReadAll(rc)

	if !containsAll(string(listing), "Feed:", "example.com/feed.ics") {
		t.Fatalf("expected the subscription's name and URL, got:\n%s", listing)
	}
	if bytes.Contains(listing, []byte("hunter2")) {
		t.Fatalf("expected the password to be masked, got:\n%s", listing)
	}
}

// No Subscriptions means no subscriptions.txt entry — the archive stays
// exactly what it was before #90 for an account with only owned Calendars.
func TestCalendarHandler_ICSAll_NoSubscriptions_OmitsListingEntry(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)
	if _, err := env.events.Create(ctx, 1, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	resp := env.get(t, "/api/calendars/ics")
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == subscriptionsZipEntry {
			t.Fatalf("expected no %s entry when there are no subscriptions", subscriptionsZipEntry)
		}
	}
}

// An instance with only Subscribed Calendars still produces a valid archive
// (#90, ADR-0032) — zero .ics entries, just the subscriptions listing.
func TestCalendarHandler_ICSAll_OnlySubscribedCalendars_StillProducesValidArchive(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()

	if err := env.calendars.Delete(ctx, 1, env.calendarID); err != nil {
		t.Fatalf("delete owned calendar: %v", err)
	}

	sourceURL := "https://example.com/feed.ics"
	if _, err := env.calendars.Create(ctx, env.userID, env.workspaceID, "33333333-3333-3333-3333-333333333333", service.CalendarWrite{
		Name: "Feed", Color: "#123456FF", SourceURL: &sourceURL,
	}); err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}

	resp := env.get(t, "/api/calendars/ics")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("expected a valid zip archive: %v", err)
	}
	if len(zr.File) != 1 || zr.File[0].Name != subscriptionsZipEntry {
		t.Fatalf("expected exactly one %s entry, got %v", subscriptionsZipEntry, zr.File)
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

// createFakeOversizedAttachment inserts an Attachment row claiming
// sizeBytes without writing that many bytes to disk (#134, ADR-0041): the
// inline ceiling is checked against the stored size before anything is
// opened, so a fake metadata-only row is enough to exercise "omitted, not
// inlined" without an actual 11MB fixture.
func createFakeOversizedAttachment(t *testing.T, env icsTestEnv, eventID, filename string, sizeBytes int64) {
	t.Helper()
	if _, err := env.attachmentRepo.Create(context.Background(), "fake-"+filename, eventID, &env.userID, filename, "application/octet-stream", sizeBytes); err != nil {
		t.Fatalf("create fake oversized attachment: %v", err)
	}
}

func TestEventHandler_ICS_InlinesAttachmentBytes(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := env.events.Create(ctx, env.userID, "evt-1", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup", Start: start, End: end,
	})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	if _, err := env.attachments.Upload(ctx, env.userID, master.ID, "notes.txt", "text/plain", strings.NewReader("hello world")); err != nil {
		t.Fatalf("upload attachment: %v", err)
	}

	resp := env.get(t, "/api/events/"+master.ID+"/ics")
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if !containsAll(text, "ENCODING=BASE64", "VALUE=BINARY", "FMTTYPE=text/plain", "FILENAME=notes.txt") {
		t.Fatalf("expected an inline ATTACH, got:\n%s", text)
	}
	if bytes.Contains(body, []byte("MANAGED-ID")) {
		t.Fatalf("expected no managed-attachment reference in an export, got:\n%s", text)
	}
}

func TestEventHandler_ICS_OmitsOversizedAttachment(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := env.events.Create(ctx, env.userID, "evt-1", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup", Start: start, End: end,
	})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	createFakeOversizedAttachment(t, env, master.ID, "huge.bin", maxImportUploadBytes+1)

	resp := env.get(t, "/api/events/"+master.ID+"/ics")
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if bytes.Contains(body, []byte("ATTACH")) {
		t.Fatalf("expected the oversized attachment to be omitted, got:\n%s", body)
	}
}

func TestEventHandler_ICSOversizedAttachments_ReportsOversized(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := env.events.Create(ctx, env.userID, "evt-1", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup", Start: start, End: end,
	})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	createFakeOversizedAttachment(t, env, master.ID, "huge.bin", maxImportUploadBytes+1)

	resp := env.get(t, "/api/events/"+master.ID+"/ics/oversized-attachments")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var summary exportSummaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if summary.Count != 1 || len(summary.Oversized) != 1 {
		t.Fatalf("expected exactly one oversized attachment, got %+v", summary)
	}
	got := summary.Oversized[0]
	if got.Filename != "huge.bin" || got.SizeBytes != maxImportUploadBytes+1 || got.EventTitle != "Standup" || got.EventID != master.ID {
		t.Fatalf("unexpected entry: %+v", got)
	}
}

func TestEventHandler_ICSOversizedAttachments_EmptyWhenNothingOversized(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := env.events.Create(ctx, env.userID, "evt-1", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup", Start: start, End: end,
	})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	if _, err := env.attachments.Upload(ctx, env.userID, master.ID, "notes.txt", "text/plain", strings.NewReader("hello")); err != nil {
		t.Fatalf("upload attachment: %v", err)
	}

	resp := env.get(t, "/api/events/"+master.ID+"/ics/oversized-attachments")
	defer resp.Body.Close()

	var summary exportSummaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if summary.Count != 0 || len(summary.Oversized) != 0 {
		t.Fatalf("expected no oversized attachments, got %+v", summary)
	}
}

func TestCalendarHandler_ICSOversizedAttachments_AggregatesAcrossEvents(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	evt1, err := env.events.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create evt-1: %v", err)
	}
	evt2, err := env.events.Create(ctx, env.userID, "evt-2", service.EventWrite{CalendarID: env.calendarID, Title: "Retro", Start: start.Add(24 * time.Hour), End: end.Add(24 * time.Hour)})
	if err != nil {
		t.Fatalf("create evt-2: %v", err)
	}
	createFakeOversizedAttachment(t, env, evt1.ID, "one.bin", maxImportUploadBytes+1)
	createFakeOversizedAttachment(t, env, evt2.ID, "two.bin", maxImportUploadBytes+2)

	resp := env.get(t, "/api/calendars/"+env.calendarID+"/ics/oversized-attachments")
	defer resp.Body.Close()

	var summary exportSummaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if summary.Count != 2 {
		t.Fatalf("expected 2 oversized attachments across the calendar, got %+v", summary)
	}
}

func TestCalendarHandler_ICSAllOversizedAttachments_ExcludesSubscribedCalendars(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := env.events.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	createFakeOversizedAttachment(t, env, master.ID, "huge.bin", maxImportUploadBytes+1)

	resp := env.get(t, "/api/calendars/ics/oversized-attachments")
	defer resp.Body.Close()

	var summary exportSummaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if summary.Count != 1 {
		t.Fatalf("expected 1 oversized attachment from the owned calendar, got %+v", summary)
	}
}

func TestCalendarHandler_ICS_InlinesAttachmentBytes(t *testing.T) {
	env := newICSTestEnv(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := env.events.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	if _, err := env.attachments.Upload(ctx, env.userID, master.ID, "notes.txt", "text/plain", strings.NewReader("hello")); err != nil {
		t.Fatalf("upload attachment: %v", err)
	}

	resp := env.get(t, "/api/calendars/"+env.calendarID+"/ics")
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if !containsAll(text, "ENCODING=BASE64", "FILENAME=notes.txt") {
		t.Fatalf("expected an inline ATTACH in the per-calendar export, got:\n%s", text)
	}
}
