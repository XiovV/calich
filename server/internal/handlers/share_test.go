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
	"golang.org/x/crypto/bcrypt"

	"github.com/XiovV/calendar/server/internal/attachmentstore"
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
	workspaceID            string
	groups                 *repository.GroupRepository
	groupID                int64
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
	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspaceSvc := service.NewWorkspaceService(sqlDB, workspaceRepo, repository.NewWorkspaceInviteRepository(sqlDB), repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB))
	calendarRepo := repository.NewCalendarRepository(sqlDB)
	shareRepo := repository.NewCalendarShareRepository(sqlDB)
	groupRepo := repository.NewGroupRepository(sqlDB)
	calendars := service.NewCalendarService(calendarRepo, shareRepo, users, repository.NewEventReminderRepository(sqlDB), repository.NewCalendarDefaultReminderRepository(sqlDB), repository.NewEventReminderExplicitRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), groupRepo)
	auth := service.NewAuthService(users, sessions, workspaceSvc, repository.NewWorkspaceInviteRepository(sqlDB), calendars, repository.NewAttendeeRepository(sqlDB), []byte("test-secret"), "owner", "owner@example.com", "hunter2", false)
	ctx := context.Background()
	ownerUser, _, err := auth.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	otherHash, err := bcrypt.GenerateFromPassword([]byte("temp-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash other user's password: %v", err)
	}
	other, err := users.Create(ctx, "other", "other@example.com", string(otherHash), false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}
	ownerLogin, err := auth.Login(ctx, "owner@example.com", "hunter2")
	if err != nil {
		t.Fatalf("owner login: %v", err)
	}
	otherLogin, err := auth.Login(ctx, "other@example.com", "temp-password")
	if err != nil {
		t.Fatalf("other login: %v", err)
	}

	ownerWorkspaces, err := workspaceSvc.ListForUser(ctx, ownerUser.ID)
	if err != nil || len(ownerWorkspaces) == 0 {
		t.Fatalf("list owner's workspaces: %v", err)
	}
	ownerWorkspaceID := strconv.FormatInt(ownerWorkspaces[0].ID, 10)

	// "other" needs to belong to owner's Workspace too — GET /api/calendars
	// is Workspace-scoped now (#155, ADR-0045): RequireWorkspace checks the
	// caller is a Member of the X-Workspace-Id they claim, and List only
	// surfaces Shared calendars whose own workspace_id matches it. A Share
	// grant alone (ADR-0034) isn't restricted by Workspace membership, but
	// seeing that share through the workspace-scoped List endpoint requires
	// it.
	if err := workspaceRepo.AddMember(ctx, ownerWorkspaces[0].ID, other.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add other as owner's workspace member: %v", err)
	}

	events := service.NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewCalendarDefaultReminderRepository(sqlDB), repository.NewEventReminderExplicitRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendars, users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo, repository.NewGroupRepository(sqlDB), repository.NewNotificationRepository(sqlDB), nil, 1000)
	attachmentStore := attachmentstore.New(t.TempDir())
	imports := service.NewImportService(events, calendars, attachmentStore, testMaxAttachmentSize, testMaxAttachmentsPerEvent)
	subscriptions := service.NewSubscribeService(events, calendars, 0, service.WithHTTPClient(&http.Client{}))
	calendarHandler := NewCalendarHandler(calendars, events, imports, subscriptions, attachmentStore)

	r := chi.NewRouter()
	r.Route("/api/calendars", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.With(httpauth.RequireWorkspace(workspaceSvc)).Post("/", calendarHandler.Create)
		r.With(httpauth.RequireWorkspace(workspaceSvc)).Get("/", calendarHandler.List)
		r.Get("/{id}", calendarHandler.Get)
		r.Patch("/{id}", calendarHandler.Update)
		r.Get("/{id}/shares", calendarHandler.ListShares)
		r.Post("/{id}/shares", calendarHandler.Share)
		r.Delete("/{id}/shares/{userId}", calendarHandler.RevokeShare)
		r.Post("/{id}/leave", calendarHandler.LeaveShare)
		r.Get("/{id}/group-shares", calendarHandler.ListGroupShares)
		r.Post("/{id}/group-shares", calendarHandler.ShareWithGroup)
		r.Delete("/{id}/group-shares/{groupId}", calendarHandler.RevokeGroupShare)
		r.Get("/{id}/share-targets", calendarHandler.ShareTargets)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp := createCalendar(t, srv.URL, ownerLogin.AccessToken, ownerWorkspaceID, "11111111-1111-1111-1111-111111111111", "Family", "#12809CFF")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create calendar: expected 201, got %d", resp.StatusCode)
	}
	var created calendarResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created calendar: %v", err)
	}
	resp.Body.Close()

	group, err := groupRepo.Create(ctx, ownerWorkspaces[0].ID, "Tech team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	return shareTestServer{baseURL: srv.URL, ownerToken: ownerLogin.AccessToken, otherToken: otherLogin.AccessToken, calendarID: created.ID, otherUserID: other.ID, calendars: calendars, workspaceID: ownerWorkspaceID, groups: groupRepo, groupID: group.ID}
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

	resp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Email: "other@example.com", Role: repository.RoleEditor})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got shareResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "other" || got.Role != repository.RoleEditor || got.UserID != s.otherUserID {
		t.Fatalf("unexpected share: %+v", got)
	}
}

// TestCalendarHandler_List_CarriesAccess covers "Calendar list responses
// carry the caller's resolved Access" (#100's acceptance criteria).
func TestCalendarHandler_List_CarriesAccess(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Email: "other@example.com", Role: repository.RoleViewer})
	shareResp.Body.Close()
	if shareResp.StatusCode != http.StatusOK {
		t.Fatalf("share: expected 200, got %d", shareResp.StatusCode)
	}

	ownerList, err := authenticatedGetWithWorkspace(s.baseURL+"/api/calendars/", s.ownerToken, s.workspaceID)
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

	otherList, err := authenticatedGetWithWorkspace(s.baseURL+"/api/calendars/", s.otherToken, s.workspaceID)
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

// TestCalendarHandler_List_CarriesOwnershipMeta covers #111's acceptance
// criteria: List and Get responses carry isOwner, ownerUsername and
// shareCount, and isOwner stays true for the Owner of a Subscribed Calendar
// even though the Subscription clamps their Access to Viewer.
func TestCalendarHandler_List_CarriesOwnershipMeta(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Email: "other@example.com", Role: repository.RoleViewer})
	shareResp.Body.Close()

	ownerList, err := authenticatedGetWithWorkspace(s.baseURL+"/api/calendars/", s.ownerToken, s.workspaceID)
	if err != nil {
		t.Fatalf("owner list: %v", err)
	}
	defer ownerList.Body.Close()
	var ownerCalendars []calendarResponse
	if err := json.NewDecoder(ownerList.Body).Decode(&ownerCalendars); err != nil {
		t.Fatalf("decode owner list: %v", err)
	}
	if len(ownerCalendars) != 1 || !ownerCalendars[0].IsOwner || ownerCalendars[0].OwnerName != "owner" || ownerCalendars[0].ShareCount != 1 {
		t.Fatalf("unexpected owner list: %+v", ownerCalendars)
	}

	otherList, err := authenticatedGetWithWorkspace(s.baseURL+"/api/calendars/", s.otherToken, s.workspaceID)
	if err != nil {
		t.Fatalf("other list: %v", err)
	}
	defer otherList.Body.Close()
	var otherCalendars []calendarResponse
	if err := json.NewDecoder(otherList.Body).Decode(&otherCalendars); err != nil {
		t.Fatalf("decode other list: %v", err)
	}
	if len(otherCalendars) != 1 || otherCalendars[0].IsOwner || otherCalendars[0].OwnerName != "owner" || otherCalendars[0].ShareCount != 1 {
		t.Fatalf("unexpected shared-with list: %+v", otherCalendars)
	}

	otherGet, err := authenticatedGet(s.baseURL+"/api/calendars/"+s.calendarID, s.otherToken)
	if err != nil {
		t.Fatalf("other get: %v", err)
	}
	defer otherGet.Body.Close()
	var otherGetBody calendarResponse
	if err := json.NewDecoder(otherGet.Body).Decode(&otherGetBody); err != nil {
		t.Fatalf("decode other get: %v", err)
	}
	if otherGetBody.IsOwner || otherGetBody.OwnerName != "owner" {
		t.Fatalf("unexpected single-calendar response for a Viewer: %+v", otherGetBody)
	}
}

// TestCalendarHandler_List_And_Get_ResolveColorPerCaller covers ADR-0038's
// "effective colour returned with every Calendar": a User's own colour
// override — set here directly through the service rather than through
// Update's REST path, which calendar_color_update_test.go covers on its own
// — is what List and Get report back to that User, while the Owner's own
// response stays the Calendar's own stored colour.
func TestCalendarHandler_List_And_Get_ResolveColorPerCaller(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Email: "other@example.com", Role: repository.RoleEditor})
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

	otherList, err := authenticatedGetWithWorkspace(s.baseURL+"/api/calendars/", s.otherToken, s.workspaceID)
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

	resp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.otherToken, shareRequest{Email: "other@example.com", Role: repository.RoleViewer})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Share_UnknownEmail(t *testing.T) {
	s := newShareTestServer(t)

	resp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Email: "ghost@example.com", Role: repository.RoleViewer})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_ListShares(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Email: "other@example.com", Role: repository.RoleEditor})
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
	if len(shares) != 1 || shares[0].Name != "other" || shares[0].Role != repository.RoleEditor || shares[0].UserID != s.otherUserID {
		t.Fatalf("unexpected shares: %+v", shares)
	}
}

func TestCalendarHandler_RevokeShare(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Email: "other@example.com", Role: repository.RoleEditor})
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

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Email: "other@example.com", Role: repository.RoleViewer})
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

// TestCalendarHandler_ShareWithGroup_And_ListGroupShares covers #159's
// Group-targeted Share REST surface end to end: granting one and listing it
// back.
func TestCalendarHandler_ShareWithGroup_And_ListGroupShares(t *testing.T) {
	s := newShareTestServer(t)

	resp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/group-shares", s.ownerToken, groupShareRequest{GroupID: s.groupID, Role: repository.RoleEditor})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var got groupShareResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.GroupID != s.groupID || got.GroupName != "Tech team" || got.Role != repository.RoleEditor {
		t.Fatalf("unexpected group share: %+v", got)
	}

	listResp, err := authenticatedGet(s.baseURL+"/api/calendars/"+s.calendarID+"/group-shares", s.ownerToken)
	if err != nil {
		t.Fatalf("list group shares: %v", err)
	}
	defer listResp.Body.Close()
	var shares []groupShareResponse
	if err := json.NewDecoder(listResp.Body).Decode(&shares); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(shares) != 1 || shares[0].GroupID != s.groupID {
		t.Fatalf("unexpected group shares: %+v", shares)
	}
}

// TestCalendarHandler_RevokeGroupShare covers revoking a Group Share over
// REST.
func TestCalendarHandler_RevokeGroupShare(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/group-shares", s.ownerToken, groupShareRequest{GroupID: s.groupID, Role: repository.RoleViewer})
	shareResp.Body.Close()

	resp := doJSON(t, http.MethodDelete, s.baseURL+"/api/calendars/"+s.calendarID+"/group-shares/"+strconv.FormatInt(s.groupID, 10), s.ownerToken, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	listResp, err := authenticatedGet(s.baseURL+"/api/calendars/"+s.calendarID+"/group-shares", s.ownerToken)
	if err != nil {
		t.Fatalf("list group shares: %v", err)
	}
	defer listResp.Body.Close()
	var shares []groupShareResponse
	if err := json.NewDecoder(listResp.Body).Decode(&shares); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(shares) != 0 {
		t.Fatalf("expected no group shares after revoke, got %+v", shares)
	}
}

// TestCalendarHandler_ShareTargets covers #159's share-dialog data source:
// every other Member and every Group of the Calendar's own Workspace.
func TestCalendarHandler_ShareTargets(t *testing.T) {
	s := newShareTestServer(t)

	resp, err := authenticatedGet(s.baseURL+"/api/calendars/"+s.calendarID+"/share-targets", s.ownerToken)
	if err != nil {
		t.Fatalf("share targets: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var got shareTargetsResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Users) != 1 || got.Users[0].Name != "other" {
		t.Fatalf("unexpected user targets: %+v", got.Users)
	}
	if len(got.Groups) != 1 || got.Groups[0].GroupID != s.groupID {
		t.Fatalf("unexpected group targets: %+v", got.Groups)
	}
}
