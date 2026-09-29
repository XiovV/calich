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

// taskListHandlerTestServer bundles the HTTP surface the Tasks panel's
// Lists filter needs (#317, ADR-0083): Register (to mint real Users, each
// with their own Workspace and auto-provisioned Inbox), the Workspace
// Invite issue/accept routes (to exercise Inbox provisioning on that other
// call path), and the /api/task-lists routes, gated by RequireWorkspace
// exactly like router.New wires them.
type taskListHandlerTestServer struct {
	srv        *httptest.Server
	graph      *service.Graph
	workspaces *service.WorkspaceService
}

func newTaskListHandlerTestServer(t *testing.T) *taskListHandlerTestServer {
	t.Helper()

	// No bootstrap account, and ENABLE_SIGNUPS on: every User here registers
	// through the HTTP surface.
	cfg := apptest.Config(t)
	cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword = "", "", ""
	cfg.EnableSignups = true
	g := newTestGraphWithConfig(t, cfg)

	workspaces := g.Workspaces
	auth := g.Auth
	taskLists := g.TaskLists

	authHandler := NewAuthHandler(auth, g.RateLimiter, false, false, false, true)
	workspaceHandler := NewWorkspaceHandler(workspaces)
	taskListHandler := NewTaskListHandler(taskLists)

	r := chi.NewRouter()
	r.Post("/api/auth/register", authHandler.Register)
	// Workspace Invite accept (ADR-0044): the new-account path is public,
	// the existing-account path requires the caller already be logged in —
	// both are exercised here to prove Inbox provisioning on both accept
	// paths, not just Workspace creation.
	r.Post("/api/auth/accept-workspace-invite", authHandler.AcceptWorkspaceInvite)
	r.With(httpauth.RequireAuth(auth)).Post("/api/auth/accept-workspace-invite/join", authHandler.JoinWorkspaceInvite)

	r.Route("/api/workspaces", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Use(httpauth.RequireEnabledUser(auth))

		r.Post("/{id}/invites", workspaceHandler.CreateInvite)
	})

	r.Route("/api/task-lists", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Use(httpauth.RequireEnabledUser(auth))
		r.Use(httpauth.RequireWorkspace(workspaces))

		r.Get("/", taskListHandler.List)
		r.Post("/", taskListHandler.Create)
		r.Patch("/{id}", taskListHandler.Rename)
		r.Patch("/{id}/color", taskListHandler.Recolor)
		r.Put("/{id}/default", taskListHandler.SetDefault)
		r.Delete("/{id}", taskListHandler.Delete)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &taskListHandlerTestServer{srv: srv, graph: g, workspaces: workspaces}
}

func (s *taskListHandlerTestServer) register(t *testing.T, username string) (accessToken string, userID, workspaceID int64) {
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

// createInvite issues a Workspace Invite for email inside workspaceID, on
// ownerToken's behalf, and returns its plaintext token.
func (s *taskListHandlerTestServer) createInvite(t *testing.T, ownerToken string, workspaceID int64, email string) string {
	t.Helper()

	body, err := json.Marshal(createWorkspaceInviteRequest{Email: email})
	if err != nil {
		t.Fatalf("marshal create invite request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, s.srv.URL+"/api/workspaces/"+strconv.FormatInt(workspaceID, 10)+"/invites", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build create invite request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST create invite: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating invite, got %d", resp.StatusCode)
	}

	var invite workspaceInviteResponse
	if err := json.NewDecoder(resp.Body).Decode(&invite); err != nil {
		t.Fatalf("decode create invite response: %v", err)
	}
	return invite.Token
}

// acceptNewAccount accepts inviteToken via the public new-account path,
// creating a brand-new User, and returns their access token.
func (s *taskListHandlerTestServer) acceptNewAccount(t *testing.T, inviteToken, name, password string) string {
	t.Helper()

	body, err := json.Marshal(acceptWorkspaceInviteRequest{Token: inviteToken, Name: name, Password: password})
	if err != nil {
		t.Fatalf("marshal accept invite request: %v", err)
	}
	resp, err := http.Post(s.srv.URL+"/api/auth/accept-workspace-invite", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST accept invite (new account): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 accepting invite (new account), got %d", resp.StatusCode)
	}

	var logged loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&logged); err != nil {
		t.Fatalf("decode accept invite response: %v", err)
	}
	return logged.AccessToken
}

// acceptExisting accepts inviteToken via the authenticated existing-account
// path, on accessToken's behalf.
func (s *taskListHandlerTestServer) acceptExisting(t *testing.T, accessToken, inviteToken string) {
	t.Helper()

	body, err := json.Marshal(joinWorkspaceInviteRequest{Token: inviteToken})
	if err != nil {
		t.Fatalf("marshal join invite request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, s.srv.URL+"/api/auth/accept-workspace-invite/join", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build join invite request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST accept invite (existing account): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 accepting invite (existing account), got %d", resp.StatusCode)
	}
}

// createTaskList creates a Task List named name in color for accessToken in
// workspaceID and returns the decoded response, failing the test on
// anything but 201.
func (s *taskListHandlerTestServer) createTaskList(t *testing.T, accessToken string, workspaceID int64, name, color string) taskListResponse {
	t.Helper()

	resp := s.do(t, http.MethodPost, "/api/task-lists/", accessToken, workspaceID, createTaskListRequest{Name: name, Color: color})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating task list, got %d", resp.StatusCode)
	}
	var list taskListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode create task list response: %v", err)
	}
	return list
}

// listTaskLists GETs accessToken's Task Lists in workspaceID.
func (s *taskListHandlerTestServer) listTaskLists(t *testing.T, accessToken string, workspaceID int64) []taskListResponse {
	t.Helper()

	resp := s.do(t, http.MethodGet, "/api/task-lists/", accessToken, workspaceID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 listing task lists, got %d", resp.StatusCode)
	}
	var list []taskListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list task lists response: %v", err)
	}
	return list
}

func (s *taskListHandlerTestServer) do(t *testing.T, method, path, accessToken string, workspaceID int64, body any) *http.Response {
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

func TestTaskListHandler_Register_ProvisionsDefaultInbox(t *testing.T) {
	s := newTaskListHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	lists := s.listTaskLists(t, token, workspaceID)
	if len(lists) != 1 {
		t.Fatalf("expected exactly one auto-provisioned task list, got %v", lists)
	}
	if lists[0].Name != "Inbox" || !lists[0].IsDefault {
		t.Fatalf("expected a default Inbox, got %v", lists[0])
	}
}

func TestTaskListHandler_WorkspaceInviteAcceptance_NewAccount_ProvisionsDefaultInbox(t *testing.T) {
	s := newTaskListHandlerTestServer(t)
	aliceToken, _, aliceWorkspaceID := s.register(t, "alice")

	inviteToken := s.createInvite(t, aliceToken, aliceWorkspaceID, "carol@example.com")
	carolToken := s.acceptNewAccount(t, inviteToken, "Carol", "hunter22")

	lists := s.listTaskLists(t, carolToken, aliceWorkspaceID)
	if len(lists) != 1 {
		t.Fatalf("expected exactly one auto-provisioned task list, got %v", lists)
	}
	if lists[0].Name != "Inbox" || !lists[0].IsDefault {
		t.Fatalf("expected a default Inbox, got %v", lists[0])
	}
}

func TestTaskListHandler_WorkspaceInviteAcceptance_ExistingAccount_ProvisionsDefaultInbox(t *testing.T) {
	s := newTaskListHandlerTestServer(t)
	aliceToken, _, aliceWorkspaceID := s.register(t, "alice")
	bobToken, _, bobWorkspaceID := s.register(t, "bob")

	inviteToken := s.createInvite(t, aliceToken, aliceWorkspaceID, "bob@example.com")
	s.acceptExisting(t, bobToken, inviteToken)

	// bob's own Workspace keeps its own Inbox, untouched.
	bobOwnLists := s.listTaskLists(t, bobToken, bobWorkspaceID)
	if len(bobOwnLists) != 1 || bobOwnLists[0].Name != "Inbox" || !bobOwnLists[0].IsDefault {
		t.Fatalf("expected bob's own workspace to still have its own default Inbox, got %v", bobOwnLists)
	}

	// bob also gets his own, independent default Inbox inside alice's
	// Workspace — provisioned the moment his (User, Workspace) membership
	// there came into being.
	bobListsInAlice := s.listTaskLists(t, bobToken, aliceWorkspaceID)
	if len(bobListsInAlice) != 1 || bobListsInAlice[0].Name != "Inbox" || !bobListsInAlice[0].IsDefault {
		t.Fatalf("expected bob to get a default Inbox in alice's workspace, got %v", bobListsInAlice)
	}
}

func TestTaskListHandler_Create_AnyMemberCanCreate(t *testing.T) {
	s := newTaskListHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	list := s.createTaskList(t, token, workspaceID, "Work", "#12809CFF")
	if list.Name != "Work" {
		t.Fatalf("expected name %q, got %q", "Work", list.Name)
	}
	if list.Color != "#12809CFF" {
		t.Fatalf("expected color %q, got %q", "#12809CFF", list.Color)
	}
	if list.IsDefault {
		t.Fatalf("expected a freshly created task list to not be default")
	}
}

func TestTaskListHandler_Create_BlankColorAutoAssignsASwatch(t *testing.T) {
	s := newTaskListHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	list := s.createTaskList(t, token, workspaceID, "Work", "")
	if list.Color == "" {
		t.Fatalf("expected a non-empty auto-assigned color")
	}
}

func TestTaskListHandler_Create_EmptyNameRejected(t *testing.T) {
	s := newTaskListHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	resp := s.do(t, http.MethodPost, "/api/task-lists/", token, workspaceID, createTaskListRequest{Name: "   ", Color: "#12809CFF"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestTaskListHandler_Create_InvalidColorRejected(t *testing.T) {
	s := newTaskListHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	resp := s.do(t, http.MethodPost, "/api/task-lists/", token, workspaceID, createTaskListRequest{Name: "Work", Color: "not-a-color"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestTaskListHandler_RenameAndRecolor(t *testing.T) {
	s := newTaskListHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	list := s.createTaskList(t, token, workspaceID, "Work", "#12809CFF")

	renameResp := s.do(t, http.MethodPatch, "/api/task-lists/"+strconv.FormatInt(list.ID, 10), token, workspaceID, renameTaskListRequest{Name: "Weekend"})
	defer renameResp.Body.Close()
	if renameResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 renaming, got %d", renameResp.StatusCode)
	}
	var renamed taskListResponse
	if err := json.NewDecoder(renameResp.Body).Decode(&renamed); err != nil {
		t.Fatalf("decode rename response: %v", err)
	}
	if renamed.Name != "Weekend" {
		t.Fatalf("expected name %q, got %q", "Weekend", renamed.Name)
	}

	recolorResp := s.do(t, http.MethodPatch, "/api/task-lists/"+strconv.FormatInt(list.ID, 10)+"/color", token, workspaceID, recolorTaskListRequest{Color: "#E2483DFF"})
	defer recolorResp.Body.Close()
	if recolorResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 recoloring, got %d", recolorResp.StatusCode)
	}
	var recolored taskListResponse
	if err := json.NewDecoder(recolorResp.Body).Decode(&recolored); err != nil {
		t.Fatalf("decode recolor response: %v", err)
	}
	if recolored.Color != "#E2483DFF" {
		t.Fatalf("expected color %q, got %q", "#E2483DFF", recolored.Color)
	}
}

func TestTaskListHandler_List_ScopedToWorkspace(t *testing.T) {
	s := newTaskListHandlerTestServer(t)
	token, userID, firstWorkspaceID := s.register(t, "alice")

	s.createTaskList(t, token, firstWorkspaceID, "Work", "#12809CFF")

	secondWorkspace, err := s.graph.Workspaces.CreateForOwner(context.Background(), userID, "Second workspace")
	if err != nil {
		t.Fatalf("create second workspace: %v", err)
	}

	// The second Workspace has its own auto-provisioned Inbox, and nothing
	// from the first.
	lists := s.listTaskLists(t, token, secondWorkspace.ID)
	if len(lists) != 1 || lists[0].Name != "Inbox" {
		t.Fatalf("expected only the second workspace's own Inbox, got %v", lists)
	}
}

func TestTaskListHandler_SecondUserCannotReadRenameRecolorOrDeleteFirstsTaskList(t *testing.T) {
	s := newTaskListHandlerTestServer(t)
	aliceToken, _, workspaceID := s.register(t, "alice")
	bobToken, bobID, _ := s.register(t, "bob")
	// bob is not a Member of alice's Workspace, but that's irrelevant here:
	// even a Member of the same Workspace must not reach another User's
	// private Task List, so add bob to alice's Workspace to prove privacy
	// holds regardless of shared Workspace membership.
	if _, err := s.graph.DB.Exec("INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)", workspaceID, bobID, "member"); err != nil {
		t.Fatalf("add bob to alice's workspace: %v", err)
	}

	list := s.createTaskList(t, aliceToken, workspaceID, "Work", "#12809CFF")

	renameResp := s.do(t, http.MethodPatch, "/api/task-lists/"+strconv.FormatInt(list.ID, 10), bobToken, workspaceID, renameTaskListRequest{Name: "Hijacked"})
	defer renameResp.Body.Close()
	if renameResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 renaming, got %d", renameResp.StatusCode)
	}

	recolorResp := s.do(t, http.MethodPatch, "/api/task-lists/"+strconv.FormatInt(list.ID, 10)+"/color", bobToken, workspaceID, recolorTaskListRequest{Color: "#E2483DFF"})
	defer recolorResp.Body.Close()
	if recolorResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 recoloring, got %d", recolorResp.StatusCode)
	}

	setDefaultResp := s.do(t, http.MethodPut, "/api/task-lists/"+strconv.FormatInt(list.ID, 10)+"/default", bobToken, workspaceID, nil)
	defer setDefaultResp.Body.Close()
	if setDefaultResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 setting default, got %d", setDefaultResp.StatusCode)
	}

	deleteResp := s.do(t, http.MethodDelete, "/api/task-lists/"+strconv.FormatInt(list.ID, 10), bobToken, workspaceID, nil)
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 deleting, got %d", deleteResp.StatusCode)
	}

	// alice's Task List survived every one of bob's attempts, untouched.
	aliceLists := s.listTaskLists(t, aliceToken, workspaceID)
	found := false
	for _, l := range aliceLists {
		if l.ID == list.ID && l.Name == "Work" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected alice's task list to survive untouched, got %v", aliceLists)
	}
}

func TestTaskListHandler_SetDefault_PromotesAndClearsOldDefault(t *testing.T) {
	s := newTaskListHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	work := s.createTaskList(t, token, workspaceID, "Work", "#12809CFF")

	resp := s.do(t, http.MethodPut, "/api/task-lists/"+strconv.FormatInt(work.ID, 10)+"/default", token, workspaceID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 setting default, got %d", resp.StatusCode)
	}
	var promoted taskListResponse
	if err := json.NewDecoder(resp.Body).Decode(&promoted); err != nil {
		t.Fatalf("decode set-default response: %v", err)
	}
	if !promoted.IsDefault {
		t.Fatalf("expected Work to be the new default")
	}

	lists := s.listTaskLists(t, token, workspaceID)
	defaultCount := 0
	var inboxIsDefault, workIsDefault bool
	for _, l := range lists {
		if l.IsDefault {
			defaultCount++
		}
		switch l.Name {
		case "Inbox":
			inboxIsDefault = l.IsDefault
		case "Work":
			workIsDefault = l.IsDefault
		}
	}
	if defaultCount != 1 {
		t.Fatalf("expected exactly one default task list, got %d in %v", defaultCount, lists)
	}
	if inboxIsDefault {
		t.Fatalf("expected Inbox to no longer be default, got %v", lists)
	}
	if !workIsDefault {
		t.Fatalf("expected Work to be default, got %v", lists)
	}
}

func TestTaskListHandler_Delete_RefusesWhileDefault(t *testing.T) {
	s := newTaskListHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	lists := s.listTaskLists(t, token, workspaceID)
	inbox := lists[0]
	if !inbox.IsDefault {
		t.Fatalf("expected the auto-provisioned Inbox to be default, got %v", inbox)
	}

	resp := s.do(t, http.MethodDelete, "/api/task-lists/"+strconv.FormatInt(inbox.ID, 10), token, workspaceID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 deleting the default task list, got %d", resp.StatusCode)
	}

	// Still there.
	survivors := s.listTaskLists(t, token, workspaceID)
	if len(survivors) != 1 {
		t.Fatalf("expected the default task list to survive the refused delete, got %v", survivors)
	}
}

func TestTaskListHandler_Delete_AllowedAfterPromotingAnotherToDefault(t *testing.T) {
	s := newTaskListHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	lists := s.listTaskLists(t, token, workspaceID)
	inbox := lists[0]

	work := s.createTaskList(t, token, workspaceID, "Work", "#12809CFF")

	promoteResp := s.do(t, http.MethodPut, "/api/task-lists/"+strconv.FormatInt(work.ID, 10)+"/default", token, workspaceID, nil)
	defer promoteResp.Body.Close()
	if promoteResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 promoting Work, got %d", promoteResp.StatusCode)
	}

	deleteResp := s.do(t, http.MethodDelete, "/api/task-lists/"+strconv.FormatInt(inbox.ID, 10), token, workspaceID, nil)
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 deleting the former default, got %d", deleteResp.StatusCode)
	}

	survivors := s.listTaskLists(t, token, workspaceID)
	if len(survivors) != 1 || survivors[0].Name != "Work" {
		t.Fatalf("expected only Work to survive, got %v", survivors)
	}
}

// TestTaskListHandler_CascadesOnWorkspaceDeletion covers task_lists.
// workspace_id's ON DELETE CASCADE (ADR-0083, mirroring calendar_sets').
func TestTaskListHandler_CascadesOnWorkspaceDeletion(t *testing.T) {
	s := newTaskListHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	lists := s.listTaskLists(t, token, workspaceID)
	if len(lists) != 1 {
		t.Fatalf("expected exactly the auto-provisioned Inbox, got %v", lists)
	}

	if _, err := s.graph.DB.Exec("DELETE FROM workspaces WHERE id = ?", workspaceID); err != nil {
		t.Fatalf("delete workspace: %v", err)
	}

	var count int
	if err := s.graph.DB.QueryRow("SELECT COUNT(*) FROM task_lists WHERE workspace_id = ?", workspaceID).Scan(&count); err != nil {
		t.Fatalf("count task lists: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected deleting the workspace to cascade every task list in it, got %d remaining", count)
	}
}

// TestTaskListHandler_CascadesOnUserDeletion covers task_lists.user_id's ON
// DELETE CASCADE (ADR-0083), isolated from the workspace_id cascade above:
// bob's own Workspace (and its Inbox) is removed first — the order
// cleanupFailedRegistration already uses, since workspaces.owner_user_id
// carries no ON DELETE behaviour — leaving only the Inbox
// AcceptWorkspaceInviteExisting provisioned for him inside alice's
// Workspace, which only the user_id cascade can remove.
func TestTaskListHandler_CascadesOnUserDeletion(t *testing.T) {
	s := newTaskListHandlerTestServer(t)
	aliceToken, _, aliceWorkspaceID := s.register(t, "alice")
	bobToken, bobID, bobWorkspaceID := s.register(t, "bob")

	inviteToken := s.createInvite(t, aliceToken, aliceWorkspaceID, "bob@example.com")
	s.acceptExisting(t, bobToken, inviteToken)

	bobListsInAlice := s.listTaskLists(t, bobToken, aliceWorkspaceID)
	if len(bobListsInAlice) != 1 {
		t.Fatalf("expected bob to have his own Inbox in alice's workspace, got %v", bobListsInAlice)
	}

	if _, err := s.graph.DB.Exec("DELETE FROM workspaces WHERE id = ?", bobWorkspaceID); err != nil {
		t.Fatalf("delete bob's own workspace: %v", err)
	}
	if _, err := s.graph.DB.Exec("DELETE FROM users WHERE id = ?", bobID); err != nil {
		t.Fatalf("delete bob: %v", err)
	}

	var count int
	if err := s.graph.DB.QueryRow("SELECT COUNT(*) FROM task_lists WHERE user_id = ?", bobID).Scan(&count); err != nil {
		t.Fatalf("count task lists: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected deleting bob to cascade his remaining task list (the Inbox in alice's workspace), got %d remaining", count)
	}
}
