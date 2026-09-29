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
	// Membership (#302) validates against a real Calendar, so this server
	// also needs enough of /api/calendars to create and delete one — Create
	// for setup, Delete to exercise the cascade leaving a Set intact.
	calendarHandler := NewCalendarHandler(g.Calendars, g.Events, g.Imports, g.Subscriptions, g.Connections, g.AttachmentStore)

	r := chi.NewRouter()
	r.Post("/api/auth/register", authHandler.Register)
	r.Route("/api/calendars", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Use(httpauth.RequireEnabledUser(auth))

		r.With(httpauth.RequireWorkspace(workspaces)).Post("/", calendarHandler.Create)
		r.Delete("/{id}", calendarHandler.Delete)
	})
	r.Route("/api/calendar-sets", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Use(httpauth.RequireEnabledUser(auth))
		r.Use(httpauth.RequireWorkspace(workspaces))

		r.Get("/", calendarSetHandler.List)
		r.Post("/", calendarSetHandler.Create)
		r.Patch("/{id}", calendarSetHandler.Rename)
		r.Delete("/{id}", calendarSetHandler.Delete)
		r.Put("/{id}/calendars/{calendarId}", calendarSetHandler.AddCalendar)
		r.Delete("/{id}/calendars/{calendarId}", calendarSetHandler.RemoveCalendar)
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

// createSet creates a Calendar Set named name for accessToken in
// workspaceID and returns its id, failing the test on anything but 201.
func (s *calendarSetHandlerTestServer) createSet(t *testing.T, accessToken string, workspaceID int64, name string) int64 {
	t.Helper()

	resp := s.do(t, http.MethodPost, "/api/calendar-sets/", accessToken, workspaceID, createCalendarSetRequest{Name: name})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating calendar set, got %d", resp.StatusCode)
	}
	var set calendarSetResponse
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		t.Fatalf("decode create calendar set response: %v", err)
	}
	return set.ID
}

// getSet finds id in accessToken's List of workspaceID's Calendar Sets,
// failing the test if it isn't there.
func (s *calendarSetHandlerTestServer) getSet(t *testing.T, accessToken string, workspaceID, id int64) calendarSetResponse {
	t.Helper()

	resp := s.do(t, http.MethodGet, "/api/calendar-sets/", accessToken, workspaceID, nil)
	defer resp.Body.Close()
	var list []calendarSetResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode calendar sets list: %v", err)
	}
	for _, set := range list {
		if set.ID == id {
			return set
		}
	}
	t.Fatalf("calendar set %d not found in list %v", id, list)
	return calendarSetResponse{}
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

// addCalendarToSet PUTs calendarID into setID's membership.
func (s *calendarSetHandlerTestServer) addCalendarToSet(t *testing.T, accessToken string, workspaceID, setID int64, calendarID string) *http.Response {
	t.Helper()
	return s.do(t, http.MethodPut, "/api/calendar-sets/"+strconv.FormatInt(setID, 10)+"/calendars/"+calendarID, accessToken, workspaceID, nil)
}

// removeCalendarFromSet DELETEs calendarID out of setID's membership.
func (s *calendarSetHandlerTestServer) removeCalendarFromSet(t *testing.T, accessToken string, workspaceID, setID int64, calendarID string) *http.Response {
	t.Helper()
	return s.do(t, http.MethodDelete, "/api/calendar-sets/"+strconv.FormatInt(setID, 10)+"/calendars/"+calendarID, accessToken, workspaceID, nil)
}

func TestCalendarSetHandler_AddAndRemoveCalendar(t *testing.T) {
	s := newCalendarSetHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	setID := s.createSet(t, token, workspaceID, "Work")
	calendarID := "11111111-1111-1111-1111-111111111111"
	createResp := createCalendar(t, s.srv.URL, token, strconv.FormatInt(workspaceID, 10), calendarID, "Personal", "#12809CFF")
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating calendar, got %d", createResp.StatusCode)
	}

	addResp := s.addCalendarToSet(t, token, workspaceID, setID, calendarID)
	defer addResp.Body.Close()
	if addResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 adding calendar, got %d", addResp.StatusCode)
	}

	set := s.getSet(t, token, workspaceID, setID)
	if len(set.CalendarIDs) != 1 || set.CalendarIDs[0] != calendarID {
		t.Fatalf("expected set to contain %q, got %v", calendarID, set.CalendarIDs)
	}

	// PUT is idempotent: re-adding an already-member Calendar is a no-op,
	// not a conflict.
	addAgainResp := s.addCalendarToSet(t, token, workspaceID, setID, calendarID)
	defer addAgainResp.Body.Close()
	if addAgainResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 re-adding calendar, got %d", addAgainResp.StatusCode)
	}
	set = s.getSet(t, token, workspaceID, setID)
	if len(set.CalendarIDs) != 1 {
		t.Fatalf("expected re-adding to stay a single membership, got %v", set.CalendarIDs)
	}

	removeResp := s.removeCalendarFromSet(t, token, workspaceID, setID, calendarID)
	defer removeResp.Body.Close()
	if removeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 removing calendar, got %d", removeResp.StatusCode)
	}

	set = s.getSet(t, token, workspaceID, setID)
	if len(set.CalendarIDs) != 0 {
		t.Fatalf("expected set to be empty after removal, got %v", set.CalendarIDs)
	}
}

// TestCalendarSetHandler_Rename_PreservesMembership guards against Rename's
// response silently truncating a populated Set's membership to empty: the
// frontend store replaces its whole cached Set with whatever Rename
// answers, so a wrong CalendarIDs here would wipe a Set's membership from
// the UI on every rename.
func TestCalendarSetHandler_Rename_PreservesMembership(t *testing.T) {
	s := newCalendarSetHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	setID := s.createSet(t, token, workspaceID, "Work")
	calendarID := "77777777-7777-7777-7777-777777777777"
	createResp := createCalendar(t, s.srv.URL, token, strconv.FormatInt(workspaceID, 10), calendarID, "Personal", "#12809CFF")
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating calendar, got %d", createResp.StatusCode)
	}

	addResp := s.addCalendarToSet(t, token, workspaceID, setID, calendarID)
	defer addResp.Body.Close()
	if addResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 adding calendar, got %d", addResp.StatusCode)
	}

	renameResp := s.do(t, http.MethodPatch, "/api/calendar-sets/"+strconv.FormatInt(setID, 10), token, workspaceID, renameCalendarSetRequest{Name: "Weekend"})
	defer renameResp.Body.Close()
	if renameResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 renaming, got %d", renameResp.StatusCode)
	}
	var renamed calendarSetResponse
	if err := json.NewDecoder(renameResp.Body).Decode(&renamed); err != nil {
		t.Fatalf("decode rename response: %v", err)
	}
	if renamed.Name != "Weekend" {
		t.Fatalf("expected renamed name %q, got %q", "Weekend", renamed.Name)
	}
	if len(renamed.CalendarIDs) != 1 || renamed.CalendarIDs[0] != calendarID {
		t.Fatalf("expected the rename response to carry the set's existing membership, got %v", renamed.CalendarIDs)
	}
}

func TestCalendarSetHandler_AddCalendar_FromAnotherWorkspaceRefused(t *testing.T) {
	s := newCalendarSetHandlerTestServer(t)
	token, userID, firstWorkspaceID := s.register(t, "alice")

	setID := s.createSet(t, token, firstWorkspaceID, "Work")

	secondWorkspace, err := s.graph.Workspaces.CreateForOwner(context.Background(), userID, "Second workspace")
	if err != nil {
		t.Fatalf("create second workspace: %v", err)
	}

	calendarID := "22222222-2222-2222-2222-222222222222"
	createResp := createCalendar(t, s.srv.URL, token, strconv.FormatInt(secondWorkspace.ID, 10), calendarID, "Other workspace", "#12809CFF")
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating calendar in the second workspace, got %d", createResp.StatusCode)
	}

	addResp := s.addCalendarToSet(t, token, firstWorkspaceID, setID, calendarID)
	defer addResp.Body.Close()
	if addResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 adding a calendar from another workspace, got %d", addResp.StatusCode)
	}

	set := s.getSet(t, token, firstWorkspaceID, setID)
	if len(set.CalendarIDs) != 0 {
		t.Fatalf("expected the refused add to leave the set empty, got %v", set.CalendarIDs)
	}
}

func TestCalendarSetHandler_AddCalendar_NoAccessRefused(t *testing.T) {
	s := newCalendarSetHandlerTestServer(t)
	aliceToken, _, workspaceID := s.register(t, "alice")
	bobToken, bobID, _ := s.register(t, "bob")
	// bob is a Member of alice's Workspace, so this proves the Access check
	// is independent of the Workspace check: same Workspace, still refused,
	// because alice owns bob's Calendar not at all and holds no Share on it.
	if _, err := s.graph.DB.Exec("INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)", workspaceID, bobID, "member"); err != nil {
		t.Fatalf("add bob to alice's workspace: %v", err)
	}

	setID := s.createSet(t, aliceToken, workspaceID, "Work")

	calendarID := "33333333-3333-3333-3333-333333333333"
	createResp := createCalendar(t, s.srv.URL, bobToken, strconv.FormatInt(workspaceID, 10), calendarID, "Bob's calendar", "#12809CFF")
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating bob's calendar, got %d", createResp.StatusCode)
	}

	addResp := s.addCalendarToSet(t, aliceToken, workspaceID, setID, calendarID)
	defer addResp.Body.Close()
	if addResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 adding a calendar alice has no access to, got %d", addResp.StatusCode)
	}

	set := s.getSet(t, aliceToken, workspaceID, setID)
	if len(set.CalendarIDs) != 0 {
		t.Fatalf("expected the refused add to leave the set empty, got %v", set.CalendarIDs)
	}
}

func TestCalendarSetHandler_CalendarCanBelongToSeveralSets(t *testing.T) {
	s := newCalendarSetHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	workSetID := s.createSet(t, token, workspaceID, "Work")
	weekendSetID := s.createSet(t, token, workspaceID, "Weekend")

	calendarID := "44444444-4444-4444-4444-444444444444"
	createResp := createCalendar(t, s.srv.URL, token, strconv.FormatInt(workspaceID, 10), calendarID, "Shared calendar", "#12809CFF")
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating calendar, got %d", createResp.StatusCode)
	}

	for _, setID := range []int64{workSetID, weekendSetID} {
		addResp := s.addCalendarToSet(t, token, workspaceID, setID, calendarID)
		defer addResp.Body.Close()
		if addResp.StatusCode != http.StatusNoContent {
			t.Fatalf("expected 204 adding calendar to set %d, got %d", setID, addResp.StatusCode)
		}
	}

	workSet := s.getSet(t, token, workspaceID, workSetID)
	weekendSet := s.getSet(t, token, workspaceID, weekendSetID)
	if len(workSet.CalendarIDs) != 1 || workSet.CalendarIDs[0] != calendarID {
		t.Fatalf("expected Work to contain the calendar, got %v", workSet.CalendarIDs)
	}
	if len(weekendSet.CalendarIDs) != 1 || weekendSet.CalendarIDs[0] != calendarID {
		t.Fatalf("expected Weekend to also contain the calendar, got %v", weekendSet.CalendarIDs)
	}
}

func TestCalendarSetHandler_SecondUserCannotAddOrRemoveFromFirstsSet(t *testing.T) {
	s := newCalendarSetHandlerTestServer(t)
	aliceToken, _, workspaceID := s.register(t, "alice")
	bobToken, bobID, _ := s.register(t, "bob")
	if _, err := s.graph.DB.Exec("INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)", workspaceID, bobID, "member"); err != nil {
		t.Fatalf("add bob to alice's workspace: %v", err)
	}

	setID := s.createSet(t, aliceToken, workspaceID, "Work")
	calendarID := "55555555-5555-5555-5555-555555555555"
	createResp := createCalendar(t, s.srv.URL, aliceToken, strconv.FormatInt(workspaceID, 10), calendarID, "Personal", "#12809CFF")
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating calendar, got %d", createResp.StatusCode)
	}

	bobAddResp := s.addCalendarToSet(t, bobToken, workspaceID, setID, calendarID)
	defer bobAddResp.Body.Close()
	if bobAddResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 adding to another user's set, got %d", bobAddResp.StatusCode)
	}

	aliceAddResp := s.addCalendarToSet(t, aliceToken, workspaceID, setID, calendarID)
	defer aliceAddResp.Body.Close()
	if aliceAddResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", aliceAddResp.StatusCode)
	}

	bobRemoveResp := s.removeCalendarFromSet(t, bobToken, workspaceID, setID, calendarID)
	defer bobRemoveResp.Body.Close()
	if bobRemoveResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 removing from another user's set, got %d", bobRemoveResp.StatusCode)
	}

	set := s.getSet(t, aliceToken, workspaceID, setID)
	if len(set.CalendarIDs) != 1 || set.CalendarIDs[0] != calendarID {
		t.Fatalf("expected alice's membership to survive bob's attempts, got %v", set.CalendarIDs)
	}
}

func TestCalendarSetHandler_DeletingCalendarCascadesOutOfSet(t *testing.T) {
	s := newCalendarSetHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	setID := s.createSet(t, token, workspaceID, "Work")
	calendarID := "66666666-6666-6666-6666-666666666666"
	createResp := createCalendar(t, s.srv.URL, token, strconv.FormatInt(workspaceID, 10), calendarID, "Personal", "#12809CFF")
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating calendar, got %d", createResp.StatusCode)
	}

	addResp := s.addCalendarToSet(t, token, workspaceID, setID, calendarID)
	defer addResp.Body.Close()
	if addResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 adding calendar, got %d", addResp.StatusCode)
	}

	deleteReq, err := http.NewRequest(http.MethodDelete, s.srv.URL+"/api/calendars/"+calendarID, nil)
	if err != nil {
		t.Fatalf("build delete calendar request: %v", err)
	}
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete calendar: %v", err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 deleting calendar, got %d", deleteResp.StatusCode)
	}

	// The cascade (calendar_set_members.calendar_id ON DELETE CASCADE,
	// ADR-0082) empties the Set's membership with no reconciliation code and
	// no notification — and the Set itself survives.
	set := s.getSet(t, token, workspaceID, setID)
	if set.Name != "Work" {
		t.Fatalf("expected the set to survive with its name intact, got %v", set)
	}
	if len(set.CalendarIDs) != 0 {
		t.Fatalf("expected the cascade to empty the set's membership, got %v", set.CalendarIDs)
	}
}
