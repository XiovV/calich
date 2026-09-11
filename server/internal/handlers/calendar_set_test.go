package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/apptest"
	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/service"
)

// calendarSetHandlerTestServer bundles the HTTP surface the Calendar Sets
// Settings Section needs (#301, ADR-0082): Register (to mint real Users,
// each with their own Workspace) and the /api/calendar-sets routes, gated by
// RequireWorkspace exactly like router.New wires them.
type calendarSetHandlerTestServer struct {
	srv        *httptest.Server
	graph      *service.Graph
	workspaces *service.WorkspaceService
}

func newCalendarSetHandlerTestServer(t *testing.T) *calendarSetHandlerTestServer {
	t.Helper()

	// No bootstrap account, and ENABLE_SIGNUPS on: every User here registers
	// through the HTTP surface.
	cfg := apptest.Config(t)
	cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword = "", "", ""
	cfg.EnableSignups = true
	g := newTestGraphWithConfig(t, cfg)

	workspaces := g.Workspaces
	auth := g.Auth
	sets := g.CalendarSets

	authHandler := NewAuthHandler(auth, g.RateLimiter, false, false, false, true)
	calendarSetHandler := NewCalendarSetHandler(sets)

	r := chi.NewRouter()
	r.Post("/api/auth/register", authHandler.Register)
	r.Route("/api/calendar-sets", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Use(httpauth.RequireEnabledUser(auth))
		r.Use(httpauth.RequireWorkspace(workspaces))

		r.Get("/", calendarSetHandler.List)
		r.Post("/", calendarSetHandler.Create)
		r.Patch("/{id}", calendarSetHandler.Rename)
		r.Delete("/{id}", calendarSetHandler.Delete)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &calendarSetHandlerTestServer{srv: srv, graph: g, workspaces: workspaces}
}

func (s *calendarSetHandlerTestServer) register(t *testing.T, username string) (accessToken string, userID, workspaceID int64) {
	t.Helper()
	ctx := context.Background()

	body, err := json.Marshal(registerRequest{Name: username, Email: username + "@example.com", Password: "hunter22"})
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

	users := s.graph.UserRepo
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

func (s *calendarSetHandlerTestServer) do(t *testing.T, method, path, accessToken string, workspaceID int64, body any) *http.Response {
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

func TestCalendarSetHandler_Create_AnyMemberCanCreate(t *testing.T) {
	s := newCalendarSetHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	resp := s.do(t, http.MethodPost, "/api/calendar-sets/", token, workspaceID, createCalendarSetRequest{Name: "Work"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var set calendarSetResponse
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if set.Name != "Work" {
		t.Fatalf("expected name %q, got %q", "Work", set.Name)
	}
}

func TestCalendarSetHandler_Create_EmptyNameRejected(t *testing.T) {
	s := newCalendarSetHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	resp := s.do(t, http.MethodPost, "/api/calendar-sets/", token, workspaceID, createCalendarSetRequest{Name: "   "})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCalendarSetHandler_RenameAndDelete_OwnerCanManage(t *testing.T) {
	s := newCalendarSetHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	createResp := s.do(t, http.MethodPost, "/api/calendar-sets/", token, workspaceID, createCalendarSetRequest{Name: "Work"})
	defer createResp.Body.Close()
	var set calendarSetResponse
	if err := json.NewDecoder(createResp.Body).Decode(&set); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	renameResp := s.do(t, http.MethodPatch, "/api/calendar-sets/"+strconv.FormatInt(set.ID, 10), token, workspaceID, renameCalendarSetRequest{Name: "Weekend"})
	defer renameResp.Body.Close()
	if renameResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", renameResp.StatusCode)
	}
	var renamed calendarSetResponse
	if err := json.NewDecoder(renameResp.Body).Decode(&renamed); err != nil {
		t.Fatalf("decode rename response: %v", err)
	}
	if renamed.Name != "Weekend" {
		t.Fatalf("expected name %q, got %q", "Weekend", renamed.Name)
	}

	deleteResp := s.do(t, http.MethodDelete, "/api/calendar-sets/"+strconv.FormatInt(set.ID, 10), token, workspaceID, nil)
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", deleteResp.StatusCode)
	}

	listResp := s.do(t, http.MethodGet, "/api/calendar-sets/", token, workspaceID, nil)
	defer listResp.Body.Close()
	var list []calendarSetResponse
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no calendar sets after delete, got %v", list)
	}
}

func TestCalendarSetHandler_List_ScopedToWorkspace(t *testing.T) {
	s := newCalendarSetHandlerTestServer(t)
	token, userID, firstWorkspaceID := s.register(t, "alice")

	createResp := s.do(t, http.MethodPost, "/api/calendar-sets/", token, firstWorkspaceID, createCalendarSetRequest{Name: "Work"})
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", createResp.StatusCode)
	}

	secondWorkspace, err := s.graph.Workspaces.CreateForOwner(context.Background(), userID, "Second workspace")
	if err != nil {
		t.Fatalf("create second workspace: %v", err)
	}

	listResp := s.do(t, http.MethodGet, "/api/calendar-sets/", token, secondWorkspace.ID, nil)
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.StatusCode)
	}
	var list []calendarSetResponse
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no calendar sets in the second workspace, got %v", list)
	}
}

func TestCalendarSetHandler_SecondUserCannotReadRenameOrDeleteFirstsSet(t *testing.T) {
	s := newCalendarSetHandlerTestServer(t)
	aliceToken, _, workspaceID := s.register(t, "alice")
	bobToken, bobID, _ := s.register(t, "bob")
	// bob is not a Member of alice's Workspace, but that's irrelevant here:
	// even a Member of the same Workspace must not reach another User's
	// private Set, so add bob to alice's Workspace to prove privacy holds
	// regardless of shared Workspace membership.
	if _, err := s.graph.DB.Exec("INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)", workspaceID, bobID, "member"); err != nil {
		t.Fatalf("add bob to alice's workspace: %v", err)
	}

	createResp := s.do(t, http.MethodPost, "/api/calendar-sets/", aliceToken, workspaceID, createCalendarSetRequest{Name: "Work"})
	defer createResp.Body.Close()
	var set calendarSetResponse
	if err := json.NewDecoder(createResp.Body).Decode(&set); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	renameResp := s.do(t, http.MethodPatch, "/api/calendar-sets/"+strconv.FormatInt(set.ID, 10), bobToken, workspaceID, renameCalendarSetRequest{Name: "Hijacked"})
	defer renameResp.Body.Close()
	if renameResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 renaming, got %d", renameResp.StatusCode)
	}

	deleteResp := s.do(t, http.MethodDelete, "/api/calendar-sets/"+strconv.FormatInt(set.ID, 10), bobToken, workspaceID, nil)
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 deleting, got %d", deleteResp.StatusCode)
	}

	listResp := s.do(t, http.MethodGet, "/api/calendar-sets/", bobToken, workspaceID, nil)
	defer listResp.Body.Close()
	var list []calendarSetResponse
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected bob to see no calendar sets, got %v", list)
	}

	// alice's Set survived every one of bob's attempts.
	aliceListResp := s.do(t, http.MethodGet, "/api/calendar-sets/", aliceToken, workspaceID, nil)
	defer aliceListResp.Body.Close()
	var aliceList []calendarSetResponse
	if err := json.NewDecoder(aliceListResp.Body).Decode(&aliceList); err != nil {
		t.Fatalf("decode alice list response: %v", err)
	}
	if len(aliceList) != 1 || aliceList[0].Name != "Work" {
		t.Fatalf("expected alice's set to survive untouched, got %v", aliceList)
	}
}
