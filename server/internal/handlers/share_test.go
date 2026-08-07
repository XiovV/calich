package handlers

import (
	"bytes"
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

// shareTestServer is #100's REST fixture: two logged-in Users (owner and
// other) against a router carrying every Calendar and Share route.
type shareTestServer struct {
	baseURL                string
	ownerToken, otherToken string
	calendarID             string
	otherUserID            int64
	calendars              *service.CalendarService
}

func newShareTestServer(t *testing.T) shareTestServer {
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
	// AccountService hashes properly via bcrypt; a raw repository.Create
	// like the one above stores "hash" verbatim, which Login's
	// bcrypt.CompareHashAndPassword would reject — so "other" logs in
	// through an Admin-issued temporary password instead.
	calendarRepo := repository.NewCalendarRepository(sqlDB)
	shareRepo := repository.NewCalendarShareRepository(sqlDB)
	calendars := service.NewCalendarService(calendarRepo, shareRepo, users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB))
	accounts := service.NewAccountService(sqlDB, users, sessions, calendarRepo, shareRepo, calendars)
	other, err := accounts.ResetPassword(ctx, 2, "temp-password")
	if err != nil {
		t.Fatalf("reset other user's password: %v", err)
	}
	if other.Username != "other" {
		t.Fatalf("expected id 2 to be %q, got %q", "other", other.Username)
	}

	ownerLogin, err := auth.Login(ctx, "owner", "hunter2")
	if err != nil {
		t.Fatalf("owner login: %v", err)
	}
	otherLogin, err := auth.Login(ctx, "other", "temp-password")
	if err != nil {
		t.Fatalf("other login: %v", err)
	}

	events := service.NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendars)
	imports := service.NewImportService(events, calendars)
	subscriptions := service.NewSubscribeService(events, calendars, 0, service.WithHTTPClient(&http.Client{}))
	calendarHandler := NewCalendarHandler(calendars, events, imports, subscriptions)

	r := chi.NewRouter()
	r.Route("/api/calendars", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Post("/", calendarHandler.Create)
		r.Get("/", calendarHandler.List)
		r.Get("/{id}", calendarHandler.Get)
		r.Get("/{id}/shares", calendarHandler.ListShares)
		r.Post("/{id}/shares", calendarHandler.Share)
		r.Delete("/{id}/shares/{userId}", calendarHandler.RevokeShare)
		r.Post("/{id}/leave", calendarHandler.LeaveShare)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp := createCalendar(t, srv.URL, ownerLogin.AccessToken, "11111111-1111-1111-1111-111111111111", "Family", "#12809CFF")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create calendar: expected 201, got %d", resp.StatusCode)
	}
	var created calendarResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created calendar: %v", err)
	}
	resp.Body.Close()

	return shareTestServer{baseURL: srv.URL, ownerToken: ownerLogin.AccessToken, otherToken: otherLogin.AccessToken, calendarID: created.ID, otherUserID: other.ID, calendars: calendars}
}

func doJSON(t *testing.T, method, url, accessToken string, body any) *http.Response {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

func TestCalendarHandler_Share(t *testing.T) {
	s := newShareTestServer(t)

	resp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Username: "other", Role: repository.RoleEditor})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got shareResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Username != "other" || got.Role != repository.RoleEditor {
		t.Fatalf("unexpected share: %+v", got)
	}
}

// TestCalendarHandler_List_CarriesAccess covers "Calendar list responses
// carry the caller's resolved Access" (#100's acceptance criteria).
func TestCalendarHandler_List_CarriesAccess(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Username: "other", Role: repository.RoleViewer})
	shareResp.Body.Close()
	if shareResp.StatusCode != http.StatusOK {
		t.Fatalf("share: expected 200, got %d", shareResp.StatusCode)
	}

	ownerList, err := authenticatedGet(s.baseURL+"/api/calendars/", s.ownerToken)
	if err != nil {
		t.Fatalf("owner list: %v", err)
	}
	defer ownerList.Body.Close()
	var ownerCalendars []calendarResponse
	if err := json.NewDecoder(ownerList.Body).Decode(&ownerCalendars); err != nil {
		t.Fatalf("decode owner list: %v", err)
	}
	if len(ownerCalendars) != 1 || ownerCalendars[0].Access != "owner" {
		t.Fatalf("unexpected owner list: %+v", ownerCalendars)
	}

	otherList, err := authenticatedGet(s.baseURL+"/api/calendars/", s.otherToken)
	if err != nil {
		t.Fatalf("other list: %v", err)
	}
	defer otherList.Body.Close()
	var otherCalendars []calendarResponse
	if err := json.NewDecoder(otherList.Body).Decode(&otherCalendars); err != nil {
		t.Fatalf("decode other list: %v", err)
	}
	if len(otherCalendars) != 1 || otherCalendars[0].Access != "viewer" {
		t.Fatalf("unexpected shared-with list: %+v", otherCalendars)
	}
}

// TestCalendarHandler_List_And_Get_ResolveColorPerCaller covers ADR-0038's
// "effective colour returned with every Calendar": a User's own colour
// override — set here directly through the service, since the REST API
// exposes no dedicated endpoint for it; the override exists to serve
// CalDAV's native-client colour picker (ADR-0038) — is what List and Get
// report back to that User, while the Owner's own response stays the
// Calendar's own stored colour.
func TestCalendarHandler_List_And_Get_ResolveColorPerCaller(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Username: "other", Role: repository.RoleEditor})
	shareResp.Body.Close()

	if _, err := s.calendars.SetColorOverride(context.Background(), s.otherUserID, s.calendarID, "#654321"); err != nil {
		t.Fatalf("set color override: %v", err)
	}

	otherGet, err := authenticatedGet(s.baseURL+"/api/calendars/"+s.calendarID, s.otherToken)
	if err != nil {
		t.Fatalf("other get: %v", err)
	}
	defer otherGet.Body.Close()
	var otherCalendar calendarResponse
	if err := json.NewDecoder(otherGet.Body).Decode(&otherCalendar); err != nil {
		t.Fatalf("decode other get: %v", err)
	}
	if otherCalendar.Color != "#654321FF" {
		t.Fatalf("expected other's resolved colour to be their override, got %q", otherCalendar.Color)
	}

	otherList, err := authenticatedGet(s.baseURL+"/api/calendars/", s.otherToken)
	if err != nil {
		t.Fatalf("other list: %v", err)
	}
	defer otherList.Body.Close()
	var otherCalendars []calendarResponse
	if err := json.NewDecoder(otherList.Body).Decode(&otherCalendars); err != nil {
		t.Fatalf("decode other list: %v", err)
	}
	if len(otherCalendars) != 1 || otherCalendars[0].Color != "#654321FF" {
		t.Fatalf("expected other's resolved colour in the list to be their override, got %+v", otherCalendars)
	}

	ownerGet, err := authenticatedGet(s.baseURL+"/api/calendars/"+s.calendarID, s.ownerToken)
	if err != nil {
		t.Fatalf("owner get: %v", err)
	}
	defer ownerGet.Body.Close()
	var ownerCalendar calendarResponse
	if err := json.NewDecoder(ownerGet.Body).Decode(&ownerCalendar); err != nil {
		t.Fatalf("decode owner get: %v", err)
	}
	if ownerCalendar.Color != "#12809CFF" {
		t.Fatalf("expected the owner's resolved colour to stay the calendar's own, got %q", ownerCalendar.Color)
	}
}

func TestCalendarHandler_Share_NonOwnerRefused(t *testing.T) {
	s := newShareTestServer(t)

	resp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.otherToken, shareRequest{Username: "other", Role: repository.RoleViewer})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Share_UnknownUsername(t *testing.T) {
	s := newShareTestServer(t)

	resp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Username: "ghost", Role: repository.RoleViewer})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_ListShares(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Username: "other", Role: repository.RoleEditor})
	shareResp.Body.Close()

	resp, err := authenticatedGet(s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var shares []shareResponse
	if err := json.NewDecoder(resp.Body).Decode(&shares); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(shares) != 1 || shares[0].Username != "other" || shares[0].Role != repository.RoleEditor {
		t.Fatalf("unexpected shares: %+v", shares)
	}
}

func TestCalendarHandler_RevokeShare(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Username: "other", Role: repository.RoleEditor})
	shareResp.Body.Close()

	resp := doJSON(t, http.MethodDelete, s.baseURL+"/api/calendars/"+s.calendarID+"/shares/2", s.ownerToken, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	// Revoked: "other" can no longer even see the Calendar.
	getResp, err := authenticatedGet(s.baseURL+"/api/calendars/"+s.calendarID, s.otherToken)
	if err != nil {
		t.Fatalf("get after revoke: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after revoke, got %d", getResp.StatusCode)
	}
}

// TestCalendarHandler_LeaveShare covers "a User can leave a Calendar shared
// with them without involving the Owner" (#100's acceptance criteria).
func TestCalendarHandler_LeaveShare(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Username: "other", Role: repository.RoleViewer})
	shareResp.Body.Close()

	resp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/leave", s.otherToken, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	getResp, err := authenticatedGet(s.baseURL+"/api/calendars/"+s.calendarID, s.otherToken)
	if err != nil {
		t.Fatalf("get after leave: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after leaving, got %d", getResp.StatusCode)
	}
}
