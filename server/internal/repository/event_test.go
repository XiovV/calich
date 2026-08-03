package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
)

// newTestEventRepository returns an EventRepository plus a real user id and
// two real calendar ids (one per user) to satisfy events' foreign keys.
func newTestEventRepository(t *testing.T) (repo *EventRepository, userID int64, calendarID, otherCalendarID string) {
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
	cal, err := calendars.Create(context.Background(), user.ID, "cal-1", "Personal", "peacock")
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	otherCal, err := calendars.Create(context.Background(), other.ID, "cal-2", "Other", "tomato")
	if err != nil {
		t.Fatalf("create other calendar: %v", err)
	}

	return NewEventRepository(sqlDB), user.ID, cal.ID, otherCal.ID
}

func TestEventRepository_CreateAndGetByID(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	created, err := repo.Create(ctx, "evt-1", userID, calendarID, "Standup", start, end, "")
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if created.Title != "Standup" || created.CalendarID != calendarID {
		t.Fatalf("unexpected created event: %+v", created)
	}

	fetched, err := repo.GetByID(ctx, userID, "evt-1")
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if fetched != created {
		t.Fatalf("expected fetched event %+v to equal created event %+v", fetched, created)
	}
}

func TestEventRepository_GetByID_NotFound(t *testing.T) {
	repo, userID, _, _ := newTestEventRepository(t)

	_, err := repo.GetByID(context.Background(), userID, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEventRepository_ListByUser_NoRangeReturnsAll(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	mustCreateEvent(t, repo, "evt-2", userID, calendarID, "2026-02-01T09:00:00Z", "2026-02-01T10:00:00Z")

	events, err := repo.ListByUser(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list by user: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
}

func TestEventRepository_ListByUser_FiltersByRange(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "jan", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	mustCreateEvent(t, repo, "feb", userID, calendarID, "2026-02-01T09:00:00Z", "2026-02-01T10:00:00Z")
	mustCreateEvent(t, repo, "mar", userID, calendarID, "2026-03-01T09:00:00Z", "2026-03-01T10:00:00Z")

	from := mustParseTime(t, "2026-01-15T00:00:00Z")
	to := mustParseTime(t, "2026-02-15T00:00:00Z")

	events, err := repo.ListByUser(ctx, userID, &from, &to)
	if err != nil {
		t.Fatalf("list by user: %v", err)
	}
	if len(events) != 1 || events[0].ID != "feb" {
		t.Fatalf("expected only the february event, got %+v", events)
	}
}

func TestEventRepository_ListByUser_ScopedToUser(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	events, err := repo.ListByUser(ctx, 99999, nil, nil)
	if err != nil {
		t.Fatalf("list by user: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events for an unrelated user, got %d", len(events))
	}
}

func TestEventRepository_Update(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	newStart := mustParseTime(t, "2026-01-01T11:00:00Z")
	newEnd := mustParseTime(t, "2026-01-01T12:00:00Z")

	updated, err := repo.Update(ctx, userID, "evt-1", calendarID, "Renamed", newStart, newEnd, "")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != "Renamed" || !updated.Start.Equal(newStart) || !updated.End.Equal(newEnd) {
		t.Fatalf("unexpected updated event: %+v", updated)
	}
}

func TestEventRepository_Update_NotFound(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)

	start := mustParseTime(t, "2026-01-01T09:00:00Z")
	end := mustParseTime(t, "2026-01-01T10:00:00Z")

	_, err := repo.Update(context.Background(), userID, "nope", calendarID, "Renamed", start, end, "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEventRepository_Update_ScopedToUser(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	start := mustParseTime(t, "2026-01-01T11:00:00Z")
	end := mustParseTime(t, "2026-01-01T12:00:00Z")

	_, err := repo.Update(ctx, 99999, "evt-1", calendarID, "Renamed", start, end, "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound updating another user's event, got %v", err)
	}
}

func TestEventRepository_Delete(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	if err := repo.Delete(ctx, userID, "evt-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := repo.GetByID(ctx, userID, "evt-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestEventRepository_Delete_NotFound(t *testing.T) {
	repo, userID, _, _ := newTestEventRepository(t)

	err := repo.Delete(context.Background(), userID, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEventRepository_Delete_ScopedToUser(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	err := repo.Delete(ctx, 99999, "evt-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting another user's event, got %v", err)
	}

	if _, err := repo.GetByID(ctx, userID, "evt-1"); err != nil {
		t.Fatalf("expected event to still exist, got %v", err)
	}
}

func TestEventRepository_CascadeDeletesWhenCalendarDeleted(t *testing.T) {
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

	calendars := NewCalendarRepository(sqlDB)
	cal, err := calendars.Create(context.Background(), user.ID, "cal-1", "Personal", "peacock")
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	events := NewEventRepository(sqlDB)
	mustCreateEvent(t, events, "evt-1", user.ID, cal.ID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	if err := calendars.Delete(context.Background(), user.ID, cal.ID); err != nil {
		t.Fatalf("delete calendar: %v", err)
	}

	_, err = events.GetByID(context.Background(), user.ID, "evt-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected the event to be cascade-deleted with its calendar, got %v", err)
	}
}

func mustCreateEvent(t *testing.T, repo *EventRepository, id string, userID int64, calendarID, start, end string) {
	t.Helper()
	if _, err := repo.Create(context.Background(), id, userID, calendarID, id, mustParseTime(t, start), mustParseTime(t, end), ""); err != nil {
		t.Fatalf("create event %q: %v", id, err)
	}
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return ts
}
