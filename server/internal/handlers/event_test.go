package handlers

import (
	"bytes"
	"context"
	"encoding/json"
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

func newEventTestServer(t *testing.T) (baseURL, accessToken, calendarID string) {
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
	cal, err := calendars.Create(context.Background(), userID, "11111111-1111-1111-1111-111111111111", "Personal", "peacock")
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	events := service.NewEventService(repository.NewEventRepository(sqlDB), calendars)
	eventHandler := NewEventHandler(events)

	r := chi.NewRouter()
	r.Route("/api/events", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Get("/", eventHandler.List)
		r.Post("/", eventHandler.Create)
		r.Get("/{id}", eventHandler.Get)
		r.Patch("/{id}", eventHandler.Update)
		r.Delete("/{id}", eventHandler.Delete)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv.URL, loginResult.AccessToken, cal.ID
}

func TestEventHandler_CreateAndList(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	resp := createEvent(t, baseURL, accessToken, "22222222-2222-2222-2222-222222222222", calendarID, "Standup", "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var created eventResponse
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

	var events []eventResponse
	if err := json.NewDecoder(listResp.Body).Decode(&events); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
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

	var events []eventResponse
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
	var created eventResponse
	json.NewDecoder(createResp.Body).Decode(&created)

	updateResp := patchEvent(t, baseURL, accessToken, created.ID, calendarID, "Renamed", "2026-01-01T11:00:00Z", "2026-01-01T12:00:00Z")
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", updateResp.StatusCode)
	}

	var updated eventResponse
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
	var created eventResponse
	json.NewDecoder(createResp.Body).Decode(&created)

	resp := patchEvent(t, baseURL, accessToken, created.ID, "does-not-exist", "Renamed", "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestEventHandler_Delete(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	createResp := createEvent(t, baseURL, accessToken, "22222222-2222-2222-2222-222222222222", calendarID, "Standup", "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	var created eventResponse
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

	startTime, err := time.Parse(time.RFC3339, start)
	if err != nil {
		t.Fatalf("parse start: %v", err)
	}
	endTime, err := time.Parse(time.RFC3339, end)
	if err != nil {
		t.Fatalf("parse end: %v", err)
	}

	body, err := json.Marshal(createEventRequest{ID: id, CalendarID: calendarID, Title: title, Start: startTime, End: endTime})
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

	startTime, err := time.Parse(time.RFC3339, start)
	if err != nil {
		t.Fatalf("parse start: %v", err)
	}
	endTime, err := time.Parse(time.RFC3339, end)
	if err != nil {
		t.Fatalf("parse end: %v", err)
	}

	body, err := json.Marshal(updateEventRequest{CalendarID: calendarID, Title: title, Start: startTime, End: endTime})
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
