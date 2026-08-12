package caldavserver

import (
	"context"
	"testing"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

// addWorkspaceGuest mints a second User and adds them to env's Workspace —
// the minimum ADR-0046 requires to make them a valid Attendee target,
// deliberately not granting any Calendar Access (an Attendee invite needs
// none).
func (env testCalDAVEnv) addWorkspaceGuest(t *testing.T, username string) (userID int64) {
	t.Helper()

	other, err := env.users.Create(context.Background(), username, username+"@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user %q: %v", username, err)
	}
	if err := env.workspaces.AddMember(context.Background(), env.workspaceID, other.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add %q as workspace member: %v", username, err)
	}
	return other.ID
}

func TestGetCalendarObject_EmitsOrganizerAndAttendeeWithPartstat(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	guestID := env.addWorkspaceGuest(t, "guest")

	created, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{
		CalendarID:      env.calendarID,
		Title:           "Planning",
		Start:           time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:             time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		AttendeeUserIDs: []int64{guestID},
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	client := newTestCalDAVClient(t, env)
	obj, err := client.GetCalendarObject(ctx, calendarObjectPath(env.userID, env.calendarID, created.ID))
	if err != nil {
		t.Fatalf("GetCalendarObject: %v", err)
	}

	events := obj.Data.Events()
	if len(events) != 1 {
		t.Fatalf("expected exactly one VEVENT, got %d", len(events))
	}
	v := events[0]

	organizer := v.Props.Get(ical.PropOrganizer)
	if organizer == nil {
		t.Fatalf("expected an ORGANIZER property")
	}
	if organizer.Value != "mailto:admin@example.com" {
		t.Fatalf("expected ORGANIZER naming the creator's own email, got %q", organizer.Value)
	}
	if cn := organizer.Params.Get(ical.ParamCommonName); cn != "admin" {
		t.Fatalf("expected ORGANIZER CN=admin, got %q", cn)
	}

	attendees := v.Props.Values(ical.PropAttendee)
	if len(attendees) != 1 {
		t.Fatalf("expected exactly one ATTENDEE, got %d", len(attendees))
	}
	if attendees[0].Value != "mailto:guest@example.com" {
		t.Fatalf("expected ATTENDEE naming the guest's email, got %q", attendees[0].Value)
	}
	if partstat := attendees[0].Params.Get(ical.ParamParticipationStatus); partstat != "NEEDS-ACTION" {
		t.Fatalf("expected PARTSTAT=NEEDS-ACTION for an unanswered invite, got %q", partstat)
	}
}

func TestGetCalendarObject_NoAttendees_EmitsNoAttendeeProperty(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	created, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{
		CalendarID: env.calendarID,
		Title:      "Solo focus block",
		Start:      time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:        time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	client := newTestCalDAVClient(t, env)
	obj, err := client.GetCalendarObject(ctx, calendarObjectPath(env.userID, env.calendarID, created.ID))
	if err != nil {
		t.Fatalf("GetCalendarObject: %v", err)
	}

	v := obj.Data.Events()[0]
	if len(v.Props.Values(ical.PropAttendee)) != 0 {
		t.Fatalf("expected no ATTENDEE property on an Event with no Attendees, got %v", v.Props.Values(ical.PropAttendee))
	}
}

// TestPutCalendarObject_InboundAttendeeChanges_Discarded is ADR-0062's core
// guarantee: a native client's edits to the guest list are refused, not
// honoured, because letting a synced client's idea of a diff drive who gets
// invited would (once Invitations exist, ADR-0059) let it cause this
// instance to email addresses nobody typed here.
func TestPutCalendarObject_InboundAttendeeChanges_Discarded(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	guestID := env.addWorkspaceGuest(t, "guest")

	created, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{
		CalendarID:      env.calendarID,
		Title:           "Planning",
		Start:           time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:             time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		AttendeeUserIDs: []int64{guestID},
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	client := newTestCalDAVClient(t, env)
	path := calendarObjectPath(env.userID, env.calendarID, created.ID)

	obj, err := client.GetCalendarObject(ctx, path)
	if err != nil {
		t.Fatalf("GetCalendarObject: %v", err)
	}

	// Simulate a native client's edit: drop the server's own ATTENDEE line
	// and add one of its own choosing.
	v := obj.Data.Events()[0]
	v.Props.Del(ical.PropAttendee)
	rogue := ical.NewProp(ical.PropAttendee)
	rogue.Params.Set(ical.ParamCommonName, "Stranger")
	rogue.Params.Set(ical.ParamParticipationStatus, "ACCEPTED")
	rogue.Value = "mailto:stranger@example.com"
	v.Props.Add(rogue)

	if _, err := client.PutCalendarObject(ctx, path, obj.Data); err != nil {
		t.Fatalf("PutCalendarObject: %v", err)
	}

	attendees, err := env.eventService.ListAttendees(ctx, env.userID, created.ID)
	if err != nil {
		t.Fatalf("ListAttendees: %v", err)
	}
	if len(attendees) != 1 {
		t.Fatalf("expected the stored guest list to be unchanged (one Attendee), got %d", len(attendees))
	}
	if attendees[0].UserID == nil || *attendees[0].UserID != guestID {
		t.Fatalf("expected the original guest to still be the sole Attendee, got %+v", attendees[0])
	}

	// The next GET must reflect the server's own state, not the PUT's.
	again, err := client.GetCalendarObject(ctx, path)
	if err != nil {
		t.Fatalf("second GetCalendarObject: %v", err)
	}
	gotAttendees := again.Data.Events()[0].Props.Values(ical.PropAttendee)
	if len(gotAttendees) != 1 {
		t.Fatalf("expected exactly one ATTENDEE on the next GET, got %d", len(gotAttendees))
	}
	if gotAttendees[0].Value != "mailto:guest@example.com" {
		t.Fatalf("expected the next GET to re-emit the original guest, got %q", gotAttendees[0].Value)
	}
}
