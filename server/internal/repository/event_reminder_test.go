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

	return NewEventRepository(sqlDB), NewEventReminderRepository(sqlDB), user.ID, cal.ID
}

func TestEventReminderRepository_ReplaceByEventIDAndListByEventIDs(t *testing.T) {
	repo, reminders, userID, calendarID := newTestEventReminderRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	want := []Reminder{
		{OffsetMinutes: 10, Channel: "notification"},
		{OffsetMinutes: 60, Channel: "email"},
	}
	if err := reminders.ReplaceByEventID(ctx, "evt-1", want); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}

	byEvent, err := reminders.ListByEventIDs(ctx, []string{"evt-1"})
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
	if err := reminders.ReplaceByEventID(ctx, "evt-1", []Reminder{
		{OffsetMinutes: 10, Channel: "notification"},
		{OffsetMinutes: 60, Channel: "email"},
	}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}

	byEvent, err := reminders.ListByEventIDs(ctx, []string{"evt-1"})
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
	_, reminders, _, _ := newTestEventReminderRepository(t)

	byEvent, err := reminders.ListByEventIDs(context.Background(), nil)
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

	if err := reminders.ReplaceByEventID(ctx, "evt-1", []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}
	if err := reminders.ReplaceByEventID(ctx, "evt-1", []Reminder{{OffsetMinutes: 30, Channel: "email"}}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}

	byEvent, err := reminders.ListByEventIDs(ctx, []string{"evt-1"})
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

	if err := reminders.ReplaceByEventID(ctx, "evt-1", []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}
	if err := reminders.ReplaceByEventID(ctx, "evt-1", nil); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}

	byEvent, err := reminders.ListByEventIDs(ctx, []string{"evt-1"})
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
	if err := reminders.ReplaceByEventID(ctx, "evt-1", []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}

	if err := repo.Delete(ctx, "evt-1"); err != nil {
		t.Fatalf("delete event: %v", err)
	}

	byEvent, err := reminders.ListByEventIDs(ctx, []string{"evt-1"})
	if err != nil {
		t.Fatalf("list by event ids: %v", err)
	}
	if len(byEvent["evt-1"]) != 0 {
		t.Fatalf("expected reminders to be cascade-deleted with their event, got %+v", byEvent["evt-1"])
	}
}
