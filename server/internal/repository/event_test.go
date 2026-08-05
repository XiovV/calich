package repository

import (
	"context"
	"errors"
	"reflect"
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

	created, err := repo.Create(ctx, "evt-1", userID, EventFields{CalendarID: calendarID, Title: "Standup", Start: start, End: end}, 0)
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
	if !reflect.DeepEqual(fetched, created) {
		t.Fatalf("expected fetched event %+v to equal created event %+v", fetched, created)
	}
}

func TestEventRepository_CreateAndUpdate_PersistsAllDay(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	start := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	created, err := repo.Create(ctx, "evt-1", userID, EventFields{CalendarID: calendarID, Title: "Holiday", Start: start, End: end, AllDay: true}, 0)
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if !created.AllDay {
		t.Fatalf("expected created event to be all-day, got %+v", created)
	}

	fetched, err := repo.GetByID(ctx, userID, "evt-1")
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if !fetched.AllDay {
		t.Fatalf("expected fetched event to be all-day, got %+v", fetched)
	}

	updated, err := repo.Update(ctx, userID, "evt-1", EventFields{CalendarID: calendarID, Title: "Holiday", Start: start, End: end}, 0)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.AllDay {
		t.Fatalf("expected updated event to no longer be all-day, got %+v", updated)
	}
}

// A nil tzid (Floating Event, ADR-0019) must round-trip as nil, and a named
// zone must round-trip verbatim through create, get, and update.
func TestEventRepository_CreateAndUpdate_RoundTripsTzid(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	created, err := repo.Create(ctx, "evt-1", userID, EventFields{CalendarID: calendarID, Title: "Standup", Start: start, End: end}, 0)
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if created.Tzid != nil {
		t.Fatalf("expected nil tzid (Floating Event), got %+v", created)
	}

	fetched, err := repo.GetByID(ctx, userID, "evt-1")
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if fetched.Tzid != nil {
		t.Fatalf("expected fetched tzid to stay nil, got %+v", fetched)
	}

	zone := "Europe/Berlin"
	updated, err := repo.Update(ctx, userID, "evt-1", EventFields{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Tzid: &zone}, 0)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Tzid == nil || *updated.Tzid != zone {
		t.Fatalf("expected updated tzid %q, got %+v", zone, updated)
	}

	fetched, err = repo.GetByID(ctx, userID, "evt-1")
	if err != nil {
		t.Fatalf("get by id after update: %v", err)
	}
	if fetched.Tzid == nil || *fetched.Tzid != zone {
		t.Fatalf("expected fetched tzid %q after update, got %+v", zone, fetched)
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

	updated, err := repo.Update(ctx, userID, "evt-1", EventFields{CalendarID: calendarID, Title: "Renamed", Start: newStart, End: newEnd}, 0)
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

	_, err := repo.Update(context.Background(), userID, "nope", EventFields{CalendarID: calendarID, Title: "Renamed", Start: start, End: end}, 0)
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

	_, err := repo.Update(ctx, 99999, "evt-1", EventFields{CalendarID: calendarID, Title: "Renamed", Start: start, End: end}, 0)
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

func TestEventRepository_CascadeDeletesOverridesWhenMasterDeleted(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "master", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	parentID := "master"
	recurrenceID := mustParseTime(t, "2026-01-02T09:00:00Z")
	if _, err := repo.Create(ctx, "override", userID, EventFields{CalendarID: calendarID, Title: "Moved", Start: mustParseTime(t, "2026-01-02T11:00:00Z"), End: mustParseTime(t, "2026-01-02T12:00:00Z"), ParentID: &parentID, RecurrenceID: &recurrenceID}, 0); err != nil {
		t.Fatalf("create override: %v", err)
	}

	if err := repo.Delete(ctx, userID, "master"); err != nil {
		t.Fatalf("delete master: %v", err)
	}

	if _, err := repo.GetByID(ctx, userID, "override"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected the override to be cascade-deleted with its master, got %v", err)
	}
}

func TestEventRepository_ReparentOverridesFrom(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "old-master", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	mustCreateEvent(t, repo, "new-master", userID, calendarID, "2026-01-05T09:00:00Z", "2026-01-05T10:00:00Z")

	oldParentID := "old-master"
	beforeSplit := mustParseTime(t, "2026-01-02T09:00:00Z")
	if _, err := repo.Create(ctx, "before-split", userID, EventFields{CalendarID: calendarID, Title: "Before", Start: mustParseTime(t, "2026-01-02T11:00:00Z"), End: mustParseTime(t, "2026-01-02T12:00:00Z"), ParentID: &oldParentID, RecurrenceID: &beforeSplit}, 0); err != nil {
		t.Fatalf("create override before split: %v", err)
	}
	afterSplit := mustParseTime(t, "2026-01-06T09:00:00Z")
	if _, err := repo.Create(ctx, "after-split", userID, EventFields{CalendarID: calendarID, Title: "After", Start: mustParseTime(t, "2026-01-06T11:00:00Z"), End: mustParseTime(t, "2026-01-06T12:00:00Z"), ParentID: &oldParentID, RecurrenceID: &afterSplit}, 0); err != nil {
		t.Fatalf("create override after split: %v", err)
	}

	fromStart := mustParseTime(t, "2026-01-05T00:00:00Z")
	if err := repo.ReparentOverridesFrom(ctx, userID, "old-master", "new-master", fromStart); err != nil {
		t.Fatalf("reparent: %v", err)
	}

	before, err := repo.GetByID(ctx, userID, "before-split")
	if err != nil {
		t.Fatalf("get before-split: %v", err)
	}
	if before.ParentID == nil || *before.ParentID != "old-master" {
		t.Fatalf("expected before-split to keep its old parent, got %+v", before)
	}

	after, err := repo.GetByID(ctx, userID, "after-split")
	if err != nil {
		t.Fatalf("get after-split: %v", err)
	}
	if after.ParentID == nil || *after.ParentID != "new-master" {
		t.Fatalf("expected after-split to be reparented to new-master, got %+v", after)
	}
}

func TestEventRepository_DeleteChildrenOf(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "master", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	parentID := "master"
	recurrenceID := mustParseTime(t, "2026-01-02T09:00:00Z")
	if _, err := repo.Create(ctx, "override", userID, EventFields{CalendarID: calendarID, Title: "Moved", Start: mustParseTime(t, "2026-01-02T11:00:00Z"), End: mustParseTime(t, "2026-01-02T12:00:00Z"), ParentID: &parentID, RecurrenceID: &recurrenceID}, 0); err != nil {
		t.Fatalf("create override: %v", err)
	}

	if err := repo.DeleteChildrenOf(ctx, userID, "master"); err != nil {
		t.Fatalf("delete children: %v", err)
	}

	if _, err := repo.GetByID(ctx, userID, "override"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected the override to be deleted, got %v", err)
	}
	if _, err := repo.GetByID(ctx, userID, "master"); err != nil {
		t.Fatalf("expected the master to survive, got %v", err)
	}
}

// ListAllWithReminders is the firing engine's read path (ADR-0021): it spans
// every user, unlike ListByUser, and only returns events that actually carry
// a Reminder.
func TestEventRepository_ListAllWithReminders(t *testing.T) {
	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	ctx := context.Background()

	users := NewUserRepository(sqlDB)
	userA, err := users.Create(ctx, "user-a", "hash", false)
	if err != nil {
		t.Fatalf("create user a: %v", err)
	}
	userB, err := users.Create(ctx, "user-b", "hash", false)
	if err != nil {
		t.Fatalf("create user b: %v", err)
	}

	calendars := NewCalendarRepository(sqlDB)
	calA, err := calendars.Create(ctx, userA.ID, "cal-a", "A", "peacock")
	if err != nil {
		t.Fatalf("create calendar a: %v", err)
	}
	calB, err := calendars.Create(ctx, userB.ID, "cal-b", "B", "tomato")
	if err != nil {
		t.Fatalf("create calendar b: %v", err)
	}

	repo := NewEventRepository(sqlDB)
	reminders := NewEventReminderRepository(sqlDB)
	mustCreateEvent(t, repo, "with-reminder", userA.ID, calA.ID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	mustCreateEvent(t, repo, "without-reminder", userA.ID, calA.ID, "2026-01-02T09:00:00Z", "2026-01-02T10:00:00Z")
	mustCreateEvent(t, repo, "other-users-reminder", userB.ID, calB.ID, "2026-01-03T09:00:00Z", "2026-01-03T10:00:00Z")

	if err := reminders.ReplaceByEventID(ctx, "with-reminder", []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}
	if err := reminders.ReplaceByEventID(ctx, "other-users-reminder", []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}

	events, err := repo.ListAllWithReminders(ctx)
	if err != nil {
		t.Fatalf("list all with reminders: %v", err)
	}

	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 events with reminders across both users, got %v", ids)
	}
	for _, want := range []string{"with-reminder", "other-users-reminder"} {
		found := false
		for _, id := range ids {
			if id == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected %q in %v", want, ids)
		}
	}
}

func mustCreateEvent(t *testing.T, repo *EventRepository, id string, userID int64, calendarID, start, end string) {
	t.Helper()
	if _, err := repo.Create(context.Background(), id, userID, EventFields{CalendarID: calendarID, Title: id, Start: mustParseTime(t, start), End: mustParseTime(t, end)}, 0); err != nil {
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
