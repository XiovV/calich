// event_attendee_email_test.go covers #200/ADR-0058 at the HTTP layer:
// POST /api/events/ accepting attendeeEmails, POST .../attendees accepting
// an email body, and DELETE .../attendees/email/{email}.
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/attachmentstore"
	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

// emailAttendeeHandlerFixture mirrors createAttendeeHandlerFixture
// (event_attendee_create_test.go) with the actors #200's wire-level tests
// need: alice (the Organizer, bootstrapped Owner), a plain Workspace
// Member, and every attendee route wired up (List/Add/Remove-by-email, plus
// Create).
type emailAttendeeHandlerFixture struct {
	baseURL     string
	accessToken string
	calendarID  string
	memberID    int64
}

func newEmailAttendeeHandlerFixture(t *testing.T) emailAttendeeHandlerFixture {
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
	calendarRepo := repository.NewCalendarRepository(sqlDB)
	groupRepo := repository.NewGroupRepository(sqlDB)
	calendars := service.NewCalendarService(calendarRepo, repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), groupRepo)
	auth := service.NewAuthService(users, sessions, workspaces, repository.NewWorkspaceInviteRepository(sqlDB), calendars, repository.NewAttendeeRepository(sqlDB), []byte("test-secret"), "alice", "alice@example.com", "hunter2", false)
	bootstrapUser, _, err := auth.Bootstrap(context.Background())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	aliceWorkspaces, err := workspaces.ListForUser(context.Background(), bootstrapUser.ID)
	if err != nil {
		t.Fatalf("list workspaces: %v", err)
	}
	workspaceID := aliceWorkspaces[0].ID

	loginResult, err := auth.Login(context.Background(), "alice@example.com", "hunter2")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	userID, err := auth.Authenticate(context.Background(), loginResult.AccessToken)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	member, err := users.Create(context.Background(), "member", "member@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := workspaceRepo.AddMember(context.Background(), workspaceID, member.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}

	cal, err := calendars.Create(context.Background(), userID, workspaceID, "11111111-1111-1111-1111-111111111111", service.CalendarWrite{Name: "Personal", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	events := service.NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendars, users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo, groupRepo, repository.NewNotificationRepository(sqlDB), repository.NewOutboxRepository(sqlDB), 1000)
	eventHandler := NewEventHandler(events, attachmentstore.New(t.TempDir()))

	r := chi.NewRouter()
	r.Route("/api/events", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Post("/", eventHandler.Create)
		r.Get("/{id}/attendees", eventHandler.ListAttendees)
		r.Post("/{id}/attendees", eventHandler.AddAttendee)
		r.Delete("/{id}/attendees/email/{email}", eventHandler.RemoveAttendeeByEmail)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return emailAttendeeHandlerFixture{
		baseURL: srv.URL, accessToken: loginResult.AccessToken, calendarID: cal.ID, memberID: member.ID,
	}
}

func (f emailAttendeeHandlerFixture) postJSON(t *testing.T, path string, body any) *http.Response {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, f.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func (f emailAttendeeHandlerFixture) createEvent(t *testing.T, id string, attendeeEmails []string) *http.Response {
	t.Helper()
	return f.postJSON(t, "/api/events/", map[string]any{
		"id":             id,
		"calendarId":     f.calendarID,
		"title":          "Standup",
		"start":          "2026-01-01T09:00:00Z",
		"end":            "2026-01-01T10:00:00Z",
		"attendeeEmails": attendeeEmails,
	})
}

// TestEventHandler_Create_WithAttendeeEmails_InvitesAnOutsider covers the
// AC bullet at the wire level: attendeeEmails on the create body invites an
// address with no account here, producing an email-shaped Attendee.
func TestEventHandler_Create_WithAttendeeEmails_InvitesAnOutsider(t *testing.T) {
	f := newEmailAttendeeHandlerFixture(t)

	resp := f.createEvent(t, "22222222-2222-2222-2222-222222222222", []string{"guest@example.com"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var created decodedEvent
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	attendeesResp, err := authenticatedGet(f.baseURL+"/api/events/"+created.ID+"/attendees", f.accessToken)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	defer attendeesResp.Body.Close()

	var attendees []attendeeWire
	if err := json.NewDecoder(attendeesResp.Body).Decode(&attendees); err != nil {
		t.Fatalf("decode attendees: %v", err)
	}
	if len(attendees) != 1 || attendees[0].UserID != nil || attendees[0].Email != "guest@example.com" {
		t.Fatalf("expected one email-shaped attendee for guest@example.com, got %+v", attendees)
	}
}

// TestEventHandler_Create_WithMalformedAttendeeEmail_Rejects400AndCreatesNoEvent
// covers the AC bullet: a malformed address rejects the whole request with
// a 400, and the Event is never created.
func TestEventHandler_Create_WithMalformedAttendeeEmail_Rejects400AndCreatesNoEvent(t *testing.T) {
	f := newEmailAttendeeHandlerFixture(t)

	resp := f.createEvent(t, "22222222-2222-2222-2222-222222222222", []string{"not-an-email"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	getResp, err := authenticatedGet(f.baseURL+"/api/events/22222222-2222-2222-2222-222222222222", f.accessToken)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected the event to not exist (404), got %d", getResp.StatusCode)
	}
}

// TestEventHandler_AddAttendee_WithEmailBody_InvitesByAddress covers
// POST .../attendees dispatching to the email path when the body carries
// email instead of userId.
func TestEventHandler_AddAttendee_WithEmailBody_InvitesByAddress(t *testing.T) {
	f := newEmailAttendeeHandlerFixture(t)
	createResp := f.createEvent(t, "22222222-2222-2222-2222-222222222222", nil)
	createResp.Body.Close()

	resp := f.postJSON(t, "/api/events/22222222-2222-2222-2222-222222222222/attendees", map[string]any{"email": "guest@example.com"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var attendee attendeeWire
	if err := json.NewDecoder(resp.Body).Decode(&attendee); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if attendee.UserID != nil || attendee.Email != "guest@example.com" {
		t.Fatalf("expected an email-shaped attendee wire, got %+v", attendee)
	}
}

// TestEventHandler_AddAttendee_BothUserIdAndEmail_Rejects400 covers the
// mutually-exclusive request-body contract.
func TestEventHandler_AddAttendee_BothUserIdAndEmail_Rejects400(t *testing.T) {
	f := newEmailAttendeeHandlerFixture(t)
	createResp := f.createEvent(t, "22222222-2222-2222-2222-222222222222", nil)
	createResp.Body.Close()

	resp := f.postJSON(t, "/api/events/22222222-2222-2222-2222-222222222222/attendees", map[string]any{"userId": f.memberID, "email": "guest@example.com"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 when both userId and email are set, got %d", resp.StatusCode)
	}
}

// TestEventHandler_AddAttendee_OrganizersOwnEmail_Rejects400 covers the AC
// bullet at the wire level.
func TestEventHandler_AddAttendee_OrganizersOwnEmail_Rejects400(t *testing.T) {
	f := newEmailAttendeeHandlerFixture(t)
	createResp := f.createEvent(t, "22222222-2222-2222-2222-222222222222", nil)
	createResp.Body.Close()

	resp := f.postJSON(t, "/api/events/22222222-2222-2222-2222-222222222222/attendees", map[string]any{"email": "alice@example.com"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 inviting the organizer's own address, got %d", resp.StatusCode)
	}
}

// TestEventHandler_RemoveAttendeeByEmail_RevokesInvite covers
// DELETE .../attendees/email/{email}.
func TestEventHandler_RemoveAttendeeByEmail_RevokesInvite(t *testing.T) {
	f := newEmailAttendeeHandlerFixture(t)
	createResp := f.createEvent(t, "22222222-2222-2222-2222-222222222222", []string{"guest@example.com"})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected create to succeed with 201, got %d", createResp.StatusCode)
	}
	createResp.Body.Close()

	req, err := http.NewRequest(http.MethodDelete, f.baseURL+"/api/events/22222222-2222-2222-2222-222222222222/attendees/email/guest%40example.com", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+f.accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	attendeesResp, err := authenticatedGet(f.baseURL+"/api/events/22222222-2222-2222-2222-222222222222/attendees", f.accessToken)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	defer attendeesResp.Body.Close()
	var attendees []attendeeWire
	if err := json.NewDecoder(attendeesResp.Body).Decode(&attendees); err != nil {
		t.Fatalf("decode attendees: %v", err)
	}
	if len(attendees) != 0 {
		t.Fatalf("expected the attendee removed, got %+v", attendees)
	}
}
