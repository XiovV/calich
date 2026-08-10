package repository

import (
	"context"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
)

// newTestFiredReminderRepository returns a FiredReminderRepository plus a
// real Reminder id (event_reminders.id) and User id to satisfy the ledger's
// foreign keys.
func newTestFiredReminderRepository(t *testing.T) (ledger *FiredReminderRepository, reminderID, userID int64) {
	t.Helper()
	ledger, reminderID, userID, _ = newTestFiredReminderRepositoryWithUsers(t)
	return ledger, reminderID, userID
}

// newTestFiredReminderRepositoryWithUsers is newTestFiredReminderRepository
// plus the UserRepository itself, for tests that need to create a second
// User to prove the ledger's per-User independence (ADR-0036).
func newTestFiredReminderRepositoryWithUsers(t *testing.T) (ledger *FiredReminderRepository, reminderID, userID int64, users *UserRepository) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	ctx := context.Background()

	users = NewUserRepository(sqlDB)
	user, err := users.Create(ctx, "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	workspaces := NewWorkspaceRepository(sqlDB)
	workspace, err := workspaces.Create(ctx, "workspace-a", user.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaces.AddMember(ctx, workspace.ID, user.ID, WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}
	calendars := NewCalendarRepository(sqlDB)
	cal, err := calendars.Create(ctx, user.ID, workspace.ID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	events := NewEventRepository(sqlDB)
	mustCreateEvent(t, events, "evt-1", user.ID, cal.ID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	remindersRepo := NewEventReminderRepository(sqlDB)
	if err := remindersRepo.ReplaceByEventID(ctx, "evt-1", []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}
	byEvent, err := remindersRepo.ListByEventIDs(ctx, []string{"evt-1"})
	if err != nil {
		t.Fatalf("list by event ids: %v", err)
	}
	if len(byEvent["evt-1"]) != 1 {
		t.Fatalf("expected 1 reminder, got %+v", byEvent["evt-1"])
	}

	return NewFiredReminderRepository(sqlDB), byEvent["evt-1"][0].ID, user.ID, users
}

func TestFiredReminderRepository_MarkFired_FirstCallReportsNew(t *testing.T) {
	ledger, reminderID, userID := newTestFiredReminderRepository(t)
	occurrenceStart := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

	isNew, err := ledger.MarkFired(context.Background(), reminderID, userID, occurrenceStart, time.Now().UTC())
	if err != nil {
		t.Fatalf("mark fired: %v", err)
	}
	if !isNew {
		t.Fatal("expected the first MarkFired for a Reminder+Occurrence+User to report new")
	}
}

// A repeated tick calling MarkFired again for the same (reminder,
// occurrenceStart, user) must report false — the exactly-once guarantee
// (ADR-0021).
func TestFiredReminderRepository_MarkFired_RepeatedCallReportsNotNew(t *testing.T) {
	ledger, reminderID, userID := newTestFiredReminderRepository(t)
	occurrenceStart := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	ctx := context.Background()

	if _, err := ledger.MarkFired(ctx, reminderID, userID, occurrenceStart, time.Now().UTC()); err != nil {
		t.Fatalf("mark fired: %v", err)
	}

	isNew, err := ledger.MarkFired(ctx, reminderID, userID, occurrenceStart, time.Now().UTC())
	if err != nil {
		t.Fatalf("mark fired again: %v", err)
	}
	if isNew {
		t.Fatal("expected a repeated MarkFired for the same reminder+occurrence+user to report not-new")
	}
}

// A different Occurrence of the same recurring Reminder fires independently.
func TestFiredReminderRepository_MarkFired_DifferentOccurrenceIsIndependent(t *testing.T) {
	ledger, reminderID, userID := newTestFiredReminderRepository(t)
	ctx := context.Background()

	firstOccurrence := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	secondOccurrence := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)

	if _, err := ledger.MarkFired(ctx, reminderID, userID, firstOccurrence, time.Now().UTC()); err != nil {
		t.Fatalf("mark fired: %v", err)
	}

	isNew, err := ledger.MarkFired(ctx, reminderID, userID, secondOccurrence, time.Now().UTC())
	if err != nil {
		t.Fatalf("mark fired for a different occurrence: %v", err)
	}
	if !isNew {
		t.Fatal("expected a different Occurrence of the same Reminder to fire independently")
	}
}

// A shared Calendar's Reminder must fire independently for each recipient —
// the ledger's per-User dimension (ADR-0036) — so one User's fire does not
// suppress another's for the same Reminder and Occurrence.
func TestFiredReminderRepository_MarkFired_DifferentUserIsIndependent(t *testing.T) {
	ledger, reminderID, ownerID, users := newTestFiredReminderRepositoryWithUsers(t)
	ctx := context.Background()
	occurrenceStart := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

	if _, err := ledger.MarkFired(ctx, reminderID, ownerID, occurrenceStart, time.Now().UTC()); err != nil {
		t.Fatalf("mark fired: %v", err)
	}

	otherUser, err := users.Create(ctx, "user-b", "user-b@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	isNew, err := ledger.MarkFired(ctx, reminderID, otherUser.ID, occurrenceStart, time.Now().UTC())
	if err != nil {
		t.Fatalf("mark fired for a different user: %v", err)
	}
	if !isNew {
		t.Fatal("expected a different User's fire of the same Reminder+Occurrence to report new")
	}
}
