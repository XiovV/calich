// attendee_test.go covers the email-shaped Attendee row (#200, ADR-0058)
// directly at the repository layer: AddEmail/RemoveEmail's own read-back and
// uniqueness, and the two list queries' LEFT JOIN shape once user_id can be
// NULL.
package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
)

// newTestAttendeeRepository returns an AttendeeRepository plus a real
// event id (and the User/Attendee repos needed to seed a User-backed row
// alongside an email-shaped one) to satisfy attendees' foreign keys.
func newTestAttendeeRepository(t *testing.T) (repo *AttendeeRepository, users *UserRepository, eventID string) {
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
	workspace, err := workspaces.Create(ctx, "workspace-a", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaces.AddMember(ctx, workspace.ID, owner.ID, WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}

	calendars := NewCalendarRepository(sqlDB)
	cal, err := calendars.Create(ctx, owner.ID, workspace.ID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	events := NewEventRepository(sqlDB)
	mustCreateEvent(t, events, "evt-1", owner.ID, cal.ID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	return NewAttendeeRepository(sqlDB), users, "evt-1"
}

func TestAttendeeRepository_AddEmail_ReadsBackAnEmailShapedAttendee(t *testing.T) {
	repo, _, eventID := newTestAttendeeRepository(t)
	ctx := context.Background()

	added, err := repo.AddEmail(ctx, eventID, "guest@example.com")
	if err != nil {
		t.Fatalf("add email: %v", err)
	}
	if added.UserID != nil {
		t.Fatalf("expected a nil UserID on an email-shaped Attendee, got %v", *added.UserID)
	}
	if added.Email != "guest@example.com" {
		t.Fatalf("expected Email %q, got %q", "guest@example.com", added.Email)
	}
	if added.Name != "" {
		t.Fatalf("expected an empty Name (no account to join against), got %q", added.Name)
	}
	if added.Response != ResponseNeedsAction {
		t.Fatalf("expected default response %q, got %q", ResponseNeedsAction, added.Response)
	}
}

// TestAttendeeRepository_AddEmail_CaseInsensitiveDuplicateFails covers
// ADR-0058's "same address cannot be invited twice" via the column's
// COLLATE NOCASE: a differently-cased repeat still trips the UNIQUE
// constraint.
func TestAttendeeRepository_AddEmail_CaseInsensitiveDuplicateFails(t *testing.T) {
	repo, _, eventID := newTestAttendeeRepository(t)
	ctx := context.Background()

	if _, err := repo.AddEmail(ctx, eventID, "guest@example.com"); err != nil {
		t.Fatalf("add email: %v", err)
	}
	if _, err := repo.AddEmail(ctx, eventID, "Guest@Example.com"); !errors.Is(err, ErrAlreadyAttendee) {
		t.Fatalf("expected ErrAlreadyAttendee for a case-different repeat, got %v", err)
	}
}

func TestAttendeeRepository_RemoveEmail_DeletesTheRow(t *testing.T) {
	repo, _, eventID := newTestAttendeeRepository(t)
	ctx := context.Background()

	if _, err := repo.AddEmail(ctx, eventID, "guest@example.com"); err != nil {
		t.Fatalf("add email: %v", err)
	}
	if err := repo.RemoveEmail(ctx, eventID, "guest@example.com"); err != nil {
		t.Fatalf("remove email: %v", err)
	}

	attendees, err := repo.ListByEventID(ctx, eventID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(attendees) != 0 {
		t.Fatalf("expected no attendees left, got %+v", attendees)
	}
}

func TestAttendeeRepository_RemoveEmail_NotFoundErrors(t *testing.T) {
	repo, _, eventID := newTestAttendeeRepository(t)
	if err := repo.RemoveEmail(context.Background(), eventID, "nobody@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestAttendeeRepository_ListByEventID_MixesUserBackedAndEmailShaped covers
// the LEFT JOIN shape both list queries share: a User-backed row carries its
// joined Name/Email and a non-nil UserID, an email-shaped row carries the
// bare attendees.email with an empty Name and a nil UserID — and both sort
// into one list together.
func TestAttendeeRepository_ListByEventID_MixesUserBackedAndEmailShaped(t *testing.T) {
	repo, users, eventID := newTestAttendeeRepository(t)
	ctx := context.Background()

	member, err := users.Create(ctx, "Alice Member", "alice@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	if _, err := repo.Add(ctx, eventID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if _, err := repo.AddEmail(ctx, eventID, "guest@example.com"); err != nil {
		t.Fatalf("add email: %v", err)
	}

	attendees, err := repo.ListByEventID(ctx, eventID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(attendees) != 2 {
		t.Fatalf("expected 2 attendees, got %+v", attendees)
	}

	var userBacked, emailShaped *AttendeeWithName
	for i := range attendees {
		if attendees[i].UserID != nil {
			userBacked = &attendees[i]
		} else {
			emailShaped = &attendees[i]
		}
	}
	if userBacked == nil || *userBacked.UserID != member.ID || userBacked.Name != "Alice Member" || userBacked.Email != "alice@example.com" {
		t.Fatalf("expected a User-backed row for Alice, got %+v", userBacked)
	}
	if emailShaped == nil || emailShaped.Email != "guest@example.com" || emailShaped.Name != "" {
		t.Fatalf("expected an email-shaped row with no Name, got %+v", emailShaped)
	}
}

// TestAttendeeRepository_ListUserIDsByEventIDs_ExcludesEmailShapedAttendees
// covers the "no Reminders on any Channel" AC bullet at its source: the
// reminder fan-out's batched read path only ever sees user_id, so an
// email-shaped Attendee can never be fanned a Reminder out to.
func TestAttendeeRepository_ListUserIDsByEventIDs_ExcludesEmailShapedAttendees(t *testing.T) {
	repo, users, eventID := newTestAttendeeRepository(t)
	ctx := context.Background()

	member, err := users.Create(ctx, "member", "member@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	if _, err := repo.Add(ctx, eventID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if _, err := repo.AddEmail(ctx, eventID, "guest@example.com"); err != nil {
		t.Fatalf("add email: %v", err)
	}

	byEvent, err := repo.ListUserIDsByEventIDs(ctx, []string{eventID})
	if err != nil {
		t.Fatalf("list user ids: %v", err)
	}
	ids := byEvent[eventID]
	if len(ids) != 1 || ids[0] != member.ID {
		t.Fatalf("expected only the User-backed member's id [%d], got %+v", member.ID, ids)
	}
}

// TestAttendeeRepository_SetResponseByEmail_UpdatesAndReadsBack is
// SetResponse's email-shaped counterpart (#202): the reply poller has no
// session to authenticate a User-backed SetResponse call through, and an
// email-shaped Attendee has no user_id to key on regardless.
func TestAttendeeRepository_SetResponseByEmail_UpdatesAndReadsBack(t *testing.T) {
	repo, _, eventID := newTestAttendeeRepository(t)
	ctx := context.Background()

	if _, err := repo.AddEmail(ctx, eventID, "guest@example.com"); err != nil {
		t.Fatalf("add email: %v", err)
	}

	updated, err := repo.SetResponseByEmail(ctx, eventID, "guest@example.com", ResponseAccepted)
	if err != nil {
		t.Fatalf("set response by email: %v", err)
	}
	if updated.Response != ResponseAccepted {
		t.Fatalf("expected response %q, got %q", ResponseAccepted, updated.Response)
	}

	fetched, err := repo.getByEmail(ctx, eventID, "guest@example.com")
	if err != nil {
		t.Fatalf("get by email: %v", err)
	}
	if fetched.Response != ResponseAccepted {
		t.Fatalf("expected persisted response %q, got %q", ResponseAccepted, fetched.Response)
	}
}

// TestAttendeeRepository_SetResponseByEmail_NotFoundErrors mirrors
// SetResponse's own not-a-member behaviour: there is no row to write onto,
// distinguishable from a successful no-op update (#202).
func TestAttendeeRepository_SetResponseByEmail_NotFoundErrors(t *testing.T) {
	repo, _, eventID := newTestAttendeeRepository(t)
	if _, err := repo.SetResponseByEmail(context.Background(), eventID, "nobody@example.com", ResponseAccepted); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
