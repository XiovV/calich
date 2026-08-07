package repository

import (
	"context"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
)

// newTestReminderOverrideRepository returns an EventRepository and a
// ReminderOverrideRepository sharing one in-memory database, plus two real
// user ids and a calendar id to satisfy foreign keys.
func newTestReminderOverrideRepository(t *testing.T) (events *EventRepository, overrides *ReminderOverrideRepository, userID, otherUserID int64, calendarID string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := NewUserRepository(sqlDB)
	user, err := users.Create(context.Background(), "user-a", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	other, err := users.Create(context.Background(), "user-b", "hash", false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	calendars := NewCalendarRepository(sqlDB)
	cal, err := calendars.Create(context.Background(), user.ID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	return NewEventRepository(sqlDB), NewReminderOverrideRepository(sqlDB), user.ID, other.ID, cal.ID
}

func intPtr(v int) *int       { return &v }
func strPtr(v string) *string { return &v }

func TestReminderOverrideRepository_GetReturnsNotFoundWhenUnset(t *testing.T) {
	_, overrides, userID, _, _ := newTestReminderOverrideRepository(t)

	if _, err := overrides.Get(context.Background(), userID, "evt-1"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestReminderOverrideRepository_UpsertAndGet(t *testing.T) {
	events, overrides, userID, _, calendarID := newTestReminderOverrideRepository(t)
	ctx := context.Background()
	mustCreateEvent(t, events, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	want := ReminderOverride{OffsetMinutes: intPtr(120), Channel: strPtr("email"), Muted: false}
	if _, err := overrides.Upsert(ctx, userID, "evt-1", want); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := overrides.Get(ctx, userID, "evt-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if *got.OffsetMinutes != 120 || *got.Channel != "email" || got.Muted {
		t.Fatalf("unexpected override: %+v", got)
	}
}

// Upsert replaces a prior override wholesale rather than merging into it.
func TestReminderOverrideRepository_UpsertReplaces(t *testing.T) {
	events, overrides, userID, _, calendarID := newTestReminderOverrideRepository(t)
	ctx := context.Background()
	mustCreateEvent(t, events, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	if _, err := overrides.Upsert(ctx, userID, "evt-1", ReminderOverride{OffsetMinutes: intPtr(30), Channel: strPtr("notification")}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := overrides.Upsert(ctx, userID, "evt-1", ReminderOverride{Muted: true}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := overrides.Get(ctx, userID, "evt-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Muted || got.OffsetMinutes != nil || got.Channel != nil {
		t.Fatalf("expected wholesale replace, got %+v", got)
	}
}

func TestReminderOverrideRepository_Delete(t *testing.T) {
	events, overrides, userID, _, calendarID := newTestReminderOverrideRepository(t)
	ctx := context.Background()
	mustCreateEvent(t, events, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	if _, err := overrides.Upsert(ctx, userID, "evt-1", ReminderOverride{Muted: true}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := overrides.Delete(ctx, userID, "evt-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := overrides.Get(ctx, userID, "evt-1"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

// Deleting an override that was never set is a no-op, not an error.
func TestReminderOverrideRepository_DeleteWhenUnsetIsNoop(t *testing.T) {
	_, overrides, userID, _, _ := newTestReminderOverrideRepository(t)

	if err := overrides.Delete(context.Background(), userID, "evt-1"); err != nil {
		t.Fatalf("expected no error deleting an unset override, got %v", err)
	}
}

func TestReminderOverrideRepository_DeleteByUserAndCalendar(t *testing.T) {
	events, overrides, userID, otherUserID, calendarID := newTestReminderOverrideRepository(t)
	ctx := context.Background()
	mustCreateEvent(t, events, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	mustCreateEvent(t, events, "evt-2", userID, calendarID, "2026-01-02T09:00:00Z", "2026-01-02T10:00:00Z")

	if _, err := overrides.Upsert(ctx, otherUserID, "evt-1", ReminderOverride{Muted: true}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := overrides.Upsert(ctx, otherUserID, "evt-2", ReminderOverride{Muted: true}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// A different User's override on the same calendar must survive.
	if _, err := overrides.Upsert(ctx, userID, "evt-1", ReminderOverride{Muted: true}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := overrides.DeleteByUserAndCalendar(ctx, otherUserID, calendarID); err != nil {
		t.Fatalf("delete by user and calendar: %v", err)
	}

	if _, err := overrides.Get(ctx, otherUserID, "evt-1"); err != ErrNotFound {
		t.Fatalf("expected otherUserID's evt-1 override gone, got %v", err)
	}
	if _, err := overrides.Get(ctx, otherUserID, "evt-2"); err != ErrNotFound {
		t.Fatalf("expected otherUserID's evt-2 override gone, got %v", err)
	}
	if _, err := overrides.Get(ctx, userID, "evt-1"); err != nil {
		t.Fatalf("expected userID's own override to survive, got %v", err)
	}
}

func TestReminderOverrideRepository_ListByEventIDs(t *testing.T) {
	events, overrides, userID, otherUserID, calendarID := newTestReminderOverrideRepository(t)
	ctx := context.Background()
	mustCreateEvent(t, events, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	mustCreateEvent(t, events, "evt-2", userID, calendarID, "2026-01-02T09:00:00Z", "2026-01-02T10:00:00Z")

	if _, err := overrides.Upsert(ctx, userID, "evt-1", ReminderOverride{OffsetMinutes: intPtr(5)}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := overrides.Upsert(ctx, otherUserID, "evt-1", ReminderOverride{Muted: true}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	byEvent, err := overrides.ListByEventIDs(ctx, []string{"evt-1", "evt-2"})
	if err != nil {
		t.Fatalf("list by event ids: %v", err)
	}
	if len(byEvent["evt-1"]) != 2 {
		t.Fatalf("expected 2 overrides on evt-1, got %+v", byEvent["evt-1"])
	}
	if _, ok := byEvent["evt-2"]; ok {
		t.Fatalf("expected no overrides on evt-2, got %+v", byEvent["evt-2"])
	}
	if !byEvent["evt-1"][otherUserID].Muted {
		t.Fatalf("expected otherUserID's override to be muted")
	}
}

func TestReminderOverrideRepository_ListByEventIDs_EmptyInput(t *testing.T) {
	_, overrides, _, _, _ := newTestReminderOverrideRepository(t)

	byEvent, err := overrides.ListByEventIDs(context.Background(), nil)
	if err != nil {
		t.Fatalf("list by event ids: %v", err)
	}
	if len(byEvent) != 0 {
		t.Fatalf("expected no overrides, got %+v", byEvent)
	}
}

func TestReminderOverrideRepository_CascadeDeletesWhenEventDeleted(t *testing.T) {
	events, overrides, userID, _, calendarID := newTestReminderOverrideRepository(t)
	ctx := context.Background()
	mustCreateEvent(t, events, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	if _, err := overrides.Upsert(ctx, userID, "evt-1", ReminderOverride{Muted: true}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := events.Delete(ctx, "evt-1"); err != nil {
		t.Fatalf("delete event: %v", err)
	}

	if _, err := overrides.Get(ctx, userID, "evt-1"); err != ErrNotFound {
		t.Fatalf("expected override to be cascade-deleted with its event, got %v", err)
	}
}
