package service

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calich/server/internal/repository"
)

// TestConnectionService_ListCalendars_AnnotatesWorkspaceState covers #295's
// "shows current-Workspace state, noting rows already imported into a
// different Workspace": a calendar imported into the Workspace the picker
// was opened in comes back ImportedHere with its local id, one imported
// into another Workspace of the same User comes back ImportedElsewhere, and
// one imported nowhere carries neither.
func TestConnectionService_ListCalendars_AnnotatesWorkspaceState(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{
		googleCalendarItem("here", "Imported here", "#0b8043", "owner", true),
		googleCalendarItem("elsewhere", "Imported elsewhere", "#7627bb", "owner", false),
		googleCalendarItem("untouched", "Not imported", "#3f51b5", "owner", false),
	}
	svc, auth, userID, workspaceA, g := newTestConnectionServiceWithGraph(t, google)
	connectionID := connectUser(t, svc, auth, userID)

	workspaceB, err := g.WorkspaceRepo.Create(context.Background(), "Second Workspace", userID)
	if err != nil {
		t.Fatalf("create second workspace: %v", err)
	}
	if err := g.WorkspaceRepo.AddMember(context.Background(), workspaceB.ID, userID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add member: %v", err)
	}

	if _, err := svc.ImportCalendars(context.Background(), userID, workspaceA, connectionID, []string{"here"}); err != nil {
		t.Fatalf("import into workspace A: %v", err)
	}
	if _, err := svc.ImportCalendars(context.Background(), userID, workspaceB.ID, connectionID, []string{"elsewhere"}); err != nil {
		t.Fatalf("import into workspace B: %v", err)
	}

	calendars, err := svc.ListCalendars(context.Background(), userID, workspaceA, connectionID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}
	byID := make(map[string]PickerCalendar, len(calendars))
	for _, c := range calendars {
		byID[c.ExternalID] = c
	}

	if here := byID["here"]; !here.ImportedHere || here.ImportedElsewhere || here.LocalCalendarID == "" {
		t.Fatalf("expected 'here' imported into this workspace with a local id, got %+v", here)
	}
	if elsewhere := byID["elsewhere"]; elsewhere.ImportedHere || !elsewhere.ImportedElsewhere {
		t.Fatalf("expected 'elsewhere' flagged as imported into another workspace, got %+v", elsewhere)
	}
	if untouched := byID["untouched"]; untouched.ImportedHere || untouched.ImportedElsewhere {
		t.Fatalf("expected 'untouched' flagged as imported nowhere, got %+v", untouched)
	}
}

// TestConnectionService_ListCalendars_ReportsShareCountForImportedRow covers
// the picker row that carries the Share count the delete confirmation needs
// to warn with (#295).
func TestConnectionService_ListCalendars_ReportsShareCountForImportedRow(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "Primary", "#0b8043", "owner", true)}
	svc, auth, userID, workspaceID, g := newTestConnectionServiceWithGraph(t, google)

	other, err := g.UserRepo.Create(context.Background(), "user-b", "user-b@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}
	if err := g.WorkspaceRepo.AddMember(context.Background(), workspaceID, other.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add other member: %v", err)
	}

	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	if _, _, err := g.Calendars.Share(context.Background(), userID, calendar.ID, "user-b@example.com", "viewer"); err != nil {
		t.Fatalf("share calendar: %v", err)
	}

	connectionID, err := connectionIDFor(g, userID)
	if err != nil {
		t.Fatalf("resolve connection id: %v", err)
	}
	calendars, err := svc.ListCalendars(context.Background(), userID, workspaceID, connectionID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}
	if len(calendars) != 1 || calendars[0].ShareCount != 1 {
		t.Fatalf("expected the imported row to report ShareCount 1, got %+v", calendars)
	}
}

// TestConnectionService_ImportCalendars_SkipsCalendarsAlreadyImportedHere
// covers #295's re-run: the picker sends the whole checked set, so a
// calendar already imported into this Workspace must not spawn a second
// Linked Calendar row.
func TestConnectionService_ImportCalendars_SkipsCalendarsAlreadyImportedHere(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{
		googleCalendarItem("primary", "Primary", "#0b8043", "owner", true),
		googleCalendarItem("secondary", "Secondary", "#7627bb", "owner", false),
	}
	svc, auth, userID, workspaceID, g := newTestConnectionServiceWithGraph(t, google)
	connectionID := connectUser(t, svc, auth, userID)

	if _, err := svc.ImportCalendars(context.Background(), userID, workspaceID, connectionID, []string{"primary"}); err != nil {
		t.Fatalf("first import: %v", err)
	}

	created, err := svc.ImportCalendars(context.Background(), userID, workspaceID, connectionID, []string{"primary", "secondary"})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if len(created) != 1 || created[0].Source == nil || *created[0].Source.ExternalCalendarID != "secondary" {
		t.Fatalf("expected only the newly-checked 'secondary' to be created, got %+v", created)
	}

	all, err := g.CalendarRepo.ListByUserAndWorkspace(context.Background(), userID, workspaceID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected exactly 2 Linked Calendars after re-running the picker, got %d", len(all))
	}
}

// TestConnectionService_Disconnect_KeepLeavesOrdinaryCalendars covers #295's
// default disposition: the Linked Calendars survive as ordinary owned
// Calendars, their Source dropped and the Provider ids cleared from their
// Events.
func TestConnectionService_Disconnect_KeepLeavesOrdinaryCalendars(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "Primary", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {googleEventItem("evt-1", "Dentist", "2026-01-15T10:00:00-05:00", "2026-01-15T11:00:00-05:00", "America/New_York")},
	}
	svc, auth, userID, workspaceID, g := newTestConnectionServiceWithGraph(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	connectionID, err := connectionIDFor(g, userID)
	if err != nil {
		t.Fatalf("resolve connection id: %v", err)
	}
	if err := svc.Disconnect(context.Background(), userID, connectionID, DisconnectKeep); err != nil {
		t.Fatalf("disconnect keep: %v", err)
	}

	got, err := g.Calendars.Get(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("expected the calendar to survive a keep disconnect: %v", err)
	}
	if got.Source != nil {
		t.Fatalf("expected the Source to be dropped, got %+v", got.Source)
	}

	masters, _, err := g.Events.ListSeriesByCalendar(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(masters) != 1 {
		t.Fatalf("expected the Event to survive, got %d", len(masters))
	}
	stored, err := g.EventRepo.GetByID(context.Background(), masters[0].ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if stored.ExternalUID != nil {
		t.Fatalf("expected the Provider id to be cleared, got %v", *stored.ExternalUID)
	}

	list, err := svc.List(context.Background(), userID)
	if err != nil {
		t.Fatalf("list connections: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected the Connection to be gone, got %d", len(list))
	}
}

// TestConnectionService_Disconnect_DeleteRemovesCalendarsAndEvents covers
// #295's explicit "delete" disposition.
func TestConnectionService_Disconnect_DeleteRemovesCalendarsAndEvents(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "Primary", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {googleEventItem("evt-1", "Dentist", "2026-01-15T10:00:00-05:00", "2026-01-15T11:00:00-05:00", "America/New_York")},
	}
	svc, auth, userID, workspaceID, g := newTestConnectionServiceWithGraph(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	connectionID, err := connectionIDFor(g, userID)
	if err != nil {
		t.Fatalf("resolve connection id: %v", err)
	}
	if err := svc.Disconnect(context.Background(), userID, connectionID, DisconnectDelete); err != nil {
		t.Fatalf("disconnect delete: %v", err)
	}

	if _, err := g.Calendars.Get(context.Background(), userID, calendar.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the calendar to be deleted, got %v", err)
	}
	if _, err := g.EventRepo.GetByID(context.Background(), "evt-1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the calendar's Events to be gone too, got %v", err)
	}
}

func TestConnectionService_Disconnect_RejectsUnknownDisposition(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, auth, userID, _ := newTestConnectionServiceWithWorkspace(t, google)
	connectionID := connectUser(t, svc, auth, userID)

	if err := svc.Disconnect(context.Background(), userID, connectionID, "shred"); !errors.Is(err, ErrInvalidDisconnectDisposition) {
		t.Fatalf("expected ErrInvalidDisconnectDisposition, got %v", err)
	}
	list, err := svc.List(context.Background(), userID)
	if err != nil {
		t.Fatalf("list connections: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected the Connection to be untouched after a refused disposition, got %d", len(list))
	}
}

// TestConnectionService_DisconnectImpact_NamesCalendarsAndShares covers
// #295's "the disconnect confirmation names affected Shares when there are
// any".
func TestConnectionService_DisconnectImpact_NamesCalendarsAndShares(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{
		googleCalendarItem("primary", "Primary", "#0b8043", "owner", true),
		googleCalendarItem("team", "Team", "#7627bb", "owner", false),
	}
	svc, auth, userID, workspaceID, g := newTestConnectionServiceWithGraph(t, google)

	other, err := g.UserRepo.Create(context.Background(), "user-b", "user-b@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}
	if err := g.WorkspaceRepo.AddMember(context.Background(), workspaceID, other.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add other member: %v", err)
	}

	connectionID := connectUser(t, svc, auth, userID)
	created, err := svc.ImportCalendars(context.Background(), userID, workspaceID, connectionID, []string{"primary", "team"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	var team repository.Calendar
	for _, c := range created {
		if *c.Source.ExternalCalendarID == "team" {
			team = c
		}
	}
	if _, _, err := g.Calendars.Share(context.Background(), userID, team.ID, "user-b@example.com", "viewer"); err != nil {
		t.Fatalf("share: %v", err)
	}

	impact, err := svc.DisconnectImpact(context.Background(), userID, connectionID)
	if err != nil {
		t.Fatalf("disconnect impact: %v", err)
	}
	if len(impact.LinkedCalendars) != 2 {
		t.Fatalf("expected 2 linked calendars in the impact, got %d", len(impact.LinkedCalendars))
	}
	shared := 0
	for _, c := range impact.LinkedCalendars {
		if c.ShareCount > 0 {
			shared++
			if c.Name != "Team" {
				t.Fatalf("expected the shared calendar to be 'Team', got %q", c.Name)
			}
		}
	}
	if shared != 1 {
		t.Fatalf("expected exactly one shared calendar in the impact, got %d", shared)
	}
}

// connectionIDFor resolves the (single) Connection a test User holds — the
// picker/refresh helpers return the Calendar, not the Connection id, so
// disposition tests recover it from the repository.
func connectionIDFor(g *Graph, userID int64) (int64, error) {
	connections, err := g.ConnectionRepo.ListByUser(context.Background(), userID)
	if err != nil {
		return 0, err
	}
	if len(connections) != 1 {
		return 0, errors.New("expected exactly one connection")
	}
	return connections[0].ID, nil
}
