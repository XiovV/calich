package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/attachmentstore"
	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

// decodedEvent is an eventResponse read back with its start/end parsed into
// instants, so assertions can compare them as times rather than strings.
//
// This lives in the test file because only tests ever decode an event
// response — the server exclusively writes them. Keeping it here stops
// handlers/event.go carrying a code path production never executes.
type decodedEvent struct {
	ID            string
	CalendarID    string
	Title         string
	Start         time.Time
	End           time.Time
	AllDay        bool
	Rrule         string
	ParentID      *string
	RecurrenceID  *time.Time
	Exdates       []time.Time
	Tzid          *string
	Reminders     []reminderWire
	Description   string
	Location      string
	Color         *string
	CreatedBy     *int64
	CreatedByName string
}

func (d *decodedEvent) UnmarshalJSON(data []byte) error {
	var wire eventResponse
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	start, err := parseEventTime(wire.Start, wire.AllDay)
	if err != nil {
		return fmt.Errorf("invalid start: %w", err)
	}
	end, err := parseEventTime(wire.End, wire.AllDay)
	if err != nil {
		return fmt.Errorf("invalid end: %w", err)
	}
	*d = decodedEvent{
		ID:            wire.ID,
		CalendarID:    wire.CalendarID,
		Title:         wire.Title,
		Start:         start,
		End:           end,
		AllDay:        wire.AllDay,
		Rrule:         wire.Rrule,
		ParentID:      wire.ParentID,
		RecurrenceID:  wire.RecurrenceID,
		Exdates:       wire.Exdates,
		Tzid:          wire.Tzid,
		Reminders:     wire.Reminders,
		Description:   wire.Description,
		Location:      wire.Location,
		Color:         wire.Color,
		CreatedBy:     wire.CreatedBy,
		CreatedByName: wire.CreatedByName,
	}
	return nil
}

func newEventTestServer(t *testing.T) (baseURL, accessToken, calendarID string) {
	t.Helper()
	baseURL, accessToken, calendarID, _, _, _, _ = newEventTestServerWithServices(t)
	return baseURL, accessToken, calendarID
}

// newEventTestServerWithServices is newEventTestServer plus the userID,
// workspaceID, and the raw CalendarService/EventService, for tests that need
// to seed state (e.g. a Subscribed Calendar, ADR-0032) no REST endpoint can
// produce.
func newEventTestServerWithServices(t *testing.T) (baseURL, accessToken, calendarID string, userID int64, workspaceID int64, calendars *service.CalendarService, events *service.EventService) {
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
	calendarRepo := repository.NewCalendarRepository(sqlDB)
	calendars = service.NewCalendarService(calendarRepo, repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	auth := service.NewAuthService(users, sessions, workspaces, repository.NewWorkspaceInviteRepository(sqlDB), calendars, []byte("test-secret"), "alice", "alice@example.com", "hunter2", false)
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
	workspaceID = aliceWorkspaces[0].ID

	loginResult, err := auth.Login(context.Background(), "alice@example.com", "hunter2")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	userID, err = auth.Authenticate(context.Background(), loginResult.AccessToken)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	cal, err := calendars.Create(context.Background(), userID, workspaceID, "11111111-1111-1111-1111-111111111111", service.CalendarWrite{Name: "Personal", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	events = service.NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendars, users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo)
	eventHandler := NewEventHandler(events, attachmentstore.New(t.TempDir()))

	r := chi.NewRouter()
	r.Route("/api/events", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Get("/", eventHandler.List)
		r.Post("/", eventHandler.Create)
		r.Get("/{id}", eventHandler.Get)
		r.Patch("/{id}", eventHandler.Update)
		r.Delete("/{id}", eventHandler.Delete)
		r.Post("/{id}/exceptions", eventHandler.AddException)
		r.Post("/{id}/reparent", eventHandler.Reparent)
		r.Get("/{id}/ics", eventHandler.ICS)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv.URL, loginResult.AccessToken, cal.ID, userID, workspaceID, calendars, events
}

func TestEventHandler_CreateAndList(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	resp := createEvent(t, baseURL, accessToken, "22222222-2222-2222-2222-222222222222", calendarID, "Standup", "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var created decodedEvent
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Title != "Standup" || created.CalendarID != calendarID {
		t.Fatalf("unexpected response: %+v", created)
	}

	listResp, err := authenticatedGet(baseURL+"/api/events/", accessToken)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer listResp.Body.Close()

	var events []decodedEvent
	if err := json.NewDecoder(listResp.Body).Decode(&events); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

// The wire format for an all-day Event's start/end is a date-only string,
// never an RFC3339 instant, so a viewer in any timezone sees the same date
// (ADR-0017).
func TestEventHandler_Create_RoundTripsAllDayAsDateOnly(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	rawBody := `{"id":"22222222-2222-2222-2222-222222222222","calendarId":"` + calendarID + `","title":"Holiday","start":"2026-08-04","end":"2026-08-05","allDay":true}`
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/events/", bytes.NewReader([]byte(rawBody)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	rawResponse, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var wire map[string]any
	if err := json.Unmarshal(rawResponse, &wire); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if wire["start"] != "2026-08-04" || wire["end"] != "2026-08-05" {
		t.Fatalf("expected date-only start/end on the wire, got %+v", wire)
	}
	if wire["allDay"] != true {
		t.Fatalf("expected allDay to round-trip true, got %+v", wire)
	}

	var created decodedEvent
	if err := json.Unmarshal(rawResponse, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !created.AllDay {
		t.Fatalf("expected created event to be all-day, got %+v", created)
	}
	if !created.Start.Equal(time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected start 2026-08-04, got %v", created.Start)
	}
	if !created.End.Equal(time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected end 2026-08-05, got %v", created.End)
	}
}

// A created Event with no tzid is a Floating Event (ADR-0019): the field is
// absent on the wire, and start/end are always emitted as a canonical UTC
// "…Z" instant regardless of the offset the client sent.
func TestEventHandler_Create_DefaultsToFloatingAndCanonicalUTC(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	rawBody := `{"id":"22222222-2222-2222-2222-222222222222","calendarId":"` + calendarID + `","title":"Standup","start":"2026-01-01T10:00:00+01:00","end":"2026-01-01T11:00:00+01:00"}`
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/events/", bytes.NewReader([]byte(rawBody)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var wire map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if _, present := wire["tzid"]; present {
		t.Fatalf("expected tzid to be absent for a Floating Event, got %+v", wire)
	}
	if wire["start"] != "2026-01-01T09:00:00Z" || wire["end"] != "2026-01-01T10:00:00Z" {
		t.Fatalf("expected canonical UTC start/end, got %+v", wire)
	}
}

// tzid must persist through create/update and round-trip on list/get
// (ADR-0019).
func TestEventHandler_RoundTripsTzid(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	zone := "Europe/Berlin"
	createResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:         "22222222-2222-2222-2222-222222222222",
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T10:00:00Z",
		Tzid:       &zone,
	})
	var created decodedEvent
	json.NewDecoder(createResp.Body).Decode(&created)
	if created.Tzid == nil || *created.Tzid != zone {
		t.Fatalf("expected created tzid %q, got %+v", zone, created)
	}

	getResp, err := authenticatedGet(baseURL+"/api/events/"+created.ID, accessToken)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer getResp.Body.Close()
	var fetched decodedEvent
	json.NewDecoder(getResp.Body).Decode(&fetched)
	if fetched.Tzid == nil || *fetched.Tzid != zone {
		t.Fatalf("expected fetched tzid %q, got %+v", zone, fetched)
	}

	updateBody, _ := json.Marshal(updateEventRequest{
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T10:00:00Z",
		Tzid:       nil,
	})
	updateReq, _ := http.NewRequest(http.MethodPatch, baseURL+"/api/events/"+created.ID, bytes.NewReader(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("Authorization", "Bearer "+accessToken)
	updateResp, err := http.DefaultClient.Do(updateReq)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	defer updateResp.Body.Close()
	var updated decodedEvent
	json.NewDecoder(updateResp.Body).Decode(&updated)
	if updated.Tzid != nil {
		t.Fatalf("expected update to clear tzid back to Floating, got %+v", updated)
	}
}

// description and location must persist through create/update and round-trip
// on get (#61).
func TestEventHandler_RoundTripsDescriptionAndLocation(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	createResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:          "22222222-2222-2222-2222-222222222222",
		CalendarID:  calendarID,
		Title:       "Standup",
		Start:       "2026-01-01T09:00:00Z",
		End:         "2026-01-01T10:00:00Z",
		Description: "Daily sync",
		Location:    "Room 4",
	})
	var created decodedEvent
	json.NewDecoder(createResp.Body).Decode(&created)
	if created.Description != "Daily sync" || created.Location != "Room 4" {
		t.Fatalf("expected created description/location to round-trip, got %+v", created)
	}

	getResp, err := authenticatedGet(baseURL+"/api/events/"+created.ID, accessToken)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer getResp.Body.Close()
	var fetched decodedEvent
	json.NewDecoder(getResp.Body).Decode(&fetched)
	if fetched.Description != "Daily sync" || fetched.Location != "Room 4" {
		t.Fatalf("expected fetched description/location to round-trip, got %+v", fetched)
	}

	updateBody, _ := json.Marshal(updateEventRequest{
		CalendarID:  calendarID,
		Title:       "Standup",
		Start:       "2026-01-01T09:00:00Z",
		End:         "2026-01-01T10:00:00Z",
		Description: "Updated sync notes",
		Location:    "Room 5",
	})
	updateReq, _ := http.NewRequest(http.MethodPatch, baseURL+"/api/events/"+created.ID, bytes.NewReader(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("Authorization", "Bearer "+accessToken)
	updateResp, err := http.DefaultClient.Do(updateReq)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	defer updateResp.Body.Close()
	var updated decodedEvent
	json.NewDecoder(updateResp.Body).Decode(&updated)
	if updated.Description != "Updated sync notes" || updated.Location != "Room 5" {
		t.Fatalf("expected updated description/location to round-trip, got %+v", updated)
	}
}

// TestEventHandler_RoundTripsCreatorAttribution is #118's REST-facing check:
// create, get and list all carry the creator's id and username, resolved
// from the accompanying repository.User rather than dropped by
// toEventResponse.
func TestEventHandler_RoundTripsCreatorAttribution(t *testing.T) {
	baseURL, accessToken, calendarID, userID, _, _, _ := newEventTestServerWithServices(t)

	createResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:         "22222222-2222-2222-2222-222222222222",
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T10:00:00Z",
	})
	var created decodedEvent
	json.NewDecoder(createResp.Body).Decode(&created)
	if created.CreatedBy == nil || *created.CreatedBy != userID || created.CreatedByName != "alice" {
		t.Fatalf("expected creator attribution to alice (%d), got %+v", userID, created)
	}

	getResp, err := authenticatedGet(baseURL+"/api/events/"+created.ID, accessToken)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer getResp.Body.Close()
	var fetched decodedEvent
	json.NewDecoder(getResp.Body).Decode(&fetched)
	if fetched.CreatedBy == nil || *fetched.CreatedBy != userID || fetched.CreatedByName != "alice" {
		t.Fatalf("expected fetched creator attribution to alice (%d), got %+v", userID, fetched)
	}

	listResp, err := authenticatedGet(baseURL+"/api/events/", accessToken)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer listResp.Body.Close()
	var list []decodedEvent
	json.NewDecoder(listResp.Body).Decode(&list)
	if len(list) != 1 || list[0].CreatedBy == nil || *list[0].CreatedBy != userID || list[0].CreatedByName != "alice" {
		t.Fatalf("expected listed creator attribution to alice (%d), got %+v", userID, list)
	}
}

func TestEventHandler_Create_RoundTripsRrule(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	body, _ := json.Marshal(createEventRequest{
		ID:         "22222222-2222-2222-2222-222222222222",
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T10:00:00Z",
		Rrule:      "FREQ=WEEKLY;BYDAY=TH",
	})

	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/events/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	var created decodedEvent
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Rrule != "FREQ=WEEKLY;BYDAY=TH" {
		t.Fatalf("expected rrule to round-trip, got %q", created.Rrule)
	}
}

func TestEventHandler_List_FiltersByFromTo(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	createEvent(t, baseURL, accessToken, "11111111-0000-0000-0000-000000000001", calendarID, "January", "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	createEvent(t, baseURL, accessToken, "11111111-0000-0000-0000-000000000002", calendarID, "February", "2026-02-01T09:00:00Z", "2026-02-01T10:00:00Z")

	url := baseURL + "/api/events/?from=2026-01-15T00:00:00Z&to=2026-03-01T00:00:00Z"
	resp, err := authenticatedGet(url, accessToken)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer resp.Body.Close()

	var events []decodedEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 1 || events[0].Title != "February" {
		t.Fatalf("expected only the february event, got %+v", events)
	}
}

func TestEventHandler_List_RejectsInvalidFromParam(t *testing.T) {
	baseURL, accessToken, _ := newEventTestServer(t)

	resp, err := authenticatedGet(baseURL+"/api/events/?from=not-a-timestamp", accessToken)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestEventHandler_Create_RejectsEmptyTitle(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	resp := createEvent(t, baseURL, accessToken, "22222222-2222-2222-2222-222222222222", calendarID, "  ", "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestEventHandler_Create_RejectsEndBeforeStart(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	resp := createEvent(t, baseURL, accessToken, "22222222-2222-2222-2222-222222222222", calendarID, "Standup", "2026-01-01T10:00:00Z", "2026-01-01T09:00:00Z")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestEventHandler_Create_RejectsUnknownCalendar(t *testing.T) {
	baseURL, accessToken, _ := newEventTestServer(t)

	resp := createEvent(t, baseURL, accessToken, "22222222-2222-2222-2222-222222222222", "does-not-exist", "Standup", "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// A Subscribed Calendar's Events are written only by Refresh's bypass
// (ADR-0032); every REST write against one names the reason as a 403, not a
// plain validation 400.
func TestEventHandler_Create_RejectsSubscribedCalendarWith403(t *testing.T) {
	baseURL, accessToken, _, userID, workspaceID, calendars, _ := newEventTestServerWithServices(t)

	sourceURL := "https://example.com/feed.ics"
	subCalendar, err := calendars.Create(context.Background(), userID, workspaceID, "33333333-3333-3333-3333-333333333333", service.CalendarWrite{
		Name: "Feed", Color: "#123456FF", SourceURL: &sourceURL,
	})
	if err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}

	resp := createEvent(t, baseURL, accessToken, "22222222-2222-2222-2222-222222222222", subCalendar.ID, "Standup", "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestEventHandler_Create_RequiresAuth(t *testing.T) {
	baseURL, _, calendarID := newEventTestServer(t)

	body, _ := json.Marshal(createEventRequest{ID: "22222222-2222-2222-2222-222222222222", CalendarID: calendarID, Title: "Standup"})
	resp, err := http.Post(baseURL+"/api/events/", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestEventHandler_UpdateAndGet(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	createResp := createEvent(t, baseURL, accessToken, "22222222-2222-2222-2222-222222222222", calendarID, "Standup", "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	var created decodedEvent
	json.NewDecoder(createResp.Body).Decode(&created)

	updateResp := patchEvent(t, baseURL, accessToken, created.ID, calendarID, "Renamed", "2026-01-01T11:00:00Z", "2026-01-01T12:00:00Z")
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", updateResp.StatusCode)
	}

	var updated decodedEvent
	json.NewDecoder(updateResp.Body).Decode(&updated)
	if updated.Title != "Renamed" {
		t.Fatalf("unexpected updated event: %+v", updated)
	}

	getResp, err := authenticatedGet(baseURL+"/api/events/"+created.ID, accessToken)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", getResp.StatusCode)
	}
}

func TestEventHandler_Update_NotFound(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	resp := patchEvent(t, baseURL, accessToken, "does-not-exist", calendarID, "Renamed", "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// An unknown calendarId is a 400 (bad input), distinct from an unknown event
// id being a 404 (missing resource) — both surface as service.ErrCalendarNotFound
// vs. repository.ErrNotFound respectively, and must map to different statuses.
func TestEventHandler_Update_RejectsUnknownCalendarWith400(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	createResp := createEvent(t, baseURL, accessToken, "22222222-2222-2222-2222-222222222222", calendarID, "Standup", "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	var created decodedEvent
	json.NewDecoder(createResp.Body).Decode(&created)

	resp := patchEvent(t, baseURL, accessToken, created.ID, "does-not-exist", "Renamed", "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestEventHandler_Delete(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	createResp := createEvent(t, baseURL, accessToken, "22222222-2222-2222-2222-222222222222", calendarID, "Standup", "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	var created decodedEvent
	json.NewDecoder(createResp.Body).Decode(&created)

	req, _ := http.NewRequest(http.MethodDelete, baseURL+"/api/events/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	getResp, err := authenticatedGet(baseURL+"/api/events/"+created.ID, accessToken)
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", getResp.StatusCode)
	}
}

func TestEventHandler_Delete_NotFound(t *testing.T) {
	baseURL, accessToken, _ := newEventTestServer(t)

	req, _ := http.NewRequest(http.MethodDelete, baseURL+"/api/events/does-not-exist", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func createEvent(t *testing.T, baseURL, accessToken, id, calendarID, title, start, end string) *http.Response {
	t.Helper()

	body, err := json.Marshal(createEventRequest{ID: id, CalendarID: calendarID, Title: title, Start: start, End: end})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/events/", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

func patchEvent(t *testing.T, baseURL, accessToken, id, calendarID, title, start, end string) *http.Response {
	t.Helper()

	body, err := json.Marshal(updateEventRequest{CalendarID: calendarID, Title: title, Start: start, End: end})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPatch, baseURL+"/api/events/"+id, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	return resp
}

func postJSON(t *testing.T, baseURL, accessToken, path string, body any) *http.Response {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

func TestEventHandler_CreateOverride(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	masterResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:         "22222222-2222-2222-2222-222222222222",
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T09:30:00Z",
		Rrule:      "FREQ=DAILY",
	})
	var master decodedEvent
	json.NewDecoder(masterResp.Body).Decode(&master)

	recurrenceID := mustParseRFC3339(t, "2026-01-02T09:00:00Z")
	overrideResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:           "33333333-3333-3333-3333-333333333333",
		CalendarID:   calendarID,
		Title:        "Standup (moved)",
		Start:        "2026-01-02T10:00:00Z",
		End:          "2026-01-02T10:30:00Z",
		ParentID:     &master.ID,
		RecurrenceID: &recurrenceID,
	})
	if overrideResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", overrideResp.StatusCode)
	}

	var override decodedEvent
	json.NewDecoder(overrideResp.Body).Decode(&override)
	if override.ParentID == nil || *override.ParentID != master.ID {
		t.Fatalf("expected override parentId %q, got %+v", master.ID, override)
	}
	if override.RecurrenceID == nil || !override.RecurrenceID.Equal(recurrenceID) {
		t.Fatalf("expected override recurrenceId %v, got %+v", recurrenceID, override)
	}
}

func TestEventHandler_CreateOverride_RejectsOwnRrule(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	masterResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:         "22222222-2222-2222-2222-222222222222",
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T09:30:00Z",
		Rrule:      "FREQ=DAILY",
	})
	var master decodedEvent
	json.NewDecoder(masterResp.Body).Decode(&master)

	recurrenceID := mustParseRFC3339(t, "2026-01-02T09:00:00Z")
	resp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:           "33333333-3333-3333-3333-333333333333",
		CalendarID:   calendarID,
		Title:        "Standup (moved)",
		Start:        "2026-01-02T10:00:00Z",
		End:          "2026-01-02T10:30:00Z",
		Rrule:        "FREQ=DAILY",
		ParentID:     &master.ID,
		RecurrenceID: &recurrenceID,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestEventHandler_AddException_ShowsUpAsExdateOnList(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	masterResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:         "22222222-2222-2222-2222-222222222222",
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T09:30:00Z",
		Rrule:      "FREQ=DAILY",
	})
	var master decodedEvent
	json.NewDecoder(masterResp.Body).Decode(&master)

	occurrenceStart := mustParseRFC3339(t, "2026-01-02T09:00:00Z")
	excResp := postJSON(t, baseURL, accessToken, "/api/events/"+master.ID+"/exceptions", createExceptionRequest{
		OccurrenceStart: occurrenceStart,
	})
	if excResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", excResp.StatusCode)
	}

	listResp, err := authenticatedGet(baseURL+"/api/events/", accessToken)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer listResp.Body.Close()

	var events []decodedEvent
	json.NewDecoder(listResp.Body).Decode(&events)
	if len(events) != 1 || len(events[0].Exdates) != 1 || !events[0].Exdates[0].Equal(occurrenceStart) {
		t.Fatalf("expected the master to carry the exdate, got %+v", events)
	}
}

func TestEventHandler_AddException_RejectsNonRecurringParent(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	createResp := createEvent(t, baseURL, accessToken, "22222222-2222-2222-2222-222222222222", calendarID, "Standup", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z")
	var created decodedEvent
	json.NewDecoder(createResp.Body).Decode(&created)

	resp := postJSON(t, baseURL, accessToken, "/api/events/"+created.ID+"/exceptions", createExceptionRequest{
		OccurrenceStart: mustParseRFC3339(t, "2026-01-01T09:00:00Z"),
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestEventHandler_Reparent_MovesOverridesAndExceptions(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	oldMasterResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:         "22222222-2222-2222-2222-222222222222",
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T09:30:00Z",
		Rrule:      "FREQ=DAILY",
	})
	var oldMaster decodedEvent
	json.NewDecoder(oldMasterResp.Body).Decode(&oldMaster)

	newMasterResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:         "44444444-4444-4444-4444-444444444444",
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-03T09:00:00Z",
		End:        "2026-01-03T09:30:00Z",
		Rrule:      "FREQ=DAILY",
	})
	var newMaster decodedEvent
	json.NewDecoder(newMasterResp.Body).Decode(&newMaster)

	recurrenceID := mustParseRFC3339(t, "2026-01-03T09:00:00Z")
	overrideResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:           "33333333-3333-3333-3333-333333333333",
		CalendarID:   calendarID,
		Title:        "Standup (moved)",
		Start:        "2026-01-03T10:00:00Z",
		End:          "2026-01-03T10:30:00Z",
		ParentID:     &oldMaster.ID,
		RecurrenceID: &recurrenceID,
	})
	var override decodedEvent
	json.NewDecoder(overrideResp.Body).Decode(&override)

	reparentResp := postJSON(t, baseURL, accessToken, "/api/events/"+oldMaster.ID+"/reparent", reparentRequest{
		NewParentID: newMaster.ID,
		FromStart:   mustParseRFC3339(t, "2026-01-03T00:00:00Z"),
	})
	if reparentResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", reparentResp.StatusCode)
	}

	getResp, err := authenticatedGet(baseURL+"/api/events/"+override.ID, accessToken)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer getResp.Body.Close()

	var reparented decodedEvent
	json.NewDecoder(getResp.Body).Decode(&reparented)
	if reparented.ParentID == nil || *reparented.ParentID != newMaster.ID {
		t.Fatalf("expected override reparented to %q, got %+v", newMaster.ID, reparented)
	}
}

func TestEventHandler_UpdateOverride_RejectsOwnRrule(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	masterResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:         "22222222-2222-2222-2222-222222222222",
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T09:30:00Z",
		Rrule:      "FREQ=DAILY",
	})
	var master decodedEvent
	json.NewDecoder(masterResp.Body).Decode(&master)

	recurrenceID := mustParseRFC3339(t, "2026-01-02T09:00:00Z")
	overrideResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:           "33333333-3333-3333-3333-333333333333",
		CalendarID:   calendarID,
		Title:        "Standup (moved)",
		Start:        "2026-01-02T10:00:00Z",
		End:          "2026-01-02T10:30:00Z",
		ParentID:     &master.ID,
		RecurrenceID: &recurrenceID,
	})
	var override decodedEvent
	json.NewDecoder(overrideResp.Body).Decode(&override)

	body, _ := json.Marshal(updateEventRequest{CalendarID: calendarID, Title: "Standup (moved)", Start: "2026-01-02T10:00:00Z", End: "2026-01-02T10:30:00Z", Rrule: "FREQ=DAILY"})
	req, _ := http.NewRequest(http.MethodPatch, baseURL+"/api/events/"+override.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestEventHandler_Update_DiscardsOverridesOnRuleChange(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	masterResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:         "22222222-2222-2222-2222-222222222222",
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T09:30:00Z",
		Rrule:      "FREQ=DAILY",
	})
	var master decodedEvent
	json.NewDecoder(masterResp.Body).Decode(&master)

	recurrenceID := mustParseRFC3339(t, "2026-01-02T09:00:00Z")
	overrideResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:           "33333333-3333-3333-3333-333333333333",
		CalendarID:   calendarID,
		Title:        "Standup (moved)",
		Start:        "2026-01-02T10:00:00Z",
		End:          "2026-01-02T10:30:00Z",
		ParentID:     &master.ID,
		RecurrenceID: &recurrenceID,
	})
	var override decodedEvent
	json.NewDecoder(overrideResp.Body).Decode(&override)

	// patchEvent doesn't carry rrule, so issue a raw PATCH with the new rule.
	body, _ := json.Marshal(updateEventRequest{CalendarID: calendarID, Title: "Standup", Start: "2026-01-01T09:00:00Z", End: "2026-01-01T09:30:00Z", Rrule: "FREQ=WEEKLY;BYDAY=TH"})
	req, _ := http.NewRequest(http.MethodPatch, baseURL+"/api/events/"+master.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if _, err := http.DefaultClient.Do(req); err != nil {
		t.Fatalf("patch: %v", err)
	}

	getResp, err := authenticatedGet(baseURL+"/api/events/"+override.ID, accessToken)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected the override to be discarded (404), got %d", getResp.StatusCode)
	}
}

func mustParseRFC3339(t *testing.T, value string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return ts
}

// The event API nests a reminders array on create and list, round-tripping
// the offset/channel pairs a client authored (ADR-0020).
func TestEventHandler_Create_RoundTripsReminders(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	createResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:         "22222222-2222-2222-2222-222222222222",
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T10:00:00Z",
		Reminders: []reminderWire{
			{OffsetMinutes: 10, Channel: "notification"},
			{OffsetMinutes: 1440, Channel: "email"},
		},
	})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", createResp.StatusCode)
	}

	var created decodedEvent
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(created.Reminders) != 2 {
		t.Fatalf("expected 2 reminders on create response, got %+v", created.Reminders)
	}

	listResp, err := authenticatedGet(baseURL+"/api/events/", accessToken)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer listResp.Body.Close()

	var events []decodedEvent
	if err := json.NewDecoder(listResp.Body).Decode(&events); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(events) != 1 || len(events[0].Reminders) != 2 {
		t.Fatalf("expected 2 reminders on list, got %+v", events)
	}
}

// Absent reminders means no Reminders, and the field is omitted from the
// response — not an empty array.
func TestEventHandler_Create_OmitsRemindersWhenAbsent(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	resp := createEvent(t, baseURL, accessToken, "22222222-2222-2222-2222-222222222222", calendarID, "Standup", "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if bytes.Contains(body, []byte("reminders")) {
		t.Fatalf("expected reminders to be omitted, got %s", body)
	}
}

func TestEventHandler_Create_RejectsInvalidReminderChannel(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	resp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:         "22222222-2222-2222-2222-222222222222",
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T10:00:00Z",
		Reminders:  []reminderWire{{OffsetMinutes: 10, Channel: "sms"}},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// PATCH replaces an Event's reminders array wholesale, on the same
// request/response contract as create (ADR-0020).
func TestEventHandler_Update_ReplacesRemindersWholesale(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	createResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:         "22222222-2222-2222-2222-222222222222",
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T10:00:00Z",
		Reminders:  []reminderWire{{OffsetMinutes: 10, Channel: "notification"}},
	})
	var created decodedEvent
	json.NewDecoder(createResp.Body).Decode(&created)

	body, _ := json.Marshal(updateEventRequest{
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T10:00:00Z",
		Reminders:  []reminderWire{{OffsetMinutes: 30, Channel: "email"}},
	})
	req, _ := http.NewRequest(http.MethodPatch, baseURL+"/api/events/"+created.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	updateResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", updateResp.StatusCode)
	}

	var updated decodedEvent
	if err := json.NewDecoder(updateResp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(updated.Reminders) != 1 || updated.Reminders[0].OffsetMinutes != 30 || updated.Reminders[0].Channel != "email" {
		t.Fatalf("expected reminders replaced wholesale, got %+v", updated.Reminders)
	}
}
