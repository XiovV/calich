package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/attachmentstore"
	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

// testMaxAttachmentSize and testMaxAttachmentsPerEvent are the
// MAX_ATTACHMENT_SIZE/MAX_ATTACHMENTS_PER_EVENT stand-ins this package's
// tests wire an ImportService with (ADR-0040), mirroring
// caldavserver/backend_test.go's own constants of the same name.
const (
	testMaxAttachmentSize      int64 = 25 << 20
	testMaxAttachmentsPerEvent       = 10
)

// newCalendarTestServer wires a calendar test server and returns the base
// URL, an access token for alice, and the id (as a string, ready for the
// X-Workspace-Id header) of the Workspace Bootstrap created for her (#155,
// ADR-0045).
func newCalendarTestServer(t *testing.T) (baseURL string, accessToken string, workspaceID string) {
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
	calendars := service.NewCalendarService(repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	auth := service.NewAuthService(users, sessions, workspaces, repository.NewWorkspaceInviteRepository(sqlDB), calendars, []byte("test-secret"), "alice", "alice@example.com", "hunter2", false)
	bootstrapUser, _, err := auth.Bootstrap(context.Background())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	userWorkspaces, err := workspaces.ListForUser(context.Background(), bootstrapUser.ID)
	if err != nil {
		t.Fatalf("list workspaces: %v", err)
	}
	if len(userWorkspaces) != 1 {
		t.Fatalf("expected alice to belong to exactly one workspace, got %d", len(userWorkspaces))
	}

	loginResult, err := auth.Login(context.Background(), "alice@example.com", "hunter2")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	events := service.NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendars, users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo, repository.NewGroupRepository(sqlDB), repository.NewNotificationRepository(sqlDB), nil)
	attachmentStore := attachmentstore.New(t.TempDir())
	imports := service.NewImportService(events, calendars, attachmentStore, testMaxAttachmentSize, testMaxAttachmentsPerEvent)
	// The address guard (#97, ADR-0032) would otherwise refuse every fetch in
	// this package's tests: the feed servers below are httptest.Server
	// instances on loopback, exactly what the guard exists to block.
	subscriptions := service.NewSubscribeService(events, calendars, 0, service.WithHTTPClient(&http.Client{}))
	calendarHandler := NewCalendarHandler(calendars, events, imports, subscriptions, attachmentStore)

	r := chi.NewRouter()
	r.Route("/api/calendars", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.With(httpauth.RequireWorkspace(workspaces)).Get("/", calendarHandler.List)
		r.With(httpauth.RequireWorkspace(workspaces)).Post("/", calendarHandler.Create)
		r.Get("/ics", calendarHandler.ICSAll)
		r.With(httpauth.RequireWorkspace(workspaces)).Post("/import", calendarHandler.Import)
		r.With(httpauth.RequireWorkspace(workspaces)).Post("/subscribe", calendarHandler.Subscribe)
		r.Get("/{id}", calendarHandler.Get)
		r.Patch("/{id}", calendarHandler.Update)
		r.Delete("/{id}", calendarHandler.Delete)
		r.Get("/{id}/ics", calendarHandler.ICS)
		r.Post("/{id}/refresh", calendarHandler.Refresh)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv.URL, loginResult.AccessToken, strconv.FormatInt(userWorkspaces[0].ID, 10)
}

func TestCalendarHandler_CreateAndList(t *testing.T) {
	baseURL, accessToken, workspaceID := newCalendarTestServer(t)

	resp := createCalendar(t, baseURL, accessToken, workspaceID, "11111111-1111-1111-1111-111111111111", "Personal", "#12809CFF")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var created calendarResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID != "11111111-1111-1111-1111-111111111111" || created.Name != "Personal" || created.Color != "#12809CFF" {
		t.Fatalf("unexpected response: %+v", created)
	}

	listResp, err := authenticatedGetWithWorkspace(baseURL+"/api/calendars/", accessToken, workspaceID)
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

func TestCalendarHandler_Create_NormalizesAnArbitraryColor(t *testing.T) {
	baseURL, accessToken, workspaceID := newCalendarTestServer(t)

	// A 3-digit hex with no swatch match anywhere in the enum this app used
	// to have (ADR-0029: any hex is valid, not just the 8 curated Swatches).
	resp := createCalendar(t, baseURL, accessToken, workspaceID, "11111111-1111-1111-1111-111111111111", "Personal", "#1af")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var created calendarResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Color != "#11AAFFFF" {
		t.Fatalf("expected the color to widen and canonicalize to #11AAFFFF, got %q", created.Color)
	}
}

func TestCalendarHandler_Create_RejectsInvalidColor(t *testing.T) {
	baseURL, accessToken, workspaceID := newCalendarTestServer(t)

	resp := createCalendar(t, baseURL, accessToken, workspaceID, "11111111-1111-1111-1111-111111111111", "Personal", "not-a-color")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Create_RejectsNonUUIDID(t *testing.T) {
	baseURL, accessToken, workspaceID := newCalendarTestServer(t)

	resp := createCalendar(t, baseURL, accessToken, workspaceID, "not-a-uuid", "Personal", "#12809CFF")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Create_RequiresAuth(t *testing.T) {
	baseURL, _, _ := newCalendarTestServer(t)

	body, _ := json.Marshal(createCalendarRequest{ID: "11111111-1111-1111-1111-111111111111", Name: "Personal", Color: "#12809CFF"})
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
	baseURL, accessToken, workspaceID := newCalendarTestServer(t)

	createResp := createCalendar(t, baseURL, accessToken, workspaceID, "11111111-1111-1111-1111-111111111111", "Personal", "#12809CFF")
	var created calendarResponse
	json.NewDecoder(createResp.Body).Decode(&created)

	updateResp := patchCalendar(t, baseURL, accessToken, created.ID, "Renamed", "#E2483DFF")
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", updateResp.StatusCode)
	}

	var updated calendarResponse
	json.NewDecoder(updateResp.Body).Decode(&updated)
	if updated.Name != "Renamed" || updated.Color != "#E2483DFF" {
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
	baseURL, accessToken, _ := newCalendarTestServer(t)

	resp := patchCalendar(t, baseURL, accessToken, "does-not-exist", "Renamed", "#E2483DFF")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Delete(t *testing.T) {
	baseURL, accessToken, workspaceID := newCalendarTestServer(t)

	createResp := createCalendar(t, baseURL, accessToken, workspaceID, "11111111-1111-1111-1111-111111111111", "Personal", "#12809CFF")
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
	baseURL, accessToken, _ := newCalendarTestServer(t)

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

func createCalendar(t *testing.T, baseURL, accessToken, workspaceID, id, name, color string) *http.Response {
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
	req.Header.Set("X-Workspace-Id", workspaceID)

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

// authenticatedGetWithWorkspace is authenticatedGet plus the X-Workspace-Id
// header the /calendars List route requires (#155, ADR-0045).
func authenticatedGetWithWorkspace(url, accessToken, workspaceID string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Workspace-Id", workspaceID)
	return http.DefaultClient.Do(req)
}
