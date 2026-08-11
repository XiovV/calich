// event_attendee_create_test.go covers #187/ADR-0055 at the HTTP layer:
// POST /api/events/ accepting attendeeUserIds/attendeeGroupIds and
// rendering EventService.Create's Attendee-specific failures the same way
// AddAttendee/AddGroupAttendee already do.
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

// createAttendeeHandlerFixture is newEventTestServerWithServices plus a
// second Workspace Member, an outsider belonging to no Workspace, and a
// Group containing the member — the targets #187's create-time invite
// tests exercise, none of which the shared fixture carries.
type createAttendeeHandlerFixture struct {
	baseURL     string
	accessToken string
	calendarID  string
	memberID    int64
	outsiderID  int64
	groupID     int64
	events      *service.EventService
}

func newCreateAttendeeHandlerFixture(t *testing.T) createAttendeeHandlerFixture {
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
	auth := service.NewAuthService(users, sessions, workspaces, repository.NewWorkspaceInviteRepository(sqlDB), calendars, []byte("test-secret"), "alice", "alice@example.com", "hunter2", false)
	bootstrapUser, _, err := auth.Bootstrap(context.Background())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	aliceWorkspaces, err := workspaces.ListForUser(context.Background(), bootstrapUser.ID)
	if err != nil {
		t.Fatalf("list workspaces: %v", err)
	}
	if len(aliceWorkspaces) != 1 {
		t.Fatalf("expected alice to belong to exactly one workspace, got %d", len(aliceWorkspaces))
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

	outsider, err := users.Create(context.Background(), "outsider", "outsider@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create outsider: %v", err)
	}

	group, err := groupRepo.Create(context.Background(), workspaceID, "Tech team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := groupRepo.AddMember(context.Background(), group.ID, member.ID); err != nil {
		t.Fatalf("add group member: %v", err)
	}

	cal, err := calendars.Create(context.Background(), userID, workspaceID, "11111111-1111-1111-1111-111111111111", service.CalendarWrite{Name: "Personal", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	events := service.NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendars, users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo, groupRepo)
	eventHandler := NewEventHandler(events, attachmentstore.New(t.TempDir()))

	r := chi.NewRouter()
	r.Route("/api/events", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Post("/", eventHandler.Create)
		r.Get("/{id}/attendees", eventHandler.ListAttendees)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return createAttendeeHandlerFixture{
		baseURL: srv.URL, accessToken: loginResult.AccessToken, calendarID: cal.ID,
		memberID: member.ID, outsiderID: outsider.ID, groupID: group.ID, events: events,
	}
}

func postCreateEventWithAttendees(t *testing.T, f createAttendeeHandlerFixture, id string, attendeeUserIds, attendeeGroupIds []int64) *http.Response {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"id":               id,
		"calendarId":       f.calendarID,
		"title":            "Standup",
		"start":            "2026-01-01T09:00:00Z",
		"end":              "2026-01-01T10:00:00Z",
		"attendeeUserIds":  attendeeUserIds,
		"attendeeGroupIds": attendeeGroupIds,
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, f.baseURL+"/api/events/", bytes.NewReader(body))
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

func TestEventHandler_Create_InvitesExplicitUserAndGroup(t *testing.T) {
	f := newCreateAttendeeHandlerFixture(t)

	resp := postCreateEventWithAttendees(t, f, "22222222-2222-2222-2222-222222222222", []int64{f.memberID}, nil)
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
	if len(attendees) != 1 || attendees[0].UserID != f.memberID {
		t.Fatalf("expected [%d], got %+v", f.memberID, attendees)
	}
}

func TestEventHandler_Create_RefusesUnknownAttendeeUserWith400AndCreatesNoEvent(t *testing.T) {
	f := newCreateAttendeeHandlerFixture(t)

	id := "22222222-2222-2222-2222-222222222222"
	resp := postCreateEventWithAttendees(t, f, id, []int64{999999}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	if _, err := f.events.Get(context.Background(), 1, id); err == nil {
		t.Fatalf("expected no event to have been created")
	}
}

func TestEventHandler_Create_RefusesAttendeeOutsideWorkspaceWith400(t *testing.T) {
	f := newCreateAttendeeHandlerFixture(t)

	resp := postCreateEventWithAttendees(t, f, "22222222-2222-2222-2222-222222222222", []int64{f.outsiderID}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestEventHandler_Create_RefusesUnknownAttendeeGroupWith400(t *testing.T) {
	f := newCreateAttendeeHandlerFixture(t)

	resp := postCreateEventWithAttendees(t, f, "22222222-2222-2222-2222-222222222222", nil, []int64{999999})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
