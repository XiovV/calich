package service

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calich/server/internal/repository"
)

// newTestConnectionServiceWithWorkspace is newTestConnectionService's own
// sibling for the Calendar picker (#286): ImportCalendars needs an active
// Workspace to place Linked Calendars into, which none of #285's existing
// Connect/Callback/Disconnect tests require.
func newTestConnectionServiceWithWorkspace(t *testing.T, google *fakeGoogleServer) (svc *ConnectionService, auth *AuthService, userID, workspaceID int64) {
	t.Helper()

	g := newTestGraph(t)

	user, err := g.UserRepo.Create(context.Background(), "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	workspace, err := g.WorkspaceRepo.Create(context.Background(), "Test Workspace", user.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := g.WorkspaceRepo.AddMember(context.Background(), workspace.ID, user.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}

	connections := repository.NewConnectionRepository(g.DB)
	svc = NewConnectionService(connections, g.Auth, g.Calendars, g.Events, "test-client-id", "test-client-secret", "test-encryption-key", true,
		withGoogleHTTPClient(google.Client()),
		withGoogleEndpoints(google.URL+"/authorize", google.URL+"/token", google.URL+"/userinfo", google.URL+"/calendarList"),
		withGoogleEventsURL(google.URL),
	)

	return svc, g.Auth, user.ID, workspace.ID
}

// connectUser drives svc's own Connect/Callback round trip for userID and
// returns the resulting Connection's id — every picker test's shared prefix,
// since ListCalendars/ImportCalendars both need an existing Connection to
// act on.
func connectUser(t *testing.T, svc *ConnectionService, auth *AuthService, userID int64) int64 {
	t.Helper()

	state, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}
	connection, err := svc.Callback(context.Background(), "auth-code", state, "https://calendar.example.com/callback")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	return connection.ID
}

func googleCalendarItem(id, summary, backgroundColor, accessRole string, selected bool) map[string]any {
	return map[string]any{
		"id":              id,
		"summary":         summary,
		"backgroundColor": backgroundColor,
		"accessRole":      accessRole,
		"selected":        selected,
	}
}

func TestConnectionService_ListCalendars_ReturnsEveryCalendarTheAccountCanSee(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{
		googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true),
		googleCalendarItem("shared-in@group.calendar.google.com", "Team calendar", "#7627bb", "reader", false),
	}
	svc, auth, userID, _ := newTestConnectionServiceWithWorkspace(t, google)
	connectionID := connectUser(t, svc, auth, userID)

	calendars, err := svc.ListCalendars(context.Background(), userID, connectionID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}
	if len(calendars) != 2 {
		t.Fatalf("expected 2 calendars, got %d", len(calendars))
	}

	primary := calendars[0]
	if primary.ExternalID != "primary" || primary.Name != "someone@gmail.com" || primary.Color != "#0B8043FF" {
		t.Fatalf("unexpected primary calendar: %+v", primary)
	}
	if !primary.Selected {
		t.Fatalf("expected primary calendar to be pre-checked from google's own selected flag")
	}
	if !primary.Writable {
		t.Fatalf("expected an owner-role calendar to be writable")
	}

	shared := calendars[1]
	if shared.Selected {
		t.Fatalf("expected the shared-in calendar to default unchecked")
	}
	if shared.Writable {
		t.Fatalf("expected a reader-role calendar to be marked read-only")
	}
}

func TestConnectionService_ListCalendars_NotFoundForSomeoneElsesConnection(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, auth, userID, _ := newTestConnectionServiceWithWorkspace(t, google)
	connectionID := connectUser(t, svc, auth, userID)

	if _, err := svc.ListCalendars(context.Background(), userID+1, connectionID); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("expected ErrConnectionNotFound, got %v", err)
	}
}

func TestConnectionService_ListCalendars_SurfacesGoogleFailure(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, auth, userID, _ := newTestConnectionServiceWithWorkspace(t, google)
	connectionID := connectUser(t, svc, auth, userID)

	google.calendarListStatus = 401
	if _, err := svc.ListCalendars(context.Background(), userID, connectionID); !errors.Is(err, ErrGoogleCalendarListFailed) {
		t.Fatalf("expected ErrGoogleCalendarListFailed, got %v", err)
	}
}

func TestConnectionService_ImportCalendars_CreatesLinkedCalendars(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{
		googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true),
		googleCalendarItem("shared-in@group.calendar.google.com", "Team calendar", "#7627bb", "reader", false),
	}
	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	connectionID := connectUser(t, svc, auth, userID)

	calendars, err := svc.ImportCalendars(context.Background(), userID, workspaceID, connectionID, []string{"primary"})
	if err != nil {
		t.Fatalf("import calendars: %v", err)
	}
	if len(calendars) != 1 {
		t.Fatalf("expected exactly one imported calendar, got %d", len(calendars))
	}

	calendar := calendars[0]
	if calendar.Name != "someone@gmail.com" {
		t.Fatalf("expected name %q, got %q", "someone@gmail.com", calendar.Name)
	}
	if calendar.Source == nil {
		t.Fatalf("expected the imported calendar to carry a Source")
	}
	if calendar.Source.Kind != repository.SourceKindConnection {
		t.Fatalf("expected source kind %q, got %q", repository.SourceKindConnection, calendar.Source.Kind)
	}
	// Every Source ImportCalendars creates is read-only regardless of
	// Google's own accessRole (ADR-0052, ADR-0075): write-back doesn't exist
	// yet, so an "owner"-role calendar must not become writable here.
	if calendar.Source.Mode != repository.SourceModeReadOnly {
		t.Fatalf("expected mode %q, got %q", repository.SourceModeReadOnly, calendar.Source.Mode)
	}
	if calendar.Source.ConnectionID == nil || *calendar.Source.ConnectionID != connectionID {
		t.Fatalf("expected connection id %d, got %v", connectionID, calendar.Source.ConnectionID)
	}
	if calendar.Source.ExternalCalendarID == nil || *calendar.Source.ExternalCalendarID != "primary" {
		t.Fatalf("expected external calendar id %q, got %v", "primary", calendar.Source.ExternalCalendarID)
	}
}

func TestConnectionService_ImportCalendars_IgnoresIDsGoogleNoLongerLists(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{
		googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true),
	}
	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	connectionID := connectUser(t, svc, auth, userID)

	calendars, err := svc.ImportCalendars(context.Background(), userID, workspaceID, connectionID, []string{"primary", "no-longer-shared"})
	if err != nil {
		t.Fatalf("import calendars: %v", err)
	}
	if len(calendars) != 1 {
		t.Fatalf("expected the unmatched id to be silently skipped, got %d calendars", len(calendars))
	}
}

func TestConnectionService_ImportCalendars_NotFoundForSomeoneElsesConnection(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	connectionID := connectUser(t, svc, auth, userID)

	if _, err := svc.ImportCalendars(context.Background(), userID+1, workspaceID, connectionID, []string{"primary"}); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("expected ErrConnectionNotFound, got %v", err)
	}
}
