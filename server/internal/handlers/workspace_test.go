package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

// workspaceHandlerTestServer bundles the HTTP surface #165's member-
// management screen needs: Register (to mint real Users, each with their own
// Workspace) and the /api/workspaces routes.
type workspaceHandlerTestServer struct {
	srv        *httptest.Server
	db         *sql.DB
	workspaces *service.WorkspaceService
}

func newWorkspaceHandlerTestServer(t *testing.T) *workspaceHandlerTestServer {
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
	inviteRepo := repository.NewWorkspaceInviteRepository(sqlDB)
	workspaces := service.NewWorkspaceService(sqlDB, workspaceRepo, inviteRepo, calendarRepo, shareRepo)
	calendars := service.NewCalendarService(calendarRepo, shareRepo, users, repository.NewEventReminderRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	auth := service.NewAuthService(users, sessions, workspaces, inviteRepo, calendars, repository.NewAttendeeRepository(sqlDB), []byte("test-secret"), "", "", "", true)

	authHandler := NewAuthHandler(auth, false, false)
	workspaceHandler := NewWorkspaceHandler(workspaces)

	r := chi.NewRouter()
	r.Post("/api/auth/register", authHandler.Register)
	r.Route("/api/workspaces", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Use(httpauth.RequireEnabledUser(auth))

		r.Get("/{id}/invites", workspaceHandler.ListInvites)
		r.Delete("/invites/{id}", workspaceHandler.CancelInvite)
		r.Get("/{id}/members", workspaceHandler.ListMembers)
		r.Put("/{id}/members/{userId}/role", workspaceHandler.SetMemberRole)
		r.Get("/{id}/members/{userId}/remove-impact", workspaceHandler.RemoveMemberImpact)
		r.Delete("/{id}/members/{userId}", workspaceHandler.RemoveMember)
		r.Post("/{id}/invites", workspaceHandler.CreateInvite)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &workspaceHandlerTestServer{srv: srv, db: sqlDB, workspaces: workspaces}
}

func (s *workspaceHandlerTestServer) register(t *testing.T, username string) (accessToken string, userID, workspaceID int64) {
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
	user, err := users.GetByEmail(ctx, username+"@example.com")
	if err != nil {
		t.Fatalf("get %s: %v", username, err)
	}
	workspaces, err := s.workspaces.ListForUser(ctx, user.ID)
	if err != nil || len(workspaces) != 1 {
		t.Fatalf("list workspaces for %s: %v (%d)", username, err, len(workspaces))
	}

	return logged.AccessToken, user.ID, workspaces[0].ID
}

func (s *workspaceHandlerTestServer) addMember(t *testing.T, workspaceID, userID int64, role string) {
	t.Helper()
	if _, err := s.db.Exec("INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)", workspaceID, userID, role); err != nil {
		t.Fatalf("add member: %v", err)
	}
}

func (s *workspaceHandlerTestServer) do(t *testing.T, method, path, accessToken string, body any) *http.Response {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, s.srv.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func TestWorkspaceHandler_ListMembers_ReturnsMembersWithUsernameAndRole(t *testing.T) {
	s := newWorkspaceHandlerTestServer(t)
	ownerToken, ownerID, workspaceID := s.register(t, "alice")
	_, bobID, _ := s.register(t, "bob")
	s.addMember(t, workspaceID, bobID, repository.WorkspaceRoleMember)

	resp := s.do(t, http.MethodGet, "/api/workspaces/"+idStr(workspaceID)+"/members", ownerToken, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var members []workspaceMemberResponse
	if err := json.NewDecoder(resp.Body).Decode(&members); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}

	byID := map[int64]workspaceMemberResponse{}
	for _, m := range members {
		byID[m.UserID] = m
	}
	if byID[ownerID].Role != repository.WorkspaceRoleOwner {
		t.Fatalf("expected owner role, got %q", byID[ownerID].Role)
	}
	if byID[bobID].Name != "bob" {
		t.Fatalf("expected bob's name, got %q", byID[bobID].Name)
	}
}

func TestWorkspaceHandler_ListMembers_RefusesNonMember(t *testing.T) {
	s := newWorkspaceHandlerTestServer(t)
	_, _, workspaceID := s.register(t, "alice")
	outsiderToken, _, _ := s.register(t, "carol")

	resp := s.do(t, http.MethodGet, "/api/workspaces/"+idStr(workspaceID)+"/members", outsiderToken, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestWorkspaceHandler_SetMemberRole_OwnerCanPromoteToAdmin(t *testing.T) {
	s := newWorkspaceHandlerTestServer(t)
	ownerToken, _, workspaceID := s.register(t, "alice")
	_, bobID, _ := s.register(t, "bob")
	s.addMember(t, workspaceID, bobID, repository.WorkspaceRoleMember)

	resp := s.do(t, http.MethodPut, "/api/workspaces/"+idStr(workspaceID)+"/members/"+idStr(bobID)+"/role", ownerToken, setWorkspaceMemberRoleRequest{Role: "admin"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var member workspaceMemberResponse
	if err := json.NewDecoder(resp.Body).Decode(&member); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if member.Role != repository.WorkspaceRoleAdmin {
		t.Fatalf("expected admin role, got %q", member.Role)
	}
}

func TestWorkspaceHandler_SetMemberRole_AdminCannotPromote(t *testing.T) {
	s := newWorkspaceHandlerTestServer(t)
	_, _, workspaceID := s.register(t, "alice")
	adminToken, adminID, _ := s.register(t, "bob")
	s.addMember(t, workspaceID, adminID, repository.WorkspaceRoleAdmin)
	_, carolID, _ := s.register(t, "carol")
	s.addMember(t, workspaceID, carolID, repository.WorkspaceRoleMember)

	resp := s.do(t, http.MethodPut, "/api/workspaces/"+idStr(workspaceID)+"/members/"+idStr(carolID)+"/role", adminToken, setWorkspaceMemberRoleRequest{Role: "admin"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestWorkspaceHandler_SetMemberRole_CannotChangeOwnerRole(t *testing.T) {
	s := newWorkspaceHandlerTestServer(t)
	ownerToken, ownerID, workspaceID := s.register(t, "alice")

	resp := s.do(t, http.MethodPut, "/api/workspaces/"+idStr(workspaceID)+"/members/"+idStr(ownerID)+"/role", ownerToken, setWorkspaceMemberRoleRequest{Role: "member"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestWorkspaceHandler_RemoveMemberImpact_ReportsOwnedCalendars(t *testing.T) {
	s := newWorkspaceHandlerTestServer(t)
	ownerToken, _, workspaceID := s.register(t, "alice")
	_, bobID, _ := s.register(t, "bob")
	s.addMember(t, workspaceID, bobID, repository.WorkspaceRoleMember)

	calendars := repository.NewCalendarRepository(s.db)
	if _, err := calendars.Create(context.Background(), bobID, workspaceID, "cal-1", repository.CalendarFields{Name: "Bob's calendar", Color: "#112233FF"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	resp := s.do(t, http.MethodGet, "/api/workspaces/"+idStr(workspaceID)+"/members/"+idStr(bobID)+"/remove-impact", ownerToken, nil)
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
}

func TestWorkspaceHandler_RemoveMemberImpact_RefusesRemovingOwner(t *testing.T) {
	s := newWorkspaceHandlerTestServer(t)
	ownerToken, ownerID, workspaceID := s.register(t, "alice")
	_, adminID, _ := s.register(t, "bob")
	s.addMember(t, workspaceID, adminID, repository.WorkspaceRoleAdmin)

	resp := s.do(t, http.MethodGet, "/api/workspaces/"+idStr(workspaceID)+"/members/"+idStr(ownerID)+"/remove-impact", ownerToken, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestWorkspaceHandler_RemoveMember_DeletesPlainMember(t *testing.T) {
	s := newWorkspaceHandlerTestServer(t)
	ownerToken, _, workspaceID := s.register(t, "alice")
	_, bobID, _ := s.register(t, "bob")
	s.addMember(t, workspaceID, bobID, repository.WorkspaceRoleMember)

	resp := s.do(t, http.MethodDelete, "/api/workspaces/"+idStr(workspaceID)+"/members/"+idStr(bobID), ownerToken, removeMemberRequest{Calendars: []calendarDispositionRequest{}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	listResp := s.do(t, http.MethodGet, "/api/workspaces/"+idStr(workspaceID)+"/members", ownerToken, nil)
	defer listResp.Body.Close()
	var members []workspaceMemberResponse
	if err := json.NewDecoder(listResp.Body).Decode(&members); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 remaining member, got %d", len(members))
	}
}

func TestWorkspaceHandler_RemoveMember_AdminRefusedAgainstAnotherAdmin(t *testing.T) {
	s := newWorkspaceHandlerTestServer(t)
	_, _, workspaceID := s.register(t, "alice")
	adminToken, adminID, _ := s.register(t, "bob")
	s.addMember(t, workspaceID, adminID, repository.WorkspaceRoleAdmin)
	_, otherAdminID, _ := s.register(t, "carol")
	s.addMember(t, workspaceID, otherAdminID, repository.WorkspaceRoleAdmin)

	resp := s.do(t, http.MethodDelete, "/api/workspaces/"+idStr(workspaceID)+"/members/"+idStr(otherAdminID), adminToken, removeMemberRequest{Calendars: []calendarDispositionRequest{}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestWorkspaceHandler_RemoveMember_RequiresDispositionForOwnedCalendar(t *testing.T) {
	s := newWorkspaceHandlerTestServer(t)
	ownerToken, _, workspaceID := s.register(t, "alice")
	_, bobID, _ := s.register(t, "bob")
	s.addMember(t, workspaceID, bobID, repository.WorkspaceRoleMember)

	calendars := repository.NewCalendarRepository(s.db)
	if _, err := calendars.Create(context.Background(), bobID, workspaceID, "cal-1", repository.CalendarFields{Name: "Bob's calendar", Color: "#112233FF"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	resp := s.do(t, http.MethodDelete, "/api/workspaces/"+idStr(workspaceID)+"/members/"+idStr(bobID), ownerToken, removeMemberRequest{Calendars: []calendarDispositionRequest{}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestWorkspaceHandler_ListInvites_ReturnsOutstandingInvites(t *testing.T) {
	s := newWorkspaceHandlerTestServer(t)
	ownerToken, _, workspaceID := s.register(t, "alice")

	createResp := s.do(t, http.MethodPost, "/api/workspaces/"+idStr(workspaceID)+"/invites", ownerToken, createWorkspaceInviteRequest{Email: "invitee@example.com"})
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating invite, got %d", createResp.StatusCode)
	}

	resp := s.do(t, http.MethodGet, "/api/workspaces/"+idStr(workspaceID)+"/invites", ownerToken, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var invites []outstandingWorkspaceInviteResponse
	if err := json.NewDecoder(resp.Body).Decode(&invites); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(invites) != 1 {
		t.Fatalf("expected 1 outstanding invite, got %d", len(invites))
	}
	if invites[0].Email != "invitee@example.com" {
		t.Fatalf("expected the invitee's email, got %q", invites[0].Email)
	}
}

func TestWorkspaceHandler_CancelInvite_RemovesItAndRequiresOwnerOrAdmin(t *testing.T) {
	s := newWorkspaceHandlerTestServer(t)
	ownerToken, _, workspaceID := s.register(t, "alice")

	var created workspaceInviteResponse
	createResp := s.do(t, http.MethodPost, "/api/workspaces/"+idStr(workspaceID)+"/invites", ownerToken, createWorkspaceInviteRequest{Email: "invitee@example.com"})
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	createResp.Body.Close()

	outsiderToken, _, _ := s.register(t, "carol")
	refused := s.do(t, http.MethodDelete, "/api/workspaces/invites/"+idStr(created.ID), outsiderToken, nil)
	defer refused.Body.Close()
	if refused.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for a non-member canceling an invite, got %d", refused.StatusCode)
	}

	resp := s.do(t, http.MethodDelete, "/api/workspaces/invites/"+idStr(created.ID), ownerToken, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	listResp := s.do(t, http.MethodGet, "/api/workspaces/"+idStr(workspaceID)+"/invites", ownerToken, nil)
	defer listResp.Body.Close()
	var invites []outstandingWorkspaceInviteResponse
	if err := json.NewDecoder(listResp.Body).Decode(&invites); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(invites) != 0 {
		t.Fatalf("expected the canceled invite to be gone, got %d", len(invites))
	}
}

func idStr(id int64) string {
	return strconv.FormatInt(id, 10)
}
