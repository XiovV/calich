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

// reminderOverrideTestServer is #105's REST fixture: an owner and a viewer
// sharing one Calendar carrying one Event with a Reminder, against a router
// serving the Calendar, Share, and Event (including reminder-override)
// routes.
type reminderOverrideTestServer struct {
	baseURL                string
	ownerToken, otherToken string
	eventID                string
}

func newReminderOverrideTestServer(t *testing.T) reminderOverrideTestServer {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	auth := service.NewAuthService(users, sessions, []byte("test-secret"), "owner", "hunter2")
	ctx := context.Background()
	if _, _, err := auth.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := users.Create(ctx, "other", "hash", false); err != nil {
		t.Fatalf("create other user: %v", err)
	}

	calendarRepo := repository.NewCalendarRepository(sqlDB)
	shareRepo := repository.NewCalendarShareRepository(sqlDB)
	calendars := service.NewCalendarService(calendarRepo, shareRepo, users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB))
	appPasswords := service.NewAppPasswordService(repository.NewAppPasswordRepository(sqlDB), users)
	accounts := service.NewAccountService(sqlDB, users, sessions, calendarRepo, shareRepo, calendars, appPasswords)
	if _, err := accounts.ResetPassword(ctx, 2, "temp-password"); err != nil {
		t.Fatalf("reset other user's password: %v", err)
	}

	ownerLogin, err := auth.Login(ctx, "owner", "hunter2")
	if err != nil {
		t.Fatalf("owner login: %v", err)
	}
	otherLogin, err := auth.Login(ctx, "other", "temp-password")
	if err != nil {
		t.Fatalf("other login: %v", err)
	}

	events := service.NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendars, users, repository.NewAttachmentRepository(sqlDB))
	imports := service.NewImportService(events, calendars)
	subscriptions := service.NewSubscribeService(events, calendars, 0, service.WithHTTPClient(&http.Client{}))
	calendarHandler := NewCalendarHandler(calendars, events, imports, subscriptions)
	eventHandler := NewEventHandler(events)

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))

		r.Route("/calendars", func(r chi.Router) {
			r.Post("/", calendarHandler.Create)
			r.Post("/{id}/shares", calendarHandler.Share)
			r.Delete("/{id}/shares/{userId}", calendarHandler.RevokeShare)
			r.Post("/{id}/leave", calendarHandler.LeaveShare)
		})

		r.Route("/events", func(r chi.Router) {
			r.Post("/", eventHandler.Create)
			r.Get("/{id}", eventHandler.Get)
			r.Get("/{id}/reminder-override", eventHandler.GetReminderOverride)
			r.Put("/{id}/reminder-override", eventHandler.SetReminderOverride)
			r.Delete("/{id}/reminder-override", eventHandler.ClearReminderOverride)
		})
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	calResp := createCalendar(t, srv.URL, ownerLogin.AccessToken, "11111111-1111-1111-1111-111111111111", "Family", "#12809CFF")
	if calResp.StatusCode != http.StatusCreated {
		t.Fatalf("create calendar: expected 201, got %d", calResp.StatusCode)
	}
	var cal calendarResponse
	if err := json.NewDecoder(calResp.Body).Decode(&cal); err != nil {
		t.Fatalf("decode created calendar: %v", err)
	}
	calResp.Body.Close()

	shareResp := doJSON(t, http.MethodPost, srv.URL+"/api/calendars/"+cal.ID+"/shares", ownerLogin.AccessToken, shareRequest{Username: "other", Role: repository.RoleViewer})
	shareResp.Body.Close()
	if shareResp.StatusCode != http.StatusOK {
		t.Fatalf("share: expected 200, got %d", shareResp.StatusCode)
	}

	eventResp := doJSON(t, http.MethodPost, srv.URL+"/api/events/", ownerLogin.AccessToken, createEventRequest{
		ID:         "22222222-2222-2222-2222-222222222222",
		CalendarID: cal.ID,
		Title:      "Bin day",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T09:30:00Z",
		Reminders:  []reminderWire{{OffsetMinutes: 10, Channel: "notification"}},
	})
	if eventResp.StatusCode != http.StatusCreated {
		t.Fatalf("create event: expected 201, got %d", eventResp.StatusCode)
	}
	var event eventResponse
	if err := json.NewDecoder(eventResp.Body).Decode(&event); err != nil {
		t.Fatalf("decode created event: %v", err)
	}
	eventResp.Body.Close()

	return reminderOverrideTestServer{
		baseURL:    srv.URL,
		ownerToken: ownerLogin.AccessToken, otherToken: otherLogin.AccessToken,
		eventID: event.ID,
	}
}

// A Viewer can set a Reminder override for themselves (#105's acceptance
// criteria) — the write is accepted even though a Viewer can't otherwise
// write anything on the Calendar.
func TestEventHandler_SetReminderOverride_ViewerAllowed(t *testing.T) {
	s := newReminderOverrideTestServer(t)

	offset := 120
	resp := doJSON(t, http.MethodPut, s.baseURL+"/api/events/"+s.eventID+"/reminder-override", s.otherToken, reminderOverrideRequest{OffsetMinutes: &offset})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got reminderOverrideResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.OffsetMinutes == nil || *got.OffsetMinutes != 120 {
		t.Fatalf("unexpected override: %+v", got)
	}
}

// GET returns a zero-value response when the caller has never set an
// override — the Event's own Reminders apply unchanged.
func TestEventHandler_GetReminderOverride_ZeroValueWhenUnset(t *testing.T) {
	s := newReminderOverrideTestServer(t)

	resp, err := authenticatedGet(s.baseURL+"/api/events/"+s.eventID+"/reminder-override", s.otherToken)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got reminderOverrideResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Muted || got.OffsetMinutes != nil || got.Channel != nil {
		t.Fatalf("expected a zero-value override, got %+v", got)
	}
}

// GET returns exactly what was previously set via PUT — a client can read
// the current override back before changing just one field of it.
func TestEventHandler_GetReminderOverride_ReturnsWhatWasSet(t *testing.T) {
	s := newReminderOverrideTestServer(t)

	offset := 90
	setResp := doJSON(t, http.MethodPut, s.baseURL+"/api/events/"+s.eventID+"/reminder-override", s.otherToken, reminderOverrideRequest{OffsetMinutes: &offset})
	setResp.Body.Close()

	resp, err := authenticatedGet(s.baseURL+"/api/events/"+s.eventID+"/reminder-override", s.otherToken)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	var got reminderOverrideResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.OffsetMinutes == nil || *got.OffsetMinutes != 90 {
		t.Fatalf("unexpected override: %+v", got)
	}
}

func TestEventHandler_SetReminderOverride_Mute(t *testing.T) {
	s := newReminderOverrideTestServer(t)

	resp := doJSON(t, http.MethodPut, s.baseURL+"/api/events/"+s.eventID+"/reminder-override", s.otherToken, reminderOverrideRequest{Muted: true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got reminderOverrideResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Muted {
		t.Fatalf("expected muted override, got %+v", got)
	}
}

func TestEventHandler_ClearReminderOverride(t *testing.T) {
	s := newReminderOverrideTestServer(t)

	setResp := doJSON(t, http.MethodPut, s.baseURL+"/api/events/"+s.eventID+"/reminder-override", s.otherToken, reminderOverrideRequest{Muted: true})
	setResp.Body.Close()

	clearResp := doJSON(t, http.MethodDelete, s.baseURL+"/api/events/"+s.eventID+"/reminder-override", s.otherToken, nil)
	defer clearResp.Body.Close()
	if clearResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", clearResp.StatusCode)
	}
}

func TestEventHandler_SetReminderOverride_InvalidChannel(t *testing.T) {
	s := newReminderOverrideTestServer(t)

	invalid := "sms"
	resp := doJSON(t, http.MethodPut, s.baseURL+"/api/events/"+s.eventID+"/reminder-override", s.ownerToken, reminderOverrideRequest{Channel: &invalid})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// A stranger with no Access to the Event's Calendar gets not-found, matching
// every other Event route's convention.
func TestEventHandler_SetReminderOverride_StrangerGetsNotFound(t *testing.T) {
	s := newReminderOverrideTestServer(t)

	resp := doJSON(t, http.MethodPut, s.baseURL+"/api/events/nonexistent-event/reminder-override", s.ownerToken, reminderOverrideRequest{Muted: true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// Revoking the Viewer's Share clears their Reminder override too, so a
// later attempt to read it (indirectly, via re-setting after re-sharing)
// starts fresh rather than resurrecting the old preference.
func TestEventHandler_RevokeShare_ClearsReminderOverride(t *testing.T) {
	s := newReminderOverrideTestServer(t)

	setResp := doJSON(t, http.MethodPut, s.baseURL+"/api/events/"+s.eventID+"/reminder-override", s.otherToken, reminderOverrideRequest{Muted: true})
	setResp.Body.Close()

	revokeResp := doJSON(t, http.MethodDelete, s.baseURL+"/api/calendars/11111111-1111-1111-1111-111111111111/shares/2", s.ownerToken, nil)
	defer revokeResp.Body.Close()
	if revokeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", revokeResp.StatusCode)
	}

	// No longer shared — setting an override for the Event fails not-found.
	resp := doJSON(t, http.MethodPut, s.baseURL+"/api/events/"+s.eventID+"/reminder-override", s.otherToken, reminderOverrideRequest{Muted: true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after revoke, got %d", resp.StatusCode)
	}
}
