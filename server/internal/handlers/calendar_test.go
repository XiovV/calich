package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

func newCalendarTestServer(t *testing.T) (baseURL string, accessToken string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	auth := service.NewAuthService(users, sessions, []byte("test-secret"), "alice", "hunter2")
	if err := auth.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	loginResult, err := auth.Login(context.Background(), "alice", "hunter2")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	calendars := service.NewCalendarService(repository.NewCalendarRepository(sqlDB))
	calendarHandler := NewCalendarHandler(calendars)

	r := chi.NewRouter()
	r.Route("/api/calendars", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Get("/", calendarHandler.List)
		r.Post("/", calendarHandler.Create)
		r.Get("/{id}", calendarHandler.Get)
		r.Patch("/{id}", calendarHandler.Update)
		r.Delete("/{id}", calendarHandler.Delete)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv.URL, loginResult.AccessToken
}

func TestCalendarHandler_CreateAndList(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)

	resp := createCalendar(t, baseURL, accessToken, "11111111-1111-1111-1111-111111111111", "Personal", "peacock")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var created calendarResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID != "11111111-1111-1111-1111-111111111111" || created.Name != "Personal" || created.Color != "peacock" {
		t.Fatalf("unexpected response: %+v", created)
	}

	listResp, err := authenticatedGet(baseURL+"/api/calendars/", accessToken)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer listResp.Body.Close()

	var calendars []calendarResponse
	if err := json.NewDecoder(listResp.Body).Decode(&calendars); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(calendars) != 1 {
		t.Fatalf("expected 1 calendar, got %d", len(calendars))
	}
}

func TestCalendarHandler_Create_RejectsInvalidColor(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)

	resp := createCalendar(t, baseURL, accessToken, "11111111-1111-1111-1111-111111111111", "Personal", "not-a-color")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Create_RejectsNonUUIDID(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)

	resp := createCalendar(t, baseURL, accessToken, "not-a-uuid", "Personal", "peacock")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Create_RequiresAuth(t *testing.T) {
	baseURL, _ := newCalendarTestServer(t)

	body, _ := json.Marshal(createCalendarRequest{ID: "11111111-1111-1111-1111-111111111111", Name: "Personal", Color: "peacock"})
	resp, err := http.Post(baseURL+"/api/calendars/", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_UpdateAndGet(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)

	createResp := createCalendar(t, baseURL, accessToken, "11111111-1111-1111-1111-111111111111", "Personal", "peacock")
	var created calendarResponse
	json.NewDecoder(createResp.Body).Decode(&created)

	updateResp := patchCalendar(t, baseURL, accessToken, created.ID, "Renamed", "tomato")
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", updateResp.StatusCode)
	}

	var updated calendarResponse
	json.NewDecoder(updateResp.Body).Decode(&updated)
	if updated.Name != "Renamed" || updated.Color != "tomato" {
		t.Fatalf("unexpected updated calendar: %+v", updated)
	}

	getResp, err := authenticatedGet(baseURL+"/api/calendars/"+created.ID, accessToken)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", getResp.StatusCode)
	}
}

func TestCalendarHandler_Update_NotFound(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)

	resp := patchCalendar(t, baseURL, accessToken, "does-not-exist", "Renamed", "tomato")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Delete(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)

	createResp := createCalendar(t, baseURL, accessToken, "11111111-1111-1111-1111-111111111111", "Personal", "peacock")
	var created calendarResponse
	json.NewDecoder(createResp.Body).Decode(&created)

	req, _ := http.NewRequest(http.MethodDelete, baseURL+"/api/calendars/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	getResp, err := authenticatedGet(baseURL+"/api/calendars/"+created.ID, accessToken)
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", getResp.StatusCode)
	}
}

func TestCalendarHandler_Delete_NotFound(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)

	req, _ := http.NewRequest(http.MethodDelete, baseURL+"/api/calendars/does-not-exist", nil)
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

func createCalendar(t *testing.T, baseURL, accessToken, id, name, color string) *http.Response {
	t.Helper()

	body, err := json.Marshal(createCalendarRequest{ID: id, Name: name, Color: color})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/calendars/", bytes.NewReader(body))
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

func patchCalendar(t *testing.T, baseURL, accessToken, id, name, color string) *http.Response {
	t.Helper()

	body, err := json.Marshal(updateCalendarRequest{Name: name, Color: color})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPatch, baseURL+"/api/calendars/"+id, bytes.NewReader(body))
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

func authenticatedGet(url, accessToken string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	return http.DefaultClient.Do(req)
}
