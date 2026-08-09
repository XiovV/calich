package handlers

import (
	"bytes"
	"context"
	"database/sql"
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

// accountHandlerTestServer bundles the HTTP surface a self-service
// AccountHandler test needs: Register/Login (to mint real Users, each with
// their own Workspace) and the self-service /api/account routes.
type accountHandlerTestServer struct {
	srv        *httptest.Server
	db         *sql.DB
	calendars  *service.CalendarService
	workspaces *service.WorkspaceService
}

func newAccountHandlerTestServer(t *testing.T) *accountHandlerTestServer {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	calendarRepo := repository.NewCalendarRepository(sqlDB)
	shareRepo := repository.NewCalendarShareRepository(sqlDB)
	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspaces := service.NewWorkspaceService(sqlDB, workspaceRepo, repository.NewWorkspaceInviteRepository(sqlDB))
	auth := service.NewAuthService(users, sessions, workspaces, repository.NewWorkspaceInviteRepository(sqlDB), []byte("test-secret"), "", "", true)
	calendars := service.NewCalendarService(calendarRepo, shareRepo, users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo)
	accounts := service.NewAccountService(sqlDB, users, sessions, calendarRepo, shareRepo, workspaceRepo, workspaces)

	authHandler := NewAuthHandler(auth, false)
	accountHandler := NewAccountHandler(accounts)

	r := chi.NewRouter()
	r.Post("/api/auth/login", authHandler.Login)
	r.Post("/api/auth/register", authHandler.Register)
	r.Route("/api/account", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Put("/disabled", accountHandler.SetDisabled)
		r.With(httpauth.RequireEnabledUser(auth)).Get("/delete-impact", accountHandler.DeleteImpact)
		r.With(httpauth.RequireEnabledUser(auth)).Delete("/", accountHandler.Delete)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &accountHandlerTestServer{srv: srv, db: sqlDB, calendars: calendars, workspaces: workspaces}
}

// register self-registers username and returns their access token, id, and
// sole Workspace id.
func (s *accountHandlerTestServer) register(t *testing.T, username string) (accessToken string, userID, workspaceID int64) {
	t.Helper()
	ctx := context.Background()

	body, err := json.Marshal(registerRequest{Name: username, Email: username + "@example.com", Password: "hunter2"})
	if err != nil {
		t.Fatalf("marshal register request: %v", err)
	}
	resp, err := http.Post(s.srv.URL+"/api/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/auth/register: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 registering %s, got %d", username, resp.StatusCode)
	}
	var logged loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&logged); err != nil {
		t.Fatalf("decode register response: %v", err)
	}

	users := repository.NewUserRepository(s.db)
	user, err := users.GetByUsername(ctx, username)
	if err != nil {
		t.Fatalf("get %s: %v", username, err)
	}
	workspaces, err := s.workspaces.ListForUser(ctx, user.ID)
	if err != nil || len(workspaces) != 1 {
		t.Fatalf("list workspaces for %s: %v (%d)", username, err, len(workspaces))
	}

	return logged.AccessToken, user.ID, workspaces[0].ID
}

// addMember inserts a WorkspaceMember row directly, standing in for
// accepting a Workspace Invite (not this handler's concern).
func (s *accountHandlerTestServer) addMember(t *testing.T, workspaceID, userID int64, role string) {
	t.Helper()
	if _, err := s.db.Exec("INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)", workspaceID, userID, role); err != nil {
		t.Fatalf("add member: %v", err)
	}
}

func setDisabled(t *testing.T, srv *httptest.Server, accessToken string, isDisabled bool) *http.Response {
	t.Helper()

	body, err := json.Marshal(setDisabledRequest{IsDisabled: isDisabled})
	if err != nil {
		t.Fatalf("marshal set disabled request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/account/disabled", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/account/disabled: %v", err)
	}
	return resp
}

func TestAccountSetDisabled_DisablesAndReactivates(t *testing.T) {
	s := newAccountHandlerTestServer(t)
	accessToken, _, _ := s.register(t, "alice")

	resp := setDisabled(t, s.srv, accessToken, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var disabled setDisabledResponse
	if err := json.NewDecoder(resp.Body).Decode(&disabled); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !disabled.IsDisabled {
		t.Fatalf("expected is_disabled true")
	}

	reactivateResp := setDisabled(t, s.srv, accessToken, false)
	defer reactivateResp.Body.Close()
	if reactivateResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", reactivateResp.StatusCode)
	}
}

func TestAccountSetDisabled_NoToken_Returns401(t *testing.T) {
	s := newAccountHandlerTestServer(t)

	body, err := json.Marshal(setDisabledRequest{IsDisabled: true})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, s.srv.URL+"/api/account/disabled", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// TestAccountSetDisabled_RefusesTheSoleOwnerOfANonEmptyWorkspace covers
// AC#3: disabling is refused while the caller is the sole Owner of a
// Workspace with other Members, with a clear error surfaced to the UI.
func TestAccountSetDisabled_RefusesTheSoleOwnerOfANonEmptyWorkspace(t *testing.T) {
	s := newAccountHandlerTestServer(t)
	aliceToken, _, aliceWorkspaceID := s.register(t, "alice")
	_, bobID, _ := s.register(t, "bob")
	s.addMember(t, aliceWorkspaceID, bobID, repository.WorkspaceRoleMember)

	resp := setDisabled(t, s.srv, aliceToken, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAccountDeleteImpact_ReportsOwnedCalendars(t *testing.T) {
	s := newAccountHandlerTestServer(t)
	accessToken, aliceID, aliceWorkspaceID := s.register(t, "alice")

	if _, err := s.calendars.Create(context.Background(), aliceID, aliceWorkspaceID, "cal-1", service.CalendarWrite{Name: "Personal", Color: "#112233FF"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, s.srv.URL+"/api/account/delete-impact", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET delete-impact: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var impact deleteImpactResponse
	if err := json.NewDecoder(resp.Body).Decode(&impact); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(impact.Calendars) != 1 {
		t.Fatalf("expected 1 calendar, got %d", len(impact.Calendars))
	}
	if impact.Calendars[0].WorkspaceID != aliceWorkspaceID {
		t.Fatalf("expected the calendar's own workspace, got %d", impact.Calendars[0].WorkspaceID)
	}
}

func deleteAccount(t *testing.T, srv *httptest.Server, accessToken string, req deleteAccountRequest) *http.Response {
	t.Helper()

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal delete account request: %v", err)
	}
	httpReq, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/account/", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("DELETE /api/account/: %v", err)
	}
	return resp
}

func TestAccountDelete_DispositionDelete_RemovesAccount(t *testing.T) {
	s := newAccountHandlerTestServer(t)
	accessToken, aliceID, aliceWorkspaceID := s.register(t, "alice")

	calendar, err := s.calendars.Create(context.Background(), aliceID, aliceWorkspaceID, "cal-1", service.CalendarWrite{Name: "Personal", Color: "#112233FF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	resp := deleteAccount(t, s.srv, accessToken, deleteAccountRequest{
		Calendars: []calendarDispositionRequest{{CalendarID: calendar.ID, Disposition: "delete"}},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	loginResp := login(t, s.srv, "alice", "hunter2")
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected a deleted account to be refused login, got %d", loginResp.StatusCode)
	}
}

func TestAccountDelete_DispositionTransfer_ReassignsCalendar(t *testing.T) {
	s := newAccountHandlerTestServer(t)
	aliceToken, aliceID, aliceWorkspaceID := s.register(t, "alice")
	_, bobID, _ := s.register(t, "bob")
	s.addMember(t, aliceWorkspaceID, bobID, repository.WorkspaceRoleMember)

	calendar, err := s.calendars.Create(context.Background(), aliceID, aliceWorkspaceID, "cal-1", service.CalendarWrite{Name: "Family", Color: "#112233FF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	// Transfer Workspace ownership to bob so alice is no longer its sole
	// Owner (ADR-0044's guard) while bob stays a Member the calendar can go to.
	if _, err := s.db.Exec("UPDATE workspaces SET owner_user_id = ? WHERE id = ?", bobID, aliceWorkspaceID); err != nil {
		t.Fatalf("transfer workspace ownership: %v", err)
	}
	if _, err := s.db.Exec("UPDATE workspace_members SET role = 'owner' WHERE workspace_id = ? AND user_id = ?", aliceWorkspaceID, bobID); err != nil {
		t.Fatalf("promote bob: %v", err)
	}
	if _, err := s.db.Exec("UPDATE workspace_members SET role = 'member' WHERE workspace_id = ? AND user_id = ?", aliceWorkspaceID, aliceID); err != nil {
		t.Fatalf("demote alice: %v", err)
	}

	resp := deleteAccount(t, s.srv, aliceToken, deleteAccountRequest{
		Calendars: []calendarDispositionRequest{{CalendarID: calendar.ID, Disposition: "transfer", TransferTo: &bobID}},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	calendarRepo := repository.NewCalendarRepository(s.db)
	transferred, err := calendarRepo.GetByIDAny(context.Background(), calendar.ID)
	if err != nil {
		t.Fatalf("expected the calendar to survive: %v", err)
	}
	if transferred.UserID != bobID {
		t.Fatalf("expected the calendar to be owned by bob, got %d", transferred.UserID)
	}
}

func TestAccountDelete_MissingDisposition_Returns400(t *testing.T) {
	s := newAccountHandlerTestServer(t)
	accessToken, aliceID, aliceWorkspaceID := s.register(t, "alice")
	if _, err := s.calendars.Create(context.Background(), aliceID, aliceWorkspaceID, "cal-1", service.CalendarWrite{Name: "Personal", Color: "#112233FF"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	resp := deleteAccount(t, s.srv, accessToken, deleteAccountRequest{})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestAccountDelete_RefusesTheSoleOwnerOfANonEmptyWorkspace covers AC#3 for
// self-Delete.
func TestAccountDelete_RefusesTheSoleOwnerOfANonEmptyWorkspace(t *testing.T) {
	s := newAccountHandlerTestServer(t)
	aliceToken, _, aliceWorkspaceID := s.register(t, "alice")
	_, bobID, _ := s.register(t, "bob")
	s.addMember(t, aliceWorkspaceID, bobID, repository.WorkspaceRoleMember)

	resp := deleteAccount(t, s.srv, aliceToken, deleteAccountRequest{})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestAccountDelete_NoToken_Returns401(t *testing.T) {
	s := newAccountHandlerTestServer(t)

	body, err := json.Marshal(deleteAccountRequest{})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequest(http.MethodDelete, s.srv.URL+"/api/account/", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
