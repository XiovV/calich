// attendee_convert_test.go covers ConvertEmailAttendeesToUser (#203,
// ADR-0058): accepting a Workspace Invite sweeps outstanding email-shaped
// Attendee rows for that address, on Events in that Workspace, onto the
// User whose account just appeared.
package repository

import (
	"context"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
)

// newTestAttendeeConvertRepository wires an AttendeeRepository plus a
// UserRepository, EventRepository and two Workspaces (each with a Calendar
// and an Event) — the fixture ConvertEmailAttendeesToUser's workspace
// scoping tests need.
func newTestAttendeeConvertRepository(t *testing.T) (attendees *AttendeeRepository, users *UserRepository, events *EventRepository, workspaceAID int64, eventInA string, workspaceBID int64, eventInB string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	ctx := context.Background()
	users = NewUserRepository(sqlDB)
	owner, err := users.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	workspaces := NewWorkspaceRepository(sqlDB)
	calendars := NewCalendarRepository(sqlDB)
	events = NewEventRepository(sqlDB)

	workspaceA, err := workspaces.Create(ctx, "workspace-a", owner.ID)
	if err != nil {
		t.Fatalf("create workspace a: %v", err)
	}
	if err := workspaces.AddMember(ctx, workspaceA.ID, owner.ID, WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace a owner: %v", err)
	}
	calA, err := calendars.Create(ctx, owner.ID, workspaceA.ID, "cal-a", CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar a: %v", err)
	}
	mustCreateEvent(t, events, "evt-a", owner.ID, calA.ID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	workspaceB, err := workspaces.Create(ctx, "workspace-b", owner.ID)
	if err != nil {
		t.Fatalf("create workspace b: %v", err)
	}
	if err := workspaces.AddMember(ctx, workspaceB.ID, owner.ID, WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace b owner: %v", err)
	}
	calB, err := calendars.Create(ctx, owner.ID, workspaceB.ID, "cal-b", CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar b: %v", err)
	}
	mustCreateEvent(t, events, "evt-b", owner.ID, calB.ID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	return NewAttendeeRepository(sqlDB), users, events, workspaceA.ID, "evt-a", workspaceB.ID, "evt-b"
}

// TestAttendeeRepository_ConvertEmailAttendeesToUser_ConvertsAndCarriesResponse
// covers the base case: an email-shaped row with no User-backed counterpart
// converts in place, keeping the Response it already carried.
func TestAttendeeRepository_ConvertEmailAttendeesToUser_ConvertsAndCarriesResponse(t *testing.T) {
	attendees, users, _, workspaceAID, eventInA, _, _ := newTestAttendeeConvertRepository(t)
	ctx := context.Background()

	if _, err := attendees.AddEmail(ctx, eventInA, "bob@example.com"); err != nil {
		t.Fatalf("add email attendee: %v", err)
	}
	if _, err := attendees.SetResponseByEmail(ctx, eventInA, "bob@example.com", ResponseAccepted); err != nil {
		t.Fatalf("set response by email: %v", err)
	}

	bob, err := users.Create(ctx, "bob", "bob@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	if err := attendees.ConvertEmailAttendeesToUser(ctx, workspaceAID, "bob@example.com", bob.ID); err != nil {
		t.Fatalf("convert email attendees: %v", err)
	}

	converted, err := attendees.Get(ctx, eventInA, bob.ID)
	if err != nil {
		t.Fatalf("get converted attendee: %v", err)
	}
	if converted.Response != ResponseAccepted {
		t.Fatalf("expected the carried-over response %q, got %q", ResponseAccepted, converted.Response)
	}

	if _, err := attendees.getByEmail(ctx, eventInA, "bob@example.com"); err == nil {
		t.Fatalf("expected the email-shaped row to be gone after conversion")
	}
}

// TestAttendeeRepository_ConvertEmailAttendeesToUser_MergeCarriesEmailAnswerOntoNeedsActionUserRow
// covers ADR-0058's merge rule: where both shapes exist and the User-backed
// row is still needs-action while the email row carries a real answer, that
// answer moves onto the surviving User-backed row and the email row is
// removed.
func TestAttendeeRepository_ConvertEmailAttendeesToUser_MergeCarriesEmailAnswerOntoNeedsActionUserRow(t *testing.T) {
	attendees, users, _, workspaceAID, eventInA, _, _ := newTestAttendeeConvertRepository(t)
	ctx := context.Background()

	bob, err := users.Create(ctx, "bob", "bob@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if _, err := attendees.Add(ctx, eventInA, bob.ID); err != nil {
		t.Fatalf("add user-backed attendee: %v", err)
	}

	if _, err := attendees.AddEmail(ctx, eventInA, "bob@example.com"); err != nil {
		t.Fatalf("add email attendee: %v", err)
	}
	if _, err := attendees.SetResponseByEmail(ctx, eventInA, "bob@example.com", ResponseDeclined); err != nil {
		t.Fatalf("set response by email: %v", err)
	}

	if err := attendees.ConvertEmailAttendeesToUser(ctx, workspaceAID, "bob@example.com", bob.ID); err != nil {
		t.Fatalf("convert email attendees: %v", err)
	}

	surviving, err := attendees.Get(ctx, eventInA, bob.ID)
	if err != nil {
		t.Fatalf("get surviving attendee: %v", err)
	}
	if surviving.Response != ResponseDeclined {
		t.Fatalf("expected the email row's answer %q to carry over, got %q", ResponseDeclined, surviving.Response)
	}

	if _, err := attendees.getByEmail(ctx, eventInA, "bob@example.com"); err == nil {
		t.Fatalf("expected the email-shaped row to be deleted")
	}
}

// TestAttendeeRepository_ConvertEmailAttendeesToUser_MergeKeepsUserRowsOwnAnswer
// covers "answering once counts": when the User-backed row already carries a
// real answer, the email row's answer must not overwrite it, even though the
// email row is still deleted.
func TestAttendeeRepository_ConvertEmailAttendeesToUser_MergeKeepsUserRowsOwnAnswer(t *testing.T) {
	attendees, users, _, workspaceAID, eventInA, _, _ := newTestAttendeeConvertRepository(t)
	ctx := context.Background()

	bob, err := users.Create(ctx, "bob", "bob@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if _, err := attendees.Add(ctx, eventInA, bob.ID); err != nil {
		t.Fatalf("add user-backed attendee: %v", err)
	}
	if _, err := attendees.SetResponse(ctx, eventInA, bob.ID, ResponseAccepted); err != nil {
		t.Fatalf("set user-backed response: %v", err)
	}

	if _, err := attendees.AddEmail(ctx, eventInA, "bob@example.com"); err != nil {
		t.Fatalf("add email attendee: %v", err)
	}
	if _, err := attendees.SetResponseByEmail(ctx, eventInA, "bob@example.com", ResponseDeclined); err != nil {
		t.Fatalf("set response by email: %v", err)
	}

	if err := attendees.ConvertEmailAttendeesToUser(ctx, workspaceAID, "bob@example.com", bob.ID); err != nil {
		t.Fatalf("convert email attendees: %v", err)
	}

	surviving, err := attendees.Get(ctx, eventInA, bob.ID)
	if err != nil {
		t.Fatalf("get surviving attendee: %v", err)
	}
	if surviving.Response != ResponseAccepted {
		t.Fatalf("expected the user row's own answer %q to survive, got %q", ResponseAccepted, surviving.Response)
	}

	if _, err := attendees.getByEmail(ctx, eventInA, "bob@example.com"); err == nil {
		t.Fatalf("expected the email-shaped row to be deleted regardless")
	}
}

// TestAttendeeRepository_ConvertEmailAttendeesToUser_OnlyTouchesGivenWorkspace
// covers the "Attendee rows on Events in other Workspaces are untouched"
// acceptance criterion.
func TestAttendeeRepository_ConvertEmailAttendeesToUser_OnlyTouchesGivenWorkspace(t *testing.T) {
	attendees, users, _, workspaceAID, eventInA, _, eventInB := newTestAttendeeConvertRepository(t)
	ctx := context.Background()

	if _, err := attendees.AddEmail(ctx, eventInA, "bob@example.com"); err != nil {
		t.Fatalf("add email attendee in workspace a: %v", err)
	}
	if _, err := attendees.AddEmail(ctx, eventInB, "bob@example.com"); err != nil {
		t.Fatalf("add email attendee in workspace b: %v", err)
	}

	bob, err := users.Create(ctx, "bob", "bob@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	if err := attendees.ConvertEmailAttendeesToUser(ctx, workspaceAID, "bob@example.com", bob.ID); err != nil {
		t.Fatalf("convert email attendees: %v", err)
	}

	if _, err := attendees.Get(ctx, eventInA, bob.ID); err != nil {
		t.Fatalf("expected workspace a's row converted: %v", err)
	}
	if _, err := attendees.getByEmail(ctx, eventInB, "bob@example.com"); err != nil {
		t.Fatalf("expected workspace b's email-shaped row untouched, got: %v", err)
	}
}

// TestAttendeeRepository_ConvertEmailAttendeesToUser_CaseInsensitiveMatch
// covers ADR-0058's "matching is case-insensitive" acceptance criterion —
// an email row stored with stray capitals still converts when the invite's
// already-folded address is passed in.
func TestAttendeeRepository_ConvertEmailAttendeesToUser_CaseInsensitiveMatch(t *testing.T) {
	attendees, users, _, workspaceAID, eventInA, _, _ := newTestAttendeeConvertRepository(t)
	ctx := context.Background()

	if _, err := attendees.AddEmail(ctx, eventInA, "Bob@Example.com"); err != nil {
		t.Fatalf("add email attendee: %v", err)
	}

	bob, err := users.Create(ctx, "bob", "bob@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	if err := attendees.ConvertEmailAttendeesToUser(ctx, workspaceAID, "bob@example.com", bob.ID); err != nil {
		t.Fatalf("convert email attendees: %v", err)
	}

	if _, err := attendees.Get(ctx, eventInA, bob.ID); err != nil {
		t.Fatalf("expected the case-differing email row to convert: %v", err)
	}
}
