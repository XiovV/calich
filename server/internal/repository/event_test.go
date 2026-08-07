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
	cal, err := calendars.Create(context.Background(), user.ID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	otherCal, err := calendars.Create(context.Background(), other.ID, "cal-2", CalendarFields{Name: "Other", Color: "tomato"})
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

	created, err := repo.Create(ctx, "evt-1", &userID, EventFields{CalendarID: calendarID, Title: "Standup", Start: start, End: end}, 0)
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if created.Title != "Standup" || created.CalendarID != calendarID {
		t.Fatalf("unexpected created event: %+v", created)
	}
	if created.CreatedBy == nil || *created.CreatedBy != userID {
		t.Fatalf("expected created_by %d, got %+v", userID, created.CreatedBy)
	}

	fetched, err := repo.GetByID(ctx, "evt-1")
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

	created, err := repo.Create(ctx, "evt-1", &userID, EventFields{CalendarID: calendarID, Title: "Holiday", Start: start, End: end, AllDay: true}, 0)
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if !created.AllDay {
		t.Fatalf("expected created event to be all-day, got %+v", created)
	}

	fetched, err := repo.GetByID(ctx, "evt-1")
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if !fetched.AllDay {
		t.Fatalf("expected fetched event to be all-day, got %+v", fetched)
	}

	updated, err := repo.Update(ctx, "evt-1", EventFields{CalendarID: calendarID, Title: "Holiday", Start: start, End: end}, 0)
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

	created, err := repo.Create(ctx, "evt-1", &userID, EventFields{CalendarID: calendarID, Title: "Standup", Start: start, End: end}, 0)
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if created.Tzid != nil {
		t.Fatalf("expected nil tzid (Floating Event), got %+v", created)
	}

	fetched, err := repo.GetByID(ctx, "evt-1")
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if fetched.Tzid != nil {
		t.Fatalf("expected fetched tzid to stay nil, got %+v", fetched)
	}

	zone := "Europe/Berlin"
	updated, err := repo.Update(ctx, "evt-1", EventFields{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Tzid: &zone}, 0)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Tzid == nil || *updated.Tzid != zone {
		t.Fatalf("expected updated tzid %q, got %+v", zone, updated)
	}

	fetched, err = repo.GetByID(ctx, "evt-1")
	if err != nil {
		t.Fatalf("get by id after update: %v", err)
	}
	if fetched.Tzid == nil || *fetched.Tzid != zone {
		t.Fatalf("expected fetched tzid %q after update, got %+v", zone, fetched)
	}
}

func TestEventRepository_GetByID_NotFound(t *testing.T) {
	repo, _, _, _ := newTestEventRepository(t)

	_, err := repo.GetByID(context.Background(), "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEventRepository_ListByCalendarIDs_NoRangeReturnsAll(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	mustCreateEvent(t, repo, "evt-2", userID, calendarID, "2026-02-01T09:00:00Z", "2026-02-01T10:00:00Z")

	events, err := repo.ListByCalendarIDs(ctx, []string{calendarID}, nil, nil)
	if err != nil {
		t.Fatalf("list by calendar ids: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
}

func TestEventRepository_ListByCalendarIDs_FiltersByRange(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "jan", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	mustCreateEvent(t, repo, "feb", userID, calendarID, "2026-02-01T09:00:00Z", "2026-02-01T10:00:00Z")
	mustCreateEvent(t, repo, "mar", userID, calendarID, "2026-03-01T09:00:00Z", "2026-03-01T10:00:00Z")

	from := mustParseTime(t, "2026-01-15T00:00:00Z")
	to := mustParseTime(t, "2026-02-15T00:00:00Z")

	events, err := repo.ListByCalendarIDs(ctx, []string{calendarID}, &from, &to)
	if err != nil {
		t.Fatalf("list by calendar ids: %v", err)
	}
	if len(events) != 1 || events[0].ID != "feb" {
		t.Fatalf("expected only the february event, got %+v", events)
	}
}

// TestEventRepository_ListByCalendarIDs_RecurringMasterFarBeforeWindowIsIncluded
// is #80's motivating case: a naive filter on the stored start/end columns
// would exclude this Master from a 2026 window since its own start is in
// 2009, even though its open-ended weekly rule still generates Occurrences
// there.
func TestEventRepository_ListByCalendarIDs_RecurringMasterFarBeforeWindowIsIncluded(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateRecurringEvent(t, repo, "old-standup", userID, calendarID,
		"2009-01-05T09:00:00Z", "2009-01-05T09:30:00Z", "FREQ=WEEKLY;BYDAY=MO")

	from := mustParseTime(t, "2026-01-01T00:00:00Z")
	to := mustParseTime(t, "2026-02-01T00:00:00Z")

	events, err := repo.ListByCalendarIDs(ctx, []string{calendarID}, &from, &to)
	if err != nil {
		t.Fatalf("list by calendar ids: %v", err)
	}
	if len(events) != 1 || events[0].ID != "old-standup" {
		t.Fatalf("expected the recurring master to be included, got %+v", events)
	}
}

// TestEventRepository_ListByCalendarIDs_RecurringMasterEndedBeforeWindowIsExcluded
// is the flip side: a Master whose rule stopped generating Occurrences
// before the window (via UNTIL) must not come back just because it's
// recurring.
func TestEventRepository_ListByCalendarIDs_RecurringMasterEndedBeforeWindowIsExcluded(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateRecurringEvent(t, repo, "retired-standup", userID, calendarID,
		"2009-01-05T09:00:00Z", "2009-01-05T09:30:00Z", "FREQ=WEEKLY;BYDAY=MO;UNTIL=20100301T000000Z")

	from := mustParseTime(t, "2026-01-01T00:00:00Z")
	to := mustParseTime(t, "2026-02-01T00:00:00Z")

	events, err := repo.ListByCalendarIDs(ctx, []string{calendarID}, &from, &to)
	if err != nil {
		t.Fatalf("list by calendar ids: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected the ended series to be excluded, got %+v", events)
	}
}

// TestEventRepository_ListByCalendarIDs_FromOnly_ExcludesEndedRecurringMaster
// regression-tests the one-sided window case: a from-only query must still
// run the Go-side rrule check on recurring rows, not just include every
// recurring Master unconditionally because there's no "to" to bound it by.
func TestEventRepository_ListByCalendarIDs_FromOnly_ExcludesEndedRecurringMaster(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateRecurringEvent(t, repo, "retired-standup", userID, calendarID,
		"2009-01-05T09:00:00Z", "2009-01-05T09:30:00Z", "FREQ=WEEKLY;BYDAY=MO;UNTIL=20100301T000000Z")
	mustCreateRecurringEvent(t, repo, "ongoing-standup", userID, calendarID,
		"2009-01-05T09:00:00Z", "2009-01-05T09:30:00Z", "FREQ=WEEKLY;BYDAY=MO")

	from := mustParseTime(t, "2026-01-01T00:00:00Z")

	events, err := repo.ListByCalendarIDs(ctx, []string{calendarID}, &from, nil)
	if err != nil {
		t.Fatalf("list by calendar ids: %v", err)
	}
	if len(events) != 1 || events[0].ID != "ongoing-standup" {
		t.Fatalf("expected only the still-ongoing series, got %+v", events)
	}
}

// TestEventRepository_ListByCalendarIDs_RecurringMasterStartingAfterWindowIsExcluded
// checks the cheap SQL pre-filter's lower bound: a series can't have
// Occurrences before its own first one, so a Master starting on/after the
// window's end is excluded without even reaching the Go-side rrule check.
func TestEventRepository_ListByCalendarIDs_RecurringMasterStartingAfterWindowIsExcluded(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateRecurringEvent(t, repo, "future-standup", userID, calendarID,
		"2027-01-04T09:00:00Z", "2027-01-04T09:30:00Z", "FREQ=WEEKLY;BYDAY=MO")

	from := mustParseTime(t, "2026-01-01T00:00:00Z")
	to := mustParseTime(t, "2026-02-01T00:00:00Z")

	events, err := repo.ListByCalendarIDs(ctx, []string{calendarID}, &from, &to)
	if err != nil {
		t.Fatalf("list by calendar ids: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected the future series to be excluded, got %+v", events)
	}
}

// TestEventRepository_ListByCalendarIDs_ScopedToCalendarIDs is the
// repository-level half of authorization: it only ever returns events whose
// calendar_id is in the caller-supplied list. Which calendars the caller may
// see is the service layer's job (ADR-0034) — the repository only trusts the
// list it's handed.
func TestEventRepository_ListByCalendarIDs_ScopedToCalendarIDs(t *testing.T) {
	repo, userID, calendarID, otherCalendarID := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "mine", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	mustCreateEvent(t, repo, "not-mine", userID, otherCalendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	events, err := repo.ListByCalendarIDs(ctx, []string{calendarID}, nil, nil)
	if err != nil {
		t.Fatalf("list by calendar ids: %v", err)
	}
	if len(events) != 1 || events[0].ID != "mine" {
		t.Fatalf("expected only the requested calendar's event, got %+v", events)
	}
}

func TestEventRepository_ListByCalendarIDs_EmptyCalendarIDsReturnsNoRows(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	events, err := repo.ListByCalendarIDs(ctx, nil, nil, nil)
	if err != nil {
		t.Fatalf("list by calendar ids: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events for an empty calendarIDs, got %d", len(events))
	}
}

func TestEventRepository_Update(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	newStart := mustParseTime(t, "2026-01-01T11:00:00Z")
	newEnd := mustParseTime(t, "2026-01-01T12:00:00Z")

	updated, err := repo.Update(ctx, "evt-1", EventFields{CalendarID: calendarID, Title: "Renamed", Start: newStart, End: newEnd}, 0)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != "Renamed" || !updated.Start.Equal(newStart) || !updated.End.Equal(newEnd) {
		t.Fatalf("unexpected updated event: %+v", updated)
	}
}

func TestEventRepository_Update_NotFound(t *testing.T) {
	repo, _, calendarID, _ := newTestEventRepository(t)

	start := mustParseTime(t, "2026-01-01T09:00:00Z")
	end := mustParseTime(t, "2026-01-01T10:00:00Z")

	_, err := repo.Update(context.Background(), "nope", EventFields{CalendarID: calendarID, Title: "Renamed", Start: start, End: end}, 0)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEventRepository_Delete(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	if err := repo.Delete(ctx, "evt-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := repo.GetByID(ctx, "evt-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestEventRepository_Delete_NotFound(t *testing.T) {
	repo, _, _, _ := newTestEventRepository(t)

	err := repo.Delete(context.Background(), "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
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
	cal, err := calendars.Create(context.Background(), user.ID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	events := NewEventRepository(sqlDB)
	mustCreateEvent(t, events, "evt-1", user.ID, cal.ID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	if err := calendars.Delete(context.Background(), user.ID, cal.ID); err != nil {
		t.Fatalf("delete calendar: %v", err)
	}

	_, err = events.GetByID(context.Background(), "evt-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected the event to be cascade-deleted with its calendar, got %v", err)
	}
}

// TestEventRepository_CreatedByPreservedWhenCreatingUserDeleted is the other
// half of ADR-0034's cascade story: created_by is attribution only, so
// deleting the User who created an Event must not take the Event down with
// it — only deleting the Event's Calendar (via its Owner) does that. Unlike
// the dropped user_id, created_by uses ON DELETE SET NULL.
func TestEventRepository_CreatedByPreservedWhenCreatingUserDeleted(t *testing.T) {
	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	ctx := context.Background()

	users := NewUserRepository(sqlDB)
	owner, err := users.Create(ctx, "owner", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	creator, err := users.Create(ctx, "creator", "hash", false)
	if err != nil {
		t.Fatalf("create creator: %v", err)
	}

	calendars := NewCalendarRepository(sqlDB)
	cal, err := calendars.Create(ctx, owner.ID, "cal-1", CalendarFields{Name: "Family", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	events := NewEventRepository(sqlDB)
	if _, err := events.Create(ctx, "evt-1", &creator.ID, EventFields{CalendarID: cal.ID, Title: "Standup", Start: mustParseTime(t, "2026-01-01T09:00:00Z"), End: mustParseTime(t, "2026-01-01T10:00:00Z")}, 0); err != nil {
		t.Fatalf("create event: %v", err)
	}

	if _, err := sqlDB.ExecContext(ctx, "DELETE FROM users WHERE id = ?", creator.ID); err != nil {
		t.Fatalf("delete creator: %v", err)
	}

	fetched, err := events.GetByID(ctx, "evt-1")
	if err != nil {
		t.Fatalf("expected the event to survive its creator's deletion, got %v", err)
	}
	if fetched.CreatedBy != nil {
		t.Fatalf("expected created_by to be cleared to nil, got %+v", fetched.CreatedBy)
	}
}

func TestEventRepository_CascadeDeletesOverridesWhenMasterDeleted(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "master", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	parentID := "master"
	recurrenceID := mustParseTime(t, "2026-01-02T09:00:00Z")
	if _, err := repo.Create(ctx, "override", &userID, EventFields{CalendarID: calendarID, Title: "Moved", Start: mustParseTime(t, "2026-01-02T11:00:00Z"), End: mustParseTime(t, "2026-01-02T12:00:00Z"), ParentID: &parentID, RecurrenceID: &recurrenceID}, 0); err != nil {
		t.Fatalf("create override: %v", err)
	}

	if err := repo.Delete(ctx, "master"); err != nil {
		t.Fatalf("delete master: %v", err)
	}

	if _, err := repo.GetByID(ctx, "override"); !errors.Is(err, ErrNotFound) {
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
	if _, err := repo.Create(ctx, "before-split", &userID, EventFields{CalendarID: calendarID, Title: "Before", Start: mustParseTime(t, "2026-01-02T11:00:00Z"), End: mustParseTime(t, "2026-01-02T12:00:00Z"), ParentID: &oldParentID, RecurrenceID: &beforeSplit}, 0); err != nil {
		t.Fatalf("create override before split: %v", err)
	}
	afterSplit := mustParseTime(t, "2026-01-06T09:00:00Z")
	if _, err := repo.Create(ctx, "after-split", &userID, EventFields{CalendarID: calendarID, Title: "After", Start: mustParseTime(t, "2026-01-06T11:00:00Z"), End: mustParseTime(t, "2026-01-06T12:00:00Z"), ParentID: &oldParentID, RecurrenceID: &afterSplit}, 0); err != nil {
		t.Fatalf("create override after split: %v", err)
	}

	fromStart := mustParseTime(t, "2026-01-05T00:00:00Z")
	if err := repo.ReparentOverridesFrom(ctx, "old-master", "new-master", fromStart); err != nil {
		t.Fatalf("reparent: %v", err)
	}

	before, err := repo.GetByID(ctx, "before-split")
	if err != nil {
		t.Fatalf("get before-split: %v", err)
	}
	if before.ParentID == nil || *before.ParentID != "old-master" {
		t.Fatalf("expected before-split to keep its old parent, got %+v", before)
	}

	after, err := repo.GetByID(ctx, "after-split")
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
	if _, err := repo.Create(ctx, "override", &userID, EventFields{CalendarID: calendarID, Title: "Moved", Start: mustParseTime(t, "2026-01-02T11:00:00Z"), End: mustParseTime(t, "2026-01-02T12:00:00Z"), ParentID: &parentID, RecurrenceID: &recurrenceID}, 0); err != nil {
		t.Fatalf("create override: %v", err)
	}

	if err := repo.DeleteChildrenOf(ctx, "master"); err != nil {
		t.Fatalf("delete children: %v", err)
	}

	if _, err := repo.GetByID(ctx, "override"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected the override to be deleted, got %v", err)
	}
	if _, err := repo.GetByID(ctx, "master"); err != nil {
		t.Fatalf("expected the master to survive, got %v", err)
	}
}

// ListAllWithReminders is the firing engine's read path (ADR-0021): it spans
// every Calendar, unlike ListByCalendarIDs, only returns events that
// actually carry a Reminder, and pairs each with its Calendar's Owner.
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
	calA, err := calendars.Create(ctx, userA.ID, "cal-a", CalendarFields{Name: "A", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar a: %v", err)
	}
	calB, err := calendars.Create(ctx, userB.ID, "cal-b", CalendarFields{Name: "B", Color: "tomato"})
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

	ownerByID := make(map[string]int64, len(events))
	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.ID
		ownerByID[e.ID] = e.CalendarOwnerID
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 events with reminders across both users, got %v", ids)
	}
	if ownerByID["with-reminder"] != userA.ID {
		t.Fatalf("expected with-reminder's owner to be user a, got %d", ownerByID["with-reminder"])
	}
	if ownerByID["other-users-reminder"] != userB.ID {
		t.Fatalf("expected other-users-reminder's owner to be user b, got %d", ownerByID["other-users-reminder"])
	}
}

func mustCreateEvent(t *testing.T, repo *EventRepository, id string, userID int64, calendarID, start, end string) {
	t.Helper()
	if _, err := repo.Create(context.Background(), id, &userID, EventFields{CalendarID: calendarID, Title: id, Start: mustParseTime(t, start), End: mustParseTime(t, end)}, 0); err != nil {
		t.Fatalf("create event %q: %v", id, err)
	}
}

func mustCreateRecurringEvent(t *testing.T, repo *EventRepository, id string, userID int64, calendarID, start, end, rrule string) {
	t.Helper()
	if _, err := repo.Create(context.Background(), id, &userID, EventFields{CalendarID: calendarID, Title: id, Start: mustParseTime(t, start), End: mustParseTime(t, end), Rrule: rrule}, 0); err != nil {
		t.Fatalf("create recurring event %q: %v", id, err)
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
