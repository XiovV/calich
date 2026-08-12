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

// groupHandlerTestServer bundles the HTTP surface the Groups management
// screen needs (#167): Register (to mint real Users, each with their own
// Workspace) and the /api/groups routes, gated by RequireWorkspace exactly
// like router.New wires them.
type groupHandlerTestServer struct {
	srv        *httptest.Server
	db         *sql.DB
	workspaces *service.WorkspaceService
}

func newGroupHandlerTestServer(t *testing.T) *groupHandlerTestServer {
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
	calendars := service.NewCalendarService(calendarRepo, shareRepo, users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	auth := service.NewAuthService(users, sessions, workspaces, inviteRepo, calendars, []byte("test-secret"), "", "", "", true)
	groups := service.NewGroupService(repository.NewGroupRepository(sqlDB), workspaceRepo)

	authHandler := NewAuthHandler(auth, false, false)
	groupHandler := NewGroupHandler(groups)

	r := chi.NewRouter()
	r.Post("/api/auth/register", authHandler.Register)
	r.Route("/api/groups", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Use(httpauth.RequireEnabledUser(auth))
		r.Use(httpauth.RequireWorkspace(workspaces))

		r.Get("/", groupHandler.List)
		r.Post("/", groupHandler.Create)
		r.Patch("/{id}", groupHandler.Rename)
		r.Delete("/{id}", groupHandler.Delete)
		r.Get("/{id}/members", groupHandler.ListMembers)
		r.Post("/{id}/members", groupHandler.AddMember)
		r.Delete("/{id}/members/{userId}", groupHandler.RemoveMember)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &groupHandlerTestServer{srv: srv, db: sqlDB, workspaces: workspaces}
}

func (s *groupHandlerTestServer) register(t *testing.T, username string) (accessToken string, userID, workspaceID int64) {
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

func (s *groupHandlerTestServer) addMember(t *testing.T, workspaceID, userID int64, role string) {
	t.Helper()
	if _, err := s.db.Exec("INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)", workspaceID, userID, role); err != nil {
		t.Fatalf("add member: %v", err)
	}
}

func (s *groupHandlerTestServer) do(t *testing.T, method, path, accessToken string, workspaceID int64, body any) *http.Response {
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
	req.Header.Set("X-Workspace-Id", strconv.FormatInt(workspaceID, 10))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func TestGroupHandler_Create_OwnerCanCreate(t *testing.T) {
	s := newGroupHandlerTestServer(t)
	ownerToken, _, workspaceID := s.register(t, "alice")

	resp := s.do(t, http.MethodPost, "/api/groups/", ownerToken, workspaceID, createGroupRequest{Name: "Tech team"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var group groupResponse
	if err := json.NewDecoder(resp.Body).Decode(&group); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if group.Name != "Tech team" {
		t.Fatalf("expected name %q, got %q", "Tech team", group.Name)
	}
}

func TestGroupHandler_Create_PlainMemberRefused(t *testing.T) {
	s := newGroupHandlerTestServer(t)
	_, _, workspaceID := s.register(t, "alice")
	memberToken, memberID, _ := s.register(t, "bob")
	s.addMember(t, workspaceID, memberID, repository.WorkspaceRoleMember)

	resp := s.do(t, http.MethodPost, "/api/groups/", memberToken, workspaceID, createGroupRequest{Name: "Tech team"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestGroupHandler_Create_EmptyNameRejected(t *testing.T) {
	s := newGroupHandlerTestServer(t)
	ownerToken, _, workspaceID := s.register(t, "alice")

	resp := s.do(t, http.MethodPost, "/api/groups/", ownerToken, workspaceID, createGroupRequest{Name: "   "})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGroupHandler_RenameAndDelete_OwnerCanManage(t *testing.T) {
	s := newGroupHandlerTestServer(t)
	ownerToken, _, workspaceID := s.register(t, "alice")

	createResp := s.do(t, http.MethodPost, "/api/groups/", ownerToken, workspaceID, createGroupRequest{Name: "Tech team"})
	defer createResp.Body.Close()
	var group groupResponse
	if err := json.NewDecoder(createResp.Body).Decode(&group); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	renameResp := s.do(t, http.MethodPatch, "/api/groups/"+strconv.FormatInt(group.ID, 10), ownerToken, workspaceID, renameGroupRequest{Name: "Engineering"})
	defer renameResp.Body.Close()
	if renameResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", renameResp.StatusCode)
	}
	var renamed groupResponse
	if err := json.NewDecoder(renameResp.Body).Decode(&renamed); err != nil {
		t.Fatalf("decode rename response: %v", err)
	}
	if renamed.Name != "Engineering" {
		t.Fatalf("expected name %q, got %q", "Engineering", renamed.Name)
	}

	deleteResp := s.do(t, http.MethodDelete, "/api/groups/"+strconv.FormatInt(group.ID, 10), ownerToken, workspaceID, nil)
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", deleteResp.StatusCode)
	}

	listResp := s.do(t, http.MethodGet, "/api/groups/", ownerToken, workspaceID, nil)
	defer listResp.Body.Close()
	var list []groupResponse
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no groups after delete, got %v", list)
	}
}

func TestGroupHandler_Rename_PlainMemberRefused(t *testing.T) {
	s := newGroupHandlerTestServer(t)
	ownerToken, _, workspaceID := s.register(t, "alice")
	memberToken, memberID, _ := s.register(t, "bob")
	s.addMember(t, workspaceID, memberID, repository.WorkspaceRoleMember)

	createResp := s.do(t, http.MethodPost, "/api/groups/", ownerToken, workspaceID, createGroupRequest{Name: "Tech team"})
	defer createResp.Body.Close()
	var group groupResponse
	if err := json.NewDecoder(createResp.Body).Decode(&group); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	resp := s.do(t, http.MethodPatch, "/api/groups/"+strconv.FormatInt(group.ID, 10), memberToken, workspaceID, renameGroupRequest{Name: "Engineering"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestGroupHandler_AddAndRemoveMember_OwnerCanManageMembership(t *testing.T) {
	s := newGroupHandlerTestServer(t)
	ownerToken, _, workspaceID := s.register(t, "alice")
	_, memberID, _ := s.register(t, "bob")
	s.addMember(t, workspaceID, memberID, repository.WorkspaceRoleMember)

	createResp := s.do(t, http.MethodPost, "/api/groups/", ownerToken, workspaceID, createGroupRequest{Name: "Tech team"})
	defer createResp.Body.Close()
	var group groupResponse
	if err := json.NewDecoder(createResp.Body).Decode(&group); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	addResp := s.do(t, http.MethodPost, "/api/groups/"+strconv.FormatInt(group.ID, 10)+"/members", ownerToken, workspaceID, addGroupMemberRequest{UserID: memberID})
	defer addResp.Body.Close()
	if addResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", addResp.StatusCode)
	}

	listResp := s.do(t, http.MethodGet, "/api/groups/"+strconv.FormatInt(group.ID, 10)+"/members", ownerToken, workspaceID, nil)
	defer listResp.Body.Close()
	var members []groupMemberResponse
	if err := json.NewDecoder(listResp.Body).Decode(&members); err != nil {
		t.Fatalf("decode members response: %v", err)
	}
	if len(members) != 1 || members[0].UserID != memberID {
		t.Fatalf("expected [%d], got %v", memberID, members)
	}

	removeResp := s.do(t, http.MethodDelete, "/api/groups/"+strconv.FormatInt(group.ID, 10)+"/members/"+strconv.FormatInt(memberID, 10), ownerToken, workspaceID, nil)
	defer removeResp.Body.Close()
	if removeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", removeResp.StatusCode)
	}
}

func TestGroupHandler_AddMember_RefusesUserOutsideWorkspace(t *testing.T) {
	s := newGroupHandlerTestServer(t)
	ownerToken, _, workspaceID := s.register(t, "alice")
	_, outsiderID, _ := s.register(t, "carol")

	createResp := s.do(t, http.MethodPost, "/api/groups/", ownerToken, workspaceID, createGroupRequest{Name: "Tech team"})
	defer createResp.Body.Close()
	var group groupResponse
	if err := json.NewDecoder(createResp.Body).Decode(&group); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	resp := s.do(t, http.MethodPost, "/api/groups/"+strconv.FormatInt(group.ID, 10)+"/members", ownerToken, workspaceID, addGroupMemberRequest{UserID: outsiderID})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
