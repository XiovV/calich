package repository

import (
	"context"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
)

// newTestEventReminderRepository returns an EventRepository and
// EventReminderRepository sharing one in-memory database, plus a real user id
// and calendar id to satisfy events' foreign keys.
func newTestEventReminderRepository(t *testing.T) (repo *EventRepository, reminders *EventReminderRepository, userID int64, calendarID string) {
	t.Helper()

	repo, reminders, userID, _, calendarID = newTestEventReminderRepositoryWithSecondUser(t)
	return repo, reminders, userID, calendarID
}

// newTestEventReminderRepositoryWithSecondUser is
// newTestEventReminderRepository's sibling for tests that need a second real
// User id — event_reminders.user_id is a foreign key, so a scoping
// assertion needs an id that actually satisfies it, not an arbitrary int64.
func newTestEventReminderRepositoryWithSecondUser(t *testing.T) (repo *EventRepository, reminders *EventReminderRepository, userID, otherUserID int64, calendarID string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := NewUserRepository(sqlDB)
	user, err := users.Create(context.Background(), "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	otherUser, err := users.Create(context.Background(), "user-b", "user-b@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	workspaces := NewWorkspaceRepository(sqlDB)
	workspace, err := workspaces.Create(context.Background(), "workspace-a", user.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaces.AddMember(context.Background(), workspace.ID, user.ID, WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}

	calendars := NewCalendarRepository(sqlDB)
	cal, err := calendars.Create(context.Background(), user.ID, workspace.ID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	return NewEventRepository(sqlDB), NewEventReminderRepository(sqlDB), user.ID, otherUser.ID, cal.ID
}

func TestEventReminderRepository_ReplaceByEventIDAndListByEventIDs(t *testing.T) {
	repo, reminders, userID, calendarID := newTestEventReminderRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	want := []Reminder{
		{OffsetMinutes: 10, Channel: "notification"},
		{OffsetMinutes: 60, Channel: "email"},
	}
	if err := reminders.ReplaceByEventID(ctx, userID, "evt-1", want); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}

	byEvent, err := reminders.ListByEventIDs(ctx, userID, []string{"evt-1"})
	if err != nil {
		t.Fatalf("list by event ids: %v", err)
	}
	if len(byEvent["evt-1"]) != 2 {
		t.Fatalf("expected 2 reminders, got %+v", byEvent["evt-1"])
	}
}

// Each Reminder's own row ID comes back distinct and non-zero — the firing
// engine's fired-ledger keys exactly-once tracking on it (ADR-0021).
func TestEventReminderRepository_ListByEventIDs_PopulatesDistinctIDs(t *testing.T) {
	repo, reminders, userID, calendarID := newTestEventReminderRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	if err := reminders.ReplaceByEventID(ctx, userID, "evt-1", []Reminder{
		{OffsetMinutes: 10, Channel: "notification"},
		{OffsetMinutes: 60, Channel: "email"},
	}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}

	byEvent, err := reminders.ListByEventIDs(ctx, userID, []string{"evt-1"})
	if err != nil {
		t.Fatalf("list by event ids: %v", err)
	}
	got := byEvent["evt-1"]
	if len(got) != 2 {
		t.Fatalf("expected 2 reminders, got %+v", got)
	}
	if got[0].ID == 0 || got[1].ID == 0 {
		t.Fatalf("expected non-zero reminder ids, got %+v", got)
	}
	if got[0].ID == got[1].ID {
		t.Fatalf("expected distinct reminder ids, got %+v", got)
	}
}

func TestEventReminderRepository_ListByEventIDs_EmptyInput(t *testing.T) {
	_, reminders, userID, _ := newTestEventReminderRepository(t)

	byEvent, err := reminders.ListByEventIDs(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("list by event ids: %v", err)
	}
	if len(byEvent) != 0 {
		t.Fatalf("expected no reminders, got %+v", byEvent)
	}
}

// ReplaceByEventID discards the previous set wholesale — an Event update
// replaces its Reminders set, it doesn't merge into it (ADR-0020).
func TestEventReminderRepository_ReplaceByEventID_ReplacesWholesale(t *testing.T) {
	repo, reminders, userID, calendarID := newTestEventReminderRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	if err := reminders.ReplaceByEventID(ctx, userID, "evt-1", []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}
	if err := reminders.ReplaceByEventID(ctx, userID, "evt-1", []Reminder{{OffsetMinutes: 30, Channel: "email"}}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}

	byEvent, err := reminders.ListByEventIDs(ctx, userID, []string{"evt-1"})
	if err != nil {
		t.Fatalf("list by event ids: %v", err)
	}
	if len(byEvent["evt-1"]) != 1 || byEvent["evt-1"][0].OffsetMinutes != 30 {
		t.Fatalf("expected only the second Replace's reminder, got %+v", byEvent["evt-1"])
	}
}

// ReplaceByEventID with an empty/nil slice clears an event's Reminders.
func TestEventReminderRepository_ReplaceByEventID_EmptyClears(t *testing.T) {
	repo, reminders, userID, calendarID := newTestEventReminderRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	if err := reminders.ReplaceByEventID(ctx, userID, "evt-1", []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}
	if err := reminders.ReplaceByEventID(ctx, userID, "evt-1", nil); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}

	byEvent, err := reminders.ListByEventIDs(ctx, userID, []string{"evt-1"})
	if err != nil {
		t.Fatalf("list by event ids: %v", err)
	}
	if len(byEvent["evt-1"]) != 0 {
		t.Fatalf("expected no reminders after clearing, got %+v", byEvent["evt-1"])
	}
}

func TestEventReminderRepository_CascadeDeletesWhenEventDeleted(t *testing.T) {
	repo, reminders, userID, calendarID := newTestEventReminderRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	if err := reminders.ReplaceByEventID(ctx, userID, "evt-1", []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}

	if err := repo.Delete(ctx, "evt-1"); err != nil {
		t.Fatalf("delete event: %v", err)
	}

	byEvent, err := reminders.ListByEventIDs(ctx, userID, []string{"evt-1"})
	if err != nil {
		t.Fatalf("list by event ids: %v", err)
	}
	if len(byEvent["evt-1"]) != 0 {
		t.Fatalf("expected reminders to be cascade-deleted with their event, got %+v", byEvent["evt-1"])
	}
}

// ListByEventIDs scopes to userID: a Reminder written for one User is
// invisible when another User's id is asked for, even on the same Event
// (ADR-0064).
func TestEventReminderRepository_ListByEventIDs_ScopedToUser(t *testing.T) {
	repo, reminders, userID, otherUserID, calendarID := newTestEventReminderRepositoryWithSecondUser(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	if err := reminders.ReplaceByEventID(ctx, userID, "evt-1", []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}

	byEvent, err := reminders.ListByEventIDs(ctx, otherUserID, []string{"evt-1"})
	if err != nil {
		t.Fatalf("list by event ids: %v", err)
	}
	if len(byEvent["evt-1"]) != 0 {
		t.Fatalf("expected no reminders visible to another user, got %+v", byEvent["evt-1"])
	}
}

// ReplaceByEventID only discards userID's own rows on eventID, leaving
// another User's rows on the same Event untouched (ADR-0064).
func TestEventReminderRepository_ReplaceByEventID_ScopedToUser(t *testing.T) {
	repo, reminders, userID, otherUserID, calendarID := newTestEventReminderRepositoryWithSecondUser(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	if err := reminders.ReplaceByEventID(ctx, otherUserID, "evt-1", []Reminder{{OffsetMinutes: 45, Channel: "email"}}); err != nil {
		t.Fatalf("replace by event id (other user): %v", err)
	}
	if err := reminders.ReplaceByEventID(ctx, userID, "evt-1", []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}

	otherByEvent, err := reminders.ListByEventIDs(ctx, otherUserID, []string{"evt-1"})
	if err != nil {
		t.Fatalf("list by event ids (other user): %v", err)
	}
	if len(otherByEvent["evt-1"]) != 1 || otherByEvent["evt-1"][0].OffsetMinutes != 45 {
		t.Fatalf("expected the other user's own reminder untouched, got %+v", otherByEvent["evt-1"])
	}
}

// CopyByEventID copies every User's Reminder rows from one Event onto
// another — an Override creation's copy of its Master's whole Reminder set
// (AC6, ADR-0064), and creates nothing for a User who had none.
func TestEventReminderRepository_CopyByEventID_CopiesEveryUsersRows(t *testing.T) {
	repo, reminders, userID, otherUserID, calendarID := newTestEventReminderRepositoryWithSecondUser(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "master-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	mustCreateEvent(t, repo, "override-1", userID, calendarID, "2026-01-08T09:00:00Z", "2026-01-08T10:00:00Z")

	if err := reminders.ReplaceByEventID(ctx, userID, "master-1", []Reminder{
		{OffsetMinutes: 10, Channel: "notification"},
		{OffsetMinutes: 1440, Channel: "email"},
	}); err != nil {
		t.Fatalf("replace by event id (owner): %v", err)
	}
	if err := reminders.ReplaceByEventID(ctx, otherUserID, "master-1", []Reminder{
		{OffsetMinutes: 30, Channel: "email"},
	}); err != nil {
		t.Fatalf("replace by event id (other user): %v", err)
	}

	if err := reminders.CopyByEventID(ctx, "master-1", "override-1"); err != nil {
		t.Fatalf("copy by event id: %v", err)
	}

	ownerCopied, err := reminders.ListByEventIDs(ctx, userID, []string{"override-1"})
	if err != nil {
		t.Fatalf("list by event ids (owner): %v", err)
	}
	if len(ownerCopied["override-1"]) != 2 {
		t.Fatalf("expected the owner's 2 reminders copied onto the override, got %+v", ownerCopied["override-1"])
	}

	otherCopied, err := reminders.ListByEventIDs(ctx, otherUserID, []string{"override-1"})
	if err != nil {
		t.Fatalf("list by event ids (other user): %v", err)
	}
	if len(otherCopied["override-1"]) != 1 || otherCopied["override-1"][0].OffsetMinutes != 30 {
		t.Fatalf("expected the other user's 1 reminder copied onto the override, got %+v", otherCopied["override-1"])
	}

	// The copy mints new rows — it doesn't touch the Master's own.
	masterStillHas, err := reminders.ListByEventIDs(ctx, userID, []string{"master-1"})
	if err != nil {
		t.Fatalf("list by event ids (master): %v", err)
	}
	if len(masterStillHas["master-1"]) != 2 {
		t.Fatalf("expected the master's own reminders untouched, got %+v", masterStillHas["master-1"])
	}
}

// CopyByEventID creates nothing for a User who had no Reminders on the
// source Event (AC6).
func TestEventReminderRepository_CopyByEventID_CreatesNoneForUserWithNone(t *testing.T) {
	repo, reminders, userID, otherUserID, calendarID := newTestEventReminderRepositoryWithSecondUser(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "master-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	mustCreateEvent(t, repo, "override-1", userID, calendarID, "2026-01-08T09:00:00Z", "2026-01-08T10:00:00Z")

	if err := reminders.ReplaceByEventID(ctx, userID, "master-1", []Reminder{
		{OffsetMinutes: 10, Channel: "notification"},
	}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}
	// otherUserID has no Reminders on master-1 at all.

	if err := reminders.CopyByEventID(ctx, "master-1", "override-1"); err != nil {
		t.Fatalf("copy by event id: %v", err)
	}

	otherCopied, err := reminders.ListByEventIDs(ctx, otherUserID, []string{"override-1"})
	if err != nil {
		t.Fatalf("list by event ids (other user): %v", err)
	}
	if len(otherCopied["override-1"]) != 0 {
		t.Fatalf("expected no reminders created for a user who had none, got %+v", otherCopied["override-1"])
	}
}

// DeleteByUserAndCalendar clears userID's Reminders across every Event on
// calendarID, leaving another User's untouched — RevokeShare/LeaveShare's
// cleanup (ADR-0064).
func TestEventReminderRepository_DeleteByUserAndCalendar(t *testing.T) {
	repo, reminders, userID, otherUserID, calendarID := newTestEventReminderRepositoryWithSecondUser(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	mustCreateEvent(t, repo, "evt-2", userID, calendarID, "2026-01-02T09:00:00Z", "2026-01-02T10:00:00Z")

	if err := reminders.ReplaceByEventID(ctx, userID, "evt-1", []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id (evt-1, target user): %v", err)
	}
	if err := reminders.ReplaceByEventID(ctx, userID, "evt-2", []Reminder{{OffsetMinutes: 5, Channel: "email"}}); err != nil {
		t.Fatalf("replace by event id (evt-2, target user): %v", err)
	}
	if err := reminders.ReplaceByEventID(ctx, otherUserID, "evt-1", []Reminder{{OffsetMinutes: 20, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id (evt-1, other user): %v", err)
	}

	if err := reminders.DeleteByUserAndCalendar(ctx, userID, calendarID); err != nil {
		t.Fatalf("delete by user and calendar: %v", err)
	}

	targetUserByEvent, err := reminders.ListByEventIDs(ctx, userID, []string{"evt-1", "evt-2"})
	if err != nil {
		t.Fatalf("list by event ids (target user): %v", err)
	}
	if len(targetUserByEvent["evt-1"]) != 0 || len(targetUserByEvent["evt-2"]) != 0 {
		t.Fatalf("expected the target user's reminders cleared across the calendar, got %+v", targetUserByEvent)
	}

	otherUserByEvent, err := reminders.ListByEventIDs(ctx, otherUserID, []string{"evt-1"})
	if err != nil {
		t.Fatalf("list by event ids (other user): %v", err)
	}
	if len(otherUserByEvent["evt-1"]) != 1 {
		t.Fatalf("expected the other user's own reminder untouched, got %+v", otherUserByEvent["evt-1"])
	}
}
