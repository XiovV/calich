package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// newTestEventService returns an EventService plus a real user id and
// calendar id to satisfy events' foreign keys.
func newTestEventService(t *testing.T) (svc *EventService, userID int64, calendarID string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	user, err := users.Create(context.Background(), "user-a", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	calendarRepo := repository.NewCalendarRepository(sqlDB)
	cal, err := calendarRepo.Create(context.Background(), user.ID, "cal-1", repository.CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	return NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), NewCalendarService(calendarRepo, repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB))), user.ID, cal.ID
}

func TestEventService_Create(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	event, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if event.Title != "Standup" {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func TestEventService_Create_RoundTripsRrule(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	rrule := "FREQ=WEEKLY;BYDAY=TH"
	created, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: rrule})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Rrule != rrule {
		t.Fatalf("expected rrule %q, got %q", rrule, created.Rrule)
	}

	fetched, err := svc.Get(ctx, userID, "evt-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Rrule != rrule {
		t.Fatalf("expected fetched rrule %q, got %q", rrule, fetched.Rrule)
	}
}

func TestEventService_Create_RoundTripsAllDay(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	created, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Holiday", Start: start, End: end, AllDay: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !created.AllDay {
		t.Fatalf("expected created event to be all-day, got %+v", created)
	}

	updated, err := svc.Update(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Holiday", Start: start, End: end})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.AllDay {
		t.Fatalf("expected updated event to no longer be all-day, got %+v", updated)
	}
}

// A nil tzid (Floating Event, ADR-0019) round-trips as nil, and a named
// zone passed to Create/Update round-trips verbatim.
func TestEventService_RoundTripsTzid(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	created, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Tzid != nil {
		t.Fatalf("expected nil tzid, got %+v", created)
	}

	zone := "Europe/Berlin"
	updated, err := svc.Update(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Tzid: &zone})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Tzid == nil || *updated.Tzid != zone {
		t.Fatalf("expected tzid %q, got %+v", zone, updated)
	}
}

func TestEventService_Create_RejectsMalformedRrule(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "not a rule"})
	if !errors.Is(err, ErrInvalidRecurrenceRule) {
		t.Fatalf("expected ErrInvalidRecurrenceRule, got %v", err)
	}
}

func TestEventService_Update_RoundTripsRrule(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create: %v", err)
	}

	rrule := "FREQ=DAILY"
	updated, err := svc.Update(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: rrule})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Rrule != rrule {
		t.Fatalf("expected rrule %q, got %q", rrule, updated.Rrule)
	}
}

func TestEventService_Create_RejectsEmptyTitle(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "   ", Start: start, End: end})
	if !errors.Is(err, ErrInvalidTitle) {
		t.Fatalf("expected ErrInvalidTitle, got %v", err)
	}
}

func TestEventService_Create_RejectsEndNotAfterStart(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("expected ErrInvalidTimeRange, got %v", err)
	}
}

func TestEventService_Create_RejectsEqualStartAndEnd(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	at := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: at, End: at})
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("expected ErrInvalidTimeRange, got %v", err)
	}
}

func TestEventService_Create_RejectsUnknownCalendar(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", EventWrite{CalendarID: "does-not-exist", Title: "Standup", Start: start, End: end})
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, got %v", err)
	}
}

func TestEventService_Create_RejectsAnotherUsersCalendar(t *testing.T) {
	svc, _, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), 99999, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound for another user's calendar, got %v", err)
	}
}

func TestEventService_List(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create: %v", err)
	}

	events, err := svc.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func TestEventService_Update(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create: %v", err)
	}

	newStart := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)
	newEnd := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	updated, err := svc.Update(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Renamed", Start: newStart, End: newEnd})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != "Renamed" {
		t.Fatalf("unexpected event: %+v", updated)
	}
}

func TestEventService_Update_RejectsEndNotAfterStart(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: end, End: start})
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("expected ErrInvalidTimeRange, got %v", err)
	}
}

func TestEventService_Update_NotFound(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Update(context.Background(), userID, "nope", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEventService_Update_RejectsUnknownCalendar(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, userID, "evt-1", EventWrite{CalendarID: "does-not-exist", Title: "Standup", Start: start, End: end})
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, got %v", err)
	}
}

func TestEventService_Update_RejectsAnotherUsersCalendar(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, 99999, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, since the calendar ownership check runs before the event lookup, got %v", err)
	}
}

func TestEventService_Delete(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.Delete(ctx, userID, "evt-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	events, err := svc.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events after delete, got %d", len(events))
	}
}

func TestEventService_Delete_NotFound(t *testing.T) {
	svc, userID, _ := newTestEventService(t)

	err := svc.Delete(context.Background(), userID, "nope")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// Under ADR-0034, an Event's authorization resolves through its Calendar
// rather than a user_id column of its own — Get, Update, and Delete on an
// id that names a real Event, but one in a Calendar the caller doesn't own,
// must report the same repository.ErrNotFound a nonexistent id would, not
// leak that the Event exists.
func TestEventService_Get_AnotherUsersEventIsNotFound(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := svc.Get(ctx, 99999, "evt-1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound reading another user's event, got %v", err)
	}
}

func TestEventService_Update_AnotherUsersEventIsNotFound(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// The attacker owns a real Calendar of their own (so write.CalendarID
	// resolves and passes the writability guard), and tries to "move" the
	// victim's event id into it. Update still has to resolve id itself before
	// touching it, and that lookup finds it in the victim's Calendar, which
	// the attacker has no Access to — exercising getOwnedEvent's guard
	// specifically, distinct from Update_RejectsAnotherUsersCalendar's
	// earlier write.CalendarID check.
	attacker, err := repository.NewUserRepository(svc.db).Create(ctx, "attacker", "hash", false)
	if err != nil {
		t.Fatalf("create attacker: %v", err)
	}
	attackerCalendar, err := repository.NewCalendarRepository(svc.db).Create(ctx, attacker.ID, "attacker-cal", repository.CalendarFields{Name: "Mine", Color: "peacock"})
	if err != nil {
		t.Fatalf("create attacker calendar: %v", err)
	}

	if _, err := svc.Update(ctx, attacker.ID, "evt-1", EventWrite{CalendarID: attackerCalendar.ID, Title: "Renamed", Start: start, End: end}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound updating another user's event, got %v", err)
	}
}

// CalendarCTag is the one EventService method that never took a userID
// before ADR-0034 — calendarID was an unguessable UUID belonging to the
// only user, so nothing else checked it either. It must refuse another
// user's Calendar now, the same way every other method does.
func TestEventService_CalendarCTag_RejectsAnotherUsersCalendar(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := svc.CalendarCTag(ctx, 99999, calendarID); !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound for another user's calendar, got %v", err)
	}
}

func TestEventService_Delete_AnotherUsersEventIsNotFound(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.Delete(ctx, 99999, "evt-1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting another user's event, got %v", err)
	}

	if _, err := svc.Get(ctx, userID, "evt-1"); err != nil {
		t.Fatalf("expected the event to still exist for its real owner, got %v", err)
	}
}

func TestEventService_CreateOverride(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), ParentID: &master.ID, RecurrenceID: &recurrenceID})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}
	if override.ParentID == nil || *override.ParentID != master.ID {
		t.Fatalf("expected override parentId %q, got %+v", master.ID, override)
	}
	if override.RecurrenceID == nil || !override.RecurrenceID.Equal(recurrenceID) {
		t.Fatalf("expected override recurrenceId %v, got %+v", recurrenceID, override)
	}
}

func TestEventService_CreateOverride_RejectsOwnRrule(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	_, err = svc.Create(ctx, userID, "override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: start, End: end, Rrule: "FREQ=DAILY", ParentID: &master.ID, RecurrenceID: &recurrenceID})
	if !errors.Is(err, ErrInvalidOverride) {
		t.Fatalf("expected ErrInvalidOverride, got %v", err)
	}
}

func TestEventService_CreateOverride_RejectsOverridingAnOverride(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), ParentID: &master.ID, RecurrenceID: &recurrenceID})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	anotherRecurrenceID := time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)
	_, err = svc.Create(ctx, userID, "double-override", EventWrite{CalendarID: calendarID, Title: "Nope", Start: start, End: end, ParentID: &override.ID, RecurrenceID: &anotherRecurrenceID})
	if !errors.Is(err, ErrParentIsOverride) {
		t.Fatalf("expected ErrParentIsOverride, got %v", err)
	}
}

func TestEventService_AddException(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	occurrenceStart := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	if err := svc.AddException(ctx, userID, master.ID, occurrenceStart); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	fetched, err := svc.Get(ctx, userID, master.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(fetched.Exdates) != 1 || !fetched.Exdates[0].Equal(occurrenceStart) {
		t.Fatalf("expected the master to carry the exdate, got %+v", fetched)
	}
}

func TestEventService_AddException_RejectsNonRecurringParent(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	err = svc.AddException(ctx, userID, master.ID, start)
	if !errors.Is(err, ErrParentNotRecurring) {
		t.Fatalf("expected ErrParentNotRecurring, got %v", err)
	}
}

func TestEventService_AddException_NotFound(t *testing.T) {
	svc, userID, _ := newTestEventService(t)

	err := svc.AddException(context.Background(), userID, "nope", time.Now())
	if !errors.Is(err, ErrParentNotFound) {
		t.Fatalf("expected ErrParentNotFound, got %v", err)
	}
}

func TestEventService_UpdateOverride_RejectsOwnRrule(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), ParentID: &master.ID, RecurrenceID: &recurrenceID})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	_, err = svc.Update(ctx, userID, override.ID, EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: start, End: end, Rrule: "FREQ=DAILY"})
	if !errors.Is(err, ErrInvalidOverride) {
		t.Fatalf("expected ErrInvalidOverride, got %v", err)
	}
}

func TestEventService_Update_DiscardsChildrenOnRuleChange(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), ParentID: &master.ID, RecurrenceID: &recurrenceID})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}
	if err := svc.AddException(ctx, userID, master.ID, time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	if _, err := svc.Update(ctx, userID, master.ID, EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY;BYDAY=TH"}); err != nil {
		t.Fatalf("update: %v", err)
	}

	if _, err := svc.Get(ctx, userID, override.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the override to be discarded, got %v", err)
	}

	fetched, err := svc.Get(ctx, userID, master.ID)
	if err != nil {
		t.Fatalf("get master: %v", err)
	}
	if len(fetched.Exdates) != 0 {
		t.Fatalf("expected exceptions to be discarded, got %+v", fetched.Exdates)
	}
}

func TestEventService_Update_KeepsChildrenWhenRuleUnchanged(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), ParentID: &master.ID, RecurrenceID: &recurrenceID})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	if _, err := svc.Update(ctx, userID, master.ID, EventWrite{CalendarID: calendarID, Title: "Renamed", Start: start, End: end, Rrule: "FREQ=DAILY"}); err != nil {
		t.Fatalf("update: %v", err)
	}

	if _, err := svc.Get(ctx, userID, override.ID); err != nil {
		t.Fatalf("expected the override to survive an unchanged rule, got %v", err)
	}
}

// A "this and following" split truncates the old master with UNTIL, which
// changes the rrule string but not its repetition pattern — this must not
// trip the rule-change discard, or overrides/exceptions before the split
// boundary would be wrongly wiped out before the caller ever reparents them
// (ADR-0016).
func TestEventService_Update_KeepsChildrenWhenOnlyUntilChanges(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), ParentID: &master.ID, RecurrenceID: &recurrenceID})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}
	if err := svc.AddException(ctx, userID, master.ID, time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	if _, err := svc.Update(ctx, userID, master.ID, EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=DAILY;UNTIL=20260101T085959Z"}); err != nil {
		t.Fatalf("update: %v", err)
	}

	if _, err := svc.Get(ctx, userID, override.ID); err != nil {
		t.Fatalf("expected the override to survive a same-pattern UNTIL truncation, got %v", err)
	}
	fetched, err := svc.Get(ctx, userID, master.ID)
	if err != nil {
		t.Fatalf("get master: %v", err)
	}
	if len(fetched.Exdates) != 1 {
		t.Fatalf("expected the exception to survive a same-pattern UNTIL truncation, got %+v", fetched.Exdates)
	}
}

// TestEventService_Update_DiscardOnRuleChangeIsAtomic asserts that when the
// discard half of a pattern-changing Update fails, the Master's rule update
// is rolled back too — never new-rule-with-stale-children (ADR-0018). Unlike
// TestEventService_ReparentFrom_Atomic, the discard writes are plain DELETEs
// with no natural collision to trip through the public API, so the failure
// here is forced with a poison trigger instead.
func TestEventService_Update_DiscardOnRuleChangeIsAtomic(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), ParentID: &master.ID, RecurrenceID: &recurrenceID})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}
	if err := svc.AddException(ctx, userID, master.ID, time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	if _, err := svc.db.ExecContext(ctx,
		`CREATE TRIGGER poison_override_delete BEFORE DELETE ON events WHEN OLD.id = 'override' BEGIN SELECT RAISE(ABORT, 'boom'); END`,
	); err != nil {
		t.Fatalf("install poison trigger: %v", err)
	}

	if _, err := svc.Update(ctx, userID, master.ID, EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY;BYDAY=TH"}); err == nil {
		t.Fatalf("expected update to fail once the override delete is poisoned")
	}

	fetchedMaster, err := svc.Get(ctx, userID, master.ID)
	if err != nil {
		t.Fatalf("get master: %v", err)
	}
	if fetchedMaster.Rrule != "FREQ=DAILY" {
		t.Fatalf("expected master's rrule to be rolled back, got %q", fetchedMaster.Rrule)
	}
	if len(fetchedMaster.Exdates) != 1 {
		t.Fatalf("expected the exception to survive the rollback, got %+v", fetchedMaster.Exdates)
	}
	if _, err := svc.Get(ctx, userID, override.ID); err != nil {
		t.Fatalf("expected the override to survive the rollback, got %v", err)
	}
}

func TestEventService_ReparentFrom(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	oldMaster, err := svc.Create(ctx, userID, "old-master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC), Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("create old master: %v", err)
	}
	newMaster, err := svc.Create(ctx, userID, "new-master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 5, 9, 30, 0, 0, time.UTC), Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("create new master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 5, 10, 30, 0, 0, time.UTC), ParentID: &oldMaster.ID, RecurrenceID: &recurrenceID})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	exceptionStart := time.Date(2026, 1, 6, 9, 0, 0, 0, time.UTC)
	if err := svc.AddException(ctx, userID, oldMaster.ID, exceptionStart); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	if err := svc.ReparentFrom(ctx, userID, oldMaster.ID, newMaster.ID, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("reparent: %v", err)
	}

	fetched, err := svc.Get(ctx, userID, override.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.ParentID == nil || *fetched.ParentID != newMaster.ID {
		t.Fatalf("expected override reparented to %q, got %+v", newMaster.ID, fetched)
	}

	fetchedOldMaster, err := svc.Get(ctx, userID, oldMaster.ID)
	if err != nil {
		t.Fatalf("get old master: %v", err)
	}
	if len(fetchedOldMaster.Exdates) != 0 {
		t.Fatalf("expected old master to have no exceptions left, got %+v", fetchedOldMaster.Exdates)
	}

	fetchedNewMaster, err := svc.Get(ctx, userID, newMaster.ID)
	if err != nil {
		t.Fatalf("get new master: %v", err)
	}
	if len(fetchedNewMaster.Exdates) != 1 || !fetchedNewMaster.Exdates[0].Equal(exceptionStart) {
		t.Fatalf("expected exception reparented to %q, got %+v", newMaster.ID, fetchedNewMaster.Exdates)
	}
}

// TestEventService_ReparentFrom_Atomic asserts that when the Exceptions half
// of the write fails, the Overrides half is not left half-reparented — the
// two writes commit or roll back together (ADR-0018). The failure is forced
// by giving the new master an Exception at the same occurrence the old
// master's Exception would move to, tripping the (parent_id,
// occurrence_start) primary key when the UPDATE runs.
func TestEventService_ReparentFrom_Atomic(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	oldMaster, err := svc.Create(ctx, userID, "old-master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC), Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("create old master: %v", err)
	}
	newMaster, err := svc.Create(ctx, userID, "new-master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 5, 9, 30, 0, 0, time.UTC), Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("create new master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 5, 10, 30, 0, 0, time.UTC), ParentID: &oldMaster.ID, RecurrenceID: &recurrenceID})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	exceptionStart := time.Date(2026, 1, 6, 9, 0, 0, 0, time.UTC)
	if err := svc.AddException(ctx, userID, oldMaster.ID, exceptionStart); err != nil {
		t.Fatalf("add exception to old master: %v", err)
	}
	// A colliding exception already on the new master: reparenting the old
	// master's exception onto it violates the (parent_id, occurrence_start)
	// primary key.
	if err := svc.AddException(ctx, userID, newMaster.ID, exceptionStart); err != nil {
		t.Fatalf("add exception to new master: %v", err)
	}

	if err := svc.ReparentFrom(ctx, userID, oldMaster.ID, newMaster.ID, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatalf("expected reparent to fail on the colliding exception")
	}

	fetchedOverride, err := svc.Get(ctx, userID, override.ID)
	if err != nil {
		t.Fatalf("get override: %v", err)
	}
	if fetchedOverride.ParentID == nil || *fetchedOverride.ParentID != oldMaster.ID {
		t.Fatalf("expected override to remain on old master after rollback, got %+v", fetchedOverride)
	}

	fetchedOldMaster, err := svc.Get(ctx, userID, oldMaster.ID)
	if err != nil {
		t.Fatalf("get old master: %v", err)
	}
	if len(fetchedOldMaster.Exdates) != 1 || !fetchedOldMaster.Exdates[0].Equal(exceptionStart) {
		t.Fatalf("expected old master to keep its exception after rollback, got %+v", fetchedOldMaster.Exdates)
	}
}

func TestEventService_ReparentFrom_NotFound(t *testing.T) {
	svc, userID, _ := newTestEventService(t)

	err := svc.ReparentFrom(context.Background(), userID, "nope", "also-nope", time.Now())
	if !errors.Is(err, ErrParentNotFound) {
		t.Fatalf("expected ErrParentNotFound, got %v", err)
	}
}

func TestEventService_Create_PersistsAndRoundTripsReminders(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	reminders := []repository.Reminder{
		{OffsetMinutes: 10, Channel: "notification"},
		{OffsetMinutes: 1440, Channel: "email"},
	}
	created, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Reminders: reminders})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(created.Reminders) != 2 {
		t.Fatalf("expected 2 reminders on create response, got %+v", created.Reminders)
	}

	fetched, err := svc.Get(ctx, userID, "evt-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(fetched.Reminders) != 2 {
		t.Fatalf("expected 2 reminders on get, got %+v", fetched.Reminders)
	}

	listed, err := svc.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || len(listed[0].Reminders) != 2 {
		t.Fatalf("expected 2 reminders on list, got %+v", listed)
	}
}

func TestEventService_Create_NoReminders(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	created, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(created.Reminders) != 0 {
		t.Fatalf("expected no reminders, got %+v", created.Reminders)
	}
}

func TestEventService_Create_RejectsInvalidReminderChannel(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Reminders: []repository.Reminder{{OffsetMinutes: 10, Channel: "sms"}}})
	if !errors.Is(err, ErrInvalidReminderChannel) {
		t.Fatalf("expected ErrInvalidReminderChannel, got %v", err)
	}
}

// Update replaces an Event's Reminders set wholesale — it doesn't merge into
// the existing set (ADR-0020).
func TestEventService_Update_ReplacesRemindersWholesale(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Reminders: []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := svc.Update(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Reminders: []repository.Reminder{{OffsetMinutes: 30, Channel: "email"}}})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(updated.Reminders) != 1 || updated.Reminders[0].OffsetMinutes != 30 || updated.Reminders[0].Channel != "email" {
		t.Fatalf("expected reminders replaced wholesale, got %+v", updated.Reminders)
	}
}

// Update with an empty Reminders slice clears an Event's Reminders.
func TestEventService_Update_EmptyRemindersClears(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Reminders: []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := svc.Update(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(updated.Reminders) != 0 {
		t.Fatalf("expected reminders cleared, got %+v", updated.Reminders)
	}
}

func TestEventService_Update_RejectsInvalidReminderChannel(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Reminders: []repository.Reminder{{OffsetMinutes: 10, Channel: "sms"}}})
	if !errors.Is(err, ErrInvalidReminderChannel) {
		t.Fatalf("expected ErrInvalidReminderChannel, got %v", err)
	}
}

// Deleting an Event removes its Reminders (ON DELETE CASCADE, ADR-0020).
func TestEventService_Delete_CascadesReminders(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Reminders: []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.Delete(ctx, userID, "evt-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	byEvent, err := svc.reminders.ListByEventIDs(ctx, []string{"evt-1"})
	if err != nil {
		t.Fatalf("list by event ids: %v", err)
	}
	if len(byEvent["evt-1"]) != 0 {
		t.Fatalf("expected reminders to be cascade-deleted with their event, got %+v", byEvent["evt-1"])
	}
}

// TestEventService_Update_ReminderReplaceIsAtomic asserts that when the
// Reminders replace fails mid-transaction, the rest of the Update (the
// Event's own field changes) is rolled back too (ADR-0018).
func TestEventService_Update_ReminderReplaceIsAtomic(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := svc.db.ExecContext(ctx,
		`CREATE TRIGGER poison_reminder_insert BEFORE INSERT ON event_reminders WHEN NEW.event_id = 'evt-1' BEGIN SELECT RAISE(ABORT, 'boom'); END`,
	); err != nil {
		t.Fatalf("install poison trigger: %v", err)
	}

	if _, err := svc.Update(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Renamed", Start: start, End: end, Reminders: []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}}); err == nil {
		t.Fatalf("expected update to fail once the reminder insert is poisoned")
	}

	fetched, err := svc.Get(ctx, userID, "evt-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Title != "Standup" {
		t.Fatalf("expected title to be rolled back, got %q", fetched.Title)
	}
}

func TestEventService_GetSeries_MasterWithOverrideAndException(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY;BYDAY=TU", Reminders: []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := start.AddDate(0, 0, 7)
	if _, err := svc.Create(ctx, userID, "evt-1-override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: recurrenceID.Add(2 * time.Hour), End: recurrenceID.Add(2*time.Hour + 30*time.Minute), ParentID: &master.ID, RecurrenceID: &recurrenceID, Reminders: []repository.Reminder{{OffsetMinutes: 5, Channel: "email"}}}); err != nil {
		t.Fatalf("create override: %v", err)
	}

	exdate := start.AddDate(0, 0, 14)
	if err := svc.AddException(ctx, userID, master.ID, exdate); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	gotMaster, overrides, err := svc.GetSeries(ctx, userID, master.ID)
	if err != nil {
		t.Fatalf("get series: %v", err)
	}
	if len(gotMaster.Reminders) != 1 || gotMaster.Reminders[0].OffsetMinutes != 10 {
		t.Fatalf("expected the master's own reminder, got %+v", gotMaster.Reminders)
	}
	if len(gotMaster.Exdates) != 1 || !gotMaster.Exdates[0].Equal(exdate) {
		t.Fatalf("expected the master's exdate, got %+v", gotMaster.Exdates)
	}
	if len(overrides) != 1 {
		t.Fatalf("expected one override, got %d", len(overrides))
	}
	if overrides[0].ID != "evt-1-override" {
		t.Fatalf("expected override evt-1-override, got %q", overrides[0].ID)
	}
	if len(overrides[0].Reminders) != 1 || overrides[0].Reminders[0].Channel != "email" {
		t.Fatalf("expected the override's own reminder, got %+v", overrides[0].Reminders)
	}
}

func TestEventService_GetSeries_RejectsOverrideID(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := start.AddDate(0, 0, 7)
	if _, err := svc.Create(ctx, userID, "evt-1-override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: recurrenceID, End: recurrenceID.Add(30 * time.Minute), ParentID: &master.ID, RecurrenceID: &recurrenceID}); err != nil {
		t.Fatalf("create override: %v", err)
	}

	if _, _, err := svc.GetSeries(ctx, userID, "evt-1-override"); !errors.Is(err, ErrParentIsOverride) {
		t.Fatalf("expected ErrParentIsOverride, got %v", err)
	}
}

func TestEventService_ListSeriesByCalendar_GroupsOverridesByMaster(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	standaloneStart := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	if _, err := svc.Create(ctx, userID, "evt-standalone", EventWrite{CalendarID: calendarID, Title: "One-off", Start: standaloneStart, End: standaloneStart.Add(time.Hour)}); err != nil {
		t.Fatalf("create standalone: %v", err)
	}

	masterStart := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	master, err := svc.Create(ctx, userID, "evt-master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: masterStart, End: masterStart.Add(30 * time.Minute), Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := masterStart.AddDate(0, 0, 7)
	if _, err := svc.Create(ctx, userID, "evt-override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: recurrenceID.Add(time.Hour), End: recurrenceID.Add(90 * time.Minute), ParentID: &master.ID, RecurrenceID: &recurrenceID}); err != nil {
		t.Fatalf("create override: %v", err)
	}

	masters, overridesByParent, err := svc.ListSeriesByCalendar(ctx, userID, calendarID)
	if err != nil {
		t.Fatalf("list series by calendar: %v", err)
	}

	if len(masters) != 2 {
		t.Fatalf("expected two masters (the standalone event and the series master), got %d", len(masters))
	}
	if len(overridesByParent["evt-master"]) != 1 || overridesByParent["evt-master"][0].ID != "evt-override" {
		t.Fatalf("expected evt-master's override, got %+v", overridesByParent["evt-master"])
	}
	if len(overridesByParent["evt-standalone"]) != 0 {
		t.Fatalf("expected the standalone event to have no overrides, got %+v", overridesByParent["evt-standalone"])
	}
}

// Both read paths that recompose whole series resolve Reminders for the
// Masters and every Override in a single batched lookup. Each row must come
// back carrying its own Reminders — a Master with none must not inherit its
// Override's, and vice versa.
func TestEventService_SeriesReadPaths_AttachRemindersPerRow(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	// A Master with no Reminders, ordered first, so an off-by-one in the
	// batched attach would show up as it acquiring someone else's.
	quietStart := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	if _, err := svc.Create(ctx, userID, "evt-quiet", EventWrite{
		CalendarID: calendarID, Title: "One-off",
		Start: quietStart, End: quietStart.Add(time.Hour),
	}); err != nil {
		t.Fatalf("create quiet master: %v", err)
	}

	masterStart := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	master, err := svc.Create(ctx, userID, "evt-master", EventWrite{
		CalendarID: calendarID, Title: "Standup",
		Start: masterStart, End: masterStart.Add(30 * time.Minute),
		Rrule:     "FREQ=WEEKLY;BYDAY=TU",
		Reminders: []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}},
	})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := masterStart.AddDate(0, 0, 7)
	if _, err := svc.Create(ctx, userID, "evt-override", EventWrite{
		CalendarID: calendarID, Title: "Standup (moved)",
		Start: recurrenceID.Add(time.Hour), End: recurrenceID.Add(90 * time.Minute),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
		Reminders: []repository.Reminder{{OffsetMinutes: 5, Channel: "email"}},
	}); err != nil {
		t.Fatalf("create override: %v", err)
	}

	assertReminders := func(t *testing.T, masters []repository.Event, overridesByParent map[string][]repository.Event) {
		t.Helper()

		byID := map[string]repository.Event{}
		for _, m := range masters {
			byID[m.ID] = m
		}
		if got := byID["evt-quiet"].Reminders; len(got) != 0 {
			t.Fatalf("expected the reminderless master to stay reminderless, got %+v", got)
		}
		if got := byID["evt-master"].Reminders; len(got) != 1 || got[0].OffsetMinutes != 10 || got[0].Channel != "notification" {
			t.Fatalf("expected the master's own reminder, got %+v", got)
		}

		overrides := overridesByParent["evt-master"]
		if len(overrides) != 1 {
			t.Fatalf("expected one override, got %d", len(overrides))
		}
		if got := overrides[0].Reminders; len(got) != 1 || got[0].OffsetMinutes != 5 || got[0].Channel != "email" {
			t.Fatalf("expected the override's own reminder, got %+v", got)
		}
	}

	t.Run("ListSeriesByCalendar", func(t *testing.T) {
		masters, overridesByParent, err := svc.ListSeriesByCalendar(ctx, userID, calendarID)
		if err != nil {
			t.Fatalf("list series by calendar: %v", err)
		}
		assertReminders(t, masters, overridesByParent)
	})

	t.Run("SyncSince", func(t *testing.T) {
		result, err := svc.SyncSince(ctx, userID, calendarID, 0)
		if err != nil {
			t.Fatalf("sync since: %v", err)
		}
		assertReminders(t, result.Masters, result.OverridesByParent)
	})
}

// masterChangeSeq is a small helper for the change_seq tests below: it reads
// masterID's own change_seq back out of the repository, bypassing the
// service layer's Get (which doesn't expose it).
func masterChangeSeq(t *testing.T, svc *EventService, userID int64, masterID string) int64 {
	t.Helper()
	e, err := svc.events.GetByID(context.Background(), masterID)
	if err != nil {
		t.Fatalf("get master %q: %v", masterID, err)
	}
	return e.ChangeSeq
}

func TestEventService_Create_Override_BumpsParentChangeSeq(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	masterStart := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	master, err := svc.Create(ctx, userID, "evt-master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: masterStart, End: masterStart.Add(30 * time.Minute), Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	before := masterChangeSeq(t, svc, userID, master.ID)

	recurrenceID := masterStart.AddDate(0, 0, 7)
	if _, err := svc.Create(ctx, userID, "evt-override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: recurrenceID.Add(time.Hour), End: recurrenceID.Add(90 * time.Minute), ParentID: &master.ID, RecurrenceID: &recurrenceID}); err != nil {
		t.Fatalf("create override: %v", err)
	}

	after := masterChangeSeq(t, svc, userID, master.ID)
	if after <= before {
		t.Fatalf("expected the master's change_seq to bump after creating an override, was %d, now %d", before, after)
	}
}

func TestEventService_Update_Override_BumpsParentChangeSeq(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	masterStart := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	master, err := svc.Create(ctx, userID, "evt-master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: masterStart, End: masterStart.Add(30 * time.Minute), Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := masterStart.AddDate(0, 0, 7)
	override, err := svc.Create(ctx, userID, "evt-override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: recurrenceID.Add(time.Hour), End: recurrenceID.Add(90 * time.Minute), ParentID: &master.ID, RecurrenceID: &recurrenceID})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}
	before := masterChangeSeq(t, svc, userID, master.ID)

	if _, err := svc.Update(ctx, userID, override.ID, EventWrite{CalendarID: calendarID, Title: "Standup (moved again)", Start: override.Start, End: override.End}); err != nil {
		t.Fatalf("update override: %v", err)
	}

	after := masterChangeSeq(t, svc, userID, master.ID)
	if after <= before {
		t.Fatalf("expected the master's change_seq to bump after updating an override, was %d, now %d", before, after)
	}
}

func TestEventService_Delete_Override_BumpsParentChangeSeq(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	masterStart := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	master, err := svc.Create(ctx, userID, "evt-master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: masterStart, End: masterStart.Add(30 * time.Minute), Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := masterStart.AddDate(0, 0, 7)
	override, err := svc.Create(ctx, userID, "evt-override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: recurrenceID.Add(time.Hour), End: recurrenceID.Add(90 * time.Minute), ParentID: &master.ID, RecurrenceID: &recurrenceID})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}
	before := masterChangeSeq(t, svc, userID, master.ID)

	if err := svc.Delete(ctx, userID, override.ID); err != nil {
		t.Fatalf("delete override: %v", err)
	}

	after := masterChangeSeq(t, svc, userID, master.ID)
	if after <= before {
		t.Fatalf("expected the master's change_seq to bump after deleting an override, was %d, now %d", before, after)
	}
}

func TestEventService_Delete_Master_WritesTombstoneInsteadOfBumpingItself(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	beforeCTag, err := svc.CalendarCTag(ctx, userID, calendarID)
	if err != nil {
		t.Fatalf("ctag: %v", err)
	}

	if err := svc.Delete(ctx, userID, master.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	afterCTag, err := svc.CalendarCTag(ctx, userID, calendarID)
	if err != nil {
		t.Fatalf("ctag: %v", err)
	}
	if afterCTag <= beforeCTag {
		t.Fatalf("expected the calendar's CTag to bump after a delete, was %d, now %d", beforeCTag, afterCTag)
	}

	result, err := svc.SyncSince(ctx, userID, calendarID, beforeCTag)
	if err != nil {
		t.Fatalf("sync since: %v", err)
	}
	if len(result.DeletedUIDs) != 1 || result.DeletedUIDs[0] != master.ID {
		t.Fatalf("expected the deleted master's id to be reported as a tombstone, got %+v", result.DeletedUIDs)
	}
}

func TestEventService_AddException_BumpsParentChangeSeq(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	masterStart := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	master, err := svc.Create(ctx, userID, "evt-master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: masterStart, End: masterStart.Add(30 * time.Minute), Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	before := masterChangeSeq(t, svc, userID, master.ID)

	if err := svc.AddException(ctx, userID, master.ID, masterStart.AddDate(0, 0, 7)); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	after := masterChangeSeq(t, svc, userID, master.ID)
	if after <= before {
		t.Fatalf("expected the master's change_seq to bump after adding an exception, was %d, now %d", before, after)
	}
}

func TestEventService_ReparentFrom_BumpsBothParentsChangeSeq(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	masterStart := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	oldParent, err := svc.Create(ctx, userID, "evt-old", EventWrite{CalendarID: calendarID, Title: "Standup", Start: masterStart, End: masterStart.Add(30 * time.Minute), Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create old parent: %v", err)
	}
	newParentStart := masterStart.AddDate(0, 0, 14)
	newParent, err := svc.Create(ctx, userID, "evt-new", EventWrite{CalendarID: calendarID, Title: "Standup", Start: newParentStart, End: newParentStart.Add(30 * time.Minute), Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create new parent: %v", err)
	}
	beforeOld := masterChangeSeq(t, svc, userID, oldParent.ID)
	beforeNew := masterChangeSeq(t, svc, userID, newParent.ID)

	if err := svc.ReparentFrom(ctx, userID, oldParent.ID, newParent.ID, newParentStart); err != nil {
		t.Fatalf("reparent from: %v", err)
	}

	if got := masterChangeSeq(t, svc, userID, oldParent.ID); got <= beforeOld {
		t.Fatalf("expected the old parent's change_seq to bump, was %d, now %d", beforeOld, got)
	}
	if got := masterChangeSeq(t, svc, userID, newParent.ID); got <= beforeNew {
		t.Fatalf("expected the new parent's change_seq to bump, was %d, now %d", beforeNew, got)
	}
}

func TestEventService_PutSeries_CreatesNewMaster(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	master, overrides, err := svc.PutSeries(ctx, userID, calendarID, "client-uid-1", SeriesWrite{
		Title: "Standup", Start: start, End: end,
	})
	if err != nil {
		t.Fatalf("put series: %v", err)
	}
	if master.ID != "client-uid-1" {
		t.Fatalf("expected the client-authored UID to become the master's id, got %q", master.ID)
	}
	if master.Title != "Standup" {
		t.Fatalf("unexpected master: %+v", master)
	}
	if len(overrides) != 0 {
		t.Fatalf("expected no overrides, got %d", len(overrides))
	}

	fetched, err := svc.Get(ctx, userID, "client-uid-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Title != "Standup" {
		t.Fatalf("expected a subsequent GET to see the created master, got %+v", fetched)
	}
}

func TestEventService_PutSeries_UpdatesExistingMaster(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	if _, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create: %v", err)
	}

	master, _, err := svc.PutSeries(ctx, userID, calendarID, "evt-1", SeriesWrite{
		Title: "Standup (renamed)", Start: start, End: end,
	})
	if err != nil {
		t.Fatalf("put series: %v", err)
	}
	if master.Title != "Standup (renamed)" {
		t.Fatalf("expected the update to take effect, got %+v", master)
	}
}

func TestEventService_PutSeries_CreatesOverride(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	masterStart := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	masterEnd := masterStart.Add(30 * time.Minute)
	if _, err := svc.Create(ctx, userID, "evt-master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: masterStart, End: masterEnd, Rrule: "FREQ=WEEKLY;BYDAY=TU"}); err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := masterStart.AddDate(0, 0, 7)
	overrideStart := recurrenceID.Add(2 * time.Hour)
	_, overrides, err := svc.PutSeries(ctx, userID, calendarID, "evt-master", SeriesWrite{
		Title: "Standup", Start: masterStart, End: masterEnd, Rrule: "FREQ=WEEKLY;BYDAY=TU",
		Overrides: []OverrideWrite{
			{RecurrenceID: recurrenceID, Title: "Standup (moved)", Start: overrideStart, End: overrideStart.Add(30 * time.Minute)},
		},
	})
	if err != nil {
		t.Fatalf("put series: %v", err)
	}
	if len(overrides) != 1 {
		t.Fatalf("expected exactly one override, got %d", len(overrides))
	}
	if overrides[0].Title != "Standup (moved)" {
		t.Fatalf("unexpected override: %+v", overrides[0])
	}
	if overrides[0].ParentID == nil || *overrides[0].ParentID != "evt-master" {
		t.Fatalf("expected the override to be parented to the master, got %+v", overrides[0])
	}

	// Other occurrences of the series are unaffected: the master's own
	// fields still describe the pattern.
	master, err := svc.Get(ctx, userID, "evt-master")
	if err != nil {
		t.Fatalf("get master: %v", err)
	}
	if master.Title != "Standup" || master.Rrule != "FREQ=WEEKLY;BYDAY=TU" {
		t.Fatalf("expected the master's series to be unchanged, got %+v", master)
	}
}

func TestEventService_PutSeries_UpdatesExistingOverride(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	masterStart := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	masterEnd := masterStart.Add(30 * time.Minute)
	master, err := svc.Create(ctx, userID, "evt-master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: masterStart, End: masterEnd, Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := masterStart.AddDate(0, 0, 7)
	override, err := svc.Create(ctx, userID, "evt-override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: recurrenceID.Add(2 * time.Hour), End: recurrenceID.Add(2*time.Hour + 30*time.Minute), ParentID: &master.ID, RecurrenceID: &recurrenceID})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	_, overrides, err := svc.PutSeries(ctx, userID, calendarID, "evt-master", SeriesWrite{
		Title: "Standup", Start: masterStart, End: masterEnd, Rrule: "FREQ=WEEKLY;BYDAY=TU",
		Overrides: []OverrideWrite{
			{RecurrenceID: recurrenceID, Title: "Standup (moved again)", Start: recurrenceID.Add(3 * time.Hour), End: recurrenceID.Add(3*time.Hour + 30*time.Minute)},
		},
	})
	if err != nil {
		t.Fatalf("put series: %v", err)
	}
	if len(overrides) != 1 {
		t.Fatalf("expected exactly one override, got %d", len(overrides))
	}
	if overrides[0].ID != override.ID {
		t.Fatalf("expected the existing override row to be updated in place, got a new id %q", overrides[0].ID)
	}
	if overrides[0].Title != "Standup (moved again)" {
		t.Fatalf("expected the override's title to be updated, got %+v", overrides[0])
	}
}

func TestEventService_PutSeries_RemovesOverrideAbsentFromWrite(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	masterStart := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	masterEnd := masterStart.Add(30 * time.Minute)
	master, err := svc.Create(ctx, userID, "evt-master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: masterStart, End: masterEnd, Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := masterStart.AddDate(0, 0, 7)
	if _, err := svc.Create(ctx, userID, "evt-override", EventWrite{CalendarID: calendarID, Title: "Standup (moved)", Start: recurrenceID.Add(2 * time.Hour), End: recurrenceID.Add(2*time.Hour + 30*time.Minute), ParentID: &master.ID, RecurrenceID: &recurrenceID}); err != nil {
		t.Fatalf("create override: %v", err)
	}

	_, overrides, err := svc.PutSeries(ctx, userID, calendarID, "evt-master", SeriesWrite{
		Title: "Standup", Start: masterStart, End: masterEnd, Rrule: "FREQ=WEEKLY;BYDAY=TU",
	})
	if err != nil {
		t.Fatalf("put series: %v", err)
	}
	if len(overrides) != 0 {
		t.Fatalf("expected the override to be removed when absent from the write, got %+v", overrides)
	}
}

func TestEventService_PutSeries_ReplacesExdatesWholesale(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	masterStart := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	masterEnd := masterStart.Add(30 * time.Minute)
	if _, err := svc.Create(ctx, userID, "evt-master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: masterStart, End: masterEnd, Rrule: "FREQ=WEEKLY;BYDAY=TU"}); err != nil {
		t.Fatalf("create master: %v", err)
	}
	if err := svc.AddException(ctx, userID, "evt-master", masterStart.AddDate(0, 0, 7)); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	newExdate := masterStart.AddDate(0, 0, 14)
	master, _, err := svc.PutSeries(ctx, userID, calendarID, "evt-master", SeriesWrite{
		Title: "Standup", Start: masterStart, End: masterEnd, Rrule: "FREQ=WEEKLY;BYDAY=TU",
		Exdates: []time.Time{newExdate},
	})
	if err != nil {
		t.Fatalf("put series: %v", err)
	}
	if len(master.Exdates) != 1 || !master.Exdates[0].Equal(newExdate) {
		t.Fatalf("expected exdates to be replaced wholesale, got %v", master.Exdates)
	}
}

func TestEventService_PutSeries_BumpsChangeSeq(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	if _, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end}); err != nil {
		t.Fatalf("create: %v", err)
	}
	before := masterChangeSeq(t, svc, userID, "evt-1")

	if _, _, err := svc.PutSeries(ctx, userID, calendarID, "evt-1", SeriesWrite{Title: "Standup (renamed)", Start: start, End: end}); err != nil {
		t.Fatalf("put series: %v", err)
	}

	after := masterChangeSeq(t, svc, userID, "evt-1")
	if after <= before {
		t.Fatalf("expected change_seq to bump, was %d, now %d", before, after)
	}
}

func TestEventService_PutSeries_RejectsInvalidTitle(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	_, _, err := svc.PutSeries(ctx, userID, calendarID, "evt-1", SeriesWrite{Title: "  ", Start: start, End: start.Add(time.Hour)})
	if !errors.Is(err, ErrInvalidTitle) {
		t.Fatalf("expected ErrInvalidTitle, got %v", err)
	}
}

func TestEventService_PutSeries_RejectsUnknownCalendar(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	ctx := context.Background()

	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	_, _, err := svc.PutSeries(ctx, userID, "does-not-exist", "evt-1", SeriesWrite{Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, got %v", err)
	}
}

func TestEventService_GetOccurrence_NonRecurring_ReturnsMasterFlattened(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	got, err := svc.GetOccurrence(ctx, userID, master.ID, start)
	if err != nil {
		t.Fatalf("get occurrence: %v", err)
	}
	if got.Title != "Standup" || !got.Start.Equal(start) || !got.End.Equal(end) {
		t.Fatalf("unexpected occurrence: %+v", got)
	}
	if got.Rrule != "" {
		t.Fatalf("expected a cleared rrule, got %q", got.Rrule)
	}
}

func TestEventService_GetOccurrence_NonRecurring_WrongStartIsNotFound(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	_, err = svc.GetOccurrence(ctx, userID, master.ID, start.Add(time.Hour))
	if !errors.Is(err, ErrOccurrenceNotFound) {
		t.Fatalf("expected ErrOccurrenceNotFound, got %v", err)
	}
}

func TestEventService_GetOccurrence_Recurring_ShiftsStartAndEndByDuration(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	occurrenceStart := start.AddDate(0, 0, 7)
	got, err := svc.GetOccurrence(ctx, userID, master.ID, occurrenceStart)
	if err != nil {
		t.Fatalf("get occurrence: %v", err)
	}
	wantEnd := occurrenceStart.Add(30 * time.Minute)
	if !got.Start.Equal(occurrenceStart) || !got.End.Equal(wantEnd) {
		t.Fatalf("expected start %v end %v, got start %v end %v", occurrenceStart, wantEnd, got.Start, got.End)
	}
	if got.Title != "Standup" {
		t.Fatalf("expected the master's own title, got %q", got.Title)
	}
}

func TestEventService_GetOccurrence_Recurring_ExcludedByExdateIsNotFound(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	exdate := start.AddDate(0, 0, 7)
	if err := svc.AddException(ctx, userID, master.ID, exdate); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	_, err = svc.GetOccurrence(ctx, userID, master.ID, exdate)
	if !errors.Is(err, ErrOccurrenceNotFound) {
		t.Fatalf("expected ErrOccurrenceNotFound, got %v", err)
	}
}

func TestEventService_GetOccurrence_OverriddenOccurrence_ReturnsOverrideFields(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := start.AddDate(0, 0, 7)
	overrideStart := recurrenceID.Add(2 * time.Hour)
	overrideEnd := recurrenceID.Add(2*time.Hour + 30*time.Minute)
	override, err := svc.Create(ctx, userID, "evt-1-override", EventWrite{
		CalendarID: calendarID, Title: "Standup (moved)", Start: overrideStart, End: overrideEnd,
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
	})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	got, err := svc.GetOccurrence(ctx, userID, master.ID, recurrenceID)
	if err != nil {
		t.Fatalf("get occurrence: %v", err)
	}
	if got.Title != "Standup (moved)" || !got.Start.Equal(overrideStart) || !got.End.Equal(overrideEnd) {
		t.Fatalf("expected the override's own fields, got %+v", got)
	}
	if got.ParentID != nil || got.RecurrenceID != nil {
		t.Fatalf("expected a flattened occurrence with no parent/recurrence-id, got %+v", got)
	}

	// Also reachable via the Override's own id, not just the Master's.
	got2, err := svc.GetOccurrence(ctx, userID, override.ID, recurrenceID)
	if err != nil {
		t.Fatalf("get occurrence via override id: %v", err)
	}
	if got2.Title != "Standup (moved)" {
		t.Fatalf("expected the override's own title via its own id, got %q", got2.Title)
	}
}

func TestEventService_GetSeriesForEvent_ResolvesViaOverrideID(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := start.AddDate(0, 0, 7)
	override, err := svc.Create(ctx, userID, "evt-1-override", EventWrite{
		CalendarID: calendarID, Title: "Standup (moved)", Start: recurrenceID.Add(2 * time.Hour), End: recurrenceID.Add(2*time.Hour + 30*time.Minute),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
	})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	gotMaster, overrides, err := svc.GetSeriesForEvent(ctx, userID, override.ID)
	if err != nil {
		t.Fatalf("get series for event: %v", err)
	}
	if gotMaster.ID != master.ID {
		t.Fatalf("expected master %q, got %q", master.ID, gotMaster.ID)
	}
	if len(overrides) != 1 || overrides[0].ID != override.ID {
		t.Fatalf("expected the one override, got %+v", overrides)
	}
}

func TestEventService_ImportSeries_WritesEverySeriesInOneChangeSeq(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)
	recurrenceID := start.AddDate(0, 0, 7)

	writes := []SeriesWrite{
		{
			Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY",
			Exdates: []time.Time{start.AddDate(0, 0, 14)},
			Overrides: []OverrideWrite{
				{RecurrenceID: recurrenceID, Title: "Standup (moved)", Start: recurrenceID.Add(time.Hour), End: recurrenceID.Add(time.Hour + 30*time.Minute)},
			},
		},
		{Title: "Planning", Start: start.AddDate(0, 0, 1), End: end.AddDate(0, 0, 1)},
	}

	count, err := svc.ImportSeries(ctx, userID, calendarID, writes)
	if err != nil {
		t.Fatalf("import series: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 series written, got %d", count)
	}

	events, err := svc.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 3 { // 2 masters + 1 override
		t.Fatalf("expected 3 rows, got %d: %+v", len(events), events)
	}

	var standup repository.Event
	for _, e := range events {
		if e.Title == "Standup" {
			standup = e
		}
	}
	if standup.ID == "" {
		t.Fatalf("expected to find the imported Standup master")
	}
	if len(standup.Exdates) != 1 {
		t.Fatalf("expected 1 exdate, got %d", len(standup.Exdates))
	}

	_, overrides, err := svc.GetSeries(ctx, userID, standup.ID)
	if err != nil {
		t.Fatalf("get series: %v", err)
	}
	if len(overrides) != 1 || overrides[0].Title != "Standup (moved)" {
		t.Fatalf("expected 1 override, got %+v", overrides)
	}

	// Both series' masters and the override all share the same change_seq —
	// the whole import is one atomic change (ADR-0030).
	changeSeqs := map[int64]bool{}
	for _, e := range events {
		changeSeqs[e.ChangeSeq] = true
	}
	if len(changeSeqs) != 1 {
		t.Fatalf("expected every row to share one change_seq, got %v", changeSeqs)
	}
}

func TestEventService_ImportSeries_RejectsInvalidTitleBeforeWritingAnything(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)

	writes := []SeriesWrite{
		{Title: "Standup", Start: start, End: end},
		{Title: "   ", Start: start, End: end},
	}

	_, err := svc.ImportSeries(ctx, userID, calendarID, writes)
	if !errors.Is(err, ErrInvalidTitle) {
		t.Fatalf("expected ErrInvalidTitle, got %v", err)
	}

	events, err := svc.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected nothing written when a later series is invalid, got %+v", events)
	}
}

func TestEventService_ImportSeries_UnknownCalendar(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)

	_, err := svc.ImportSeries(ctx, userID, "does-not-exist", []SeriesWrite{{Title: "Standup", Start: start, End: end}})
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, got %v", err)
	}
}

// --- Subscribed Calendar write guard (#84, ADR-0032) ---
//
// One test per mutating method proves it refuses a Subscribed Calendar with
// ErrCalendarReadOnly, and a further test proves
// ImportSubscribedSeries — the sole bypass — writes successfully where
// ImportSeries would now refuse.

// newTestSubscribedCalendar creates a Calendar carrying a SourceURL — a
// Subscribed Calendar (ADR-0032) — via svc's own CalendarService, reaching
// into the unexported field since this test lives in package service.
func newTestSubscribedCalendar(t *testing.T, svc *EventService, userID int64) string {
	t.Helper()
	sourceURL := "https://example.com/feed.ics"
	cal, err := svc.calendars.Create(context.Background(), userID, "sub-cal-1", CalendarWrite{Name: "Feed", Color: "#123456FF", SourceURL: &sourceURL})
	if err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}
	return cal.ID
}

// seedSubscribedEvent writes one series into subCalendarID via the bypass,
// then returns the id of the Master matching write.Title — the only way to
// get an Event into a Subscribed Calendar for the
// Update/Delete/AddException/ReparentFrom refusal tests below, since every
// other writer now refuses it. Looks up by title rather than assuming it's
// the calendar's only Master, so a test can seed more than one.
func seedSubscribedEvent(t *testing.T, svc *EventService, userID int64, subCalendarID string, write SeriesWrite) string {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.ImportSubscribedSeries(ctx, userID, subCalendarID, []SeriesWrite{write}); err != nil {
		t.Fatalf("seed subscribed event: %v", err)
	}
	events, err := svc.events.ListMastersByCalendar(ctx, subCalendarID)
	if err != nil {
		t.Fatalf("list masters: %v", err)
	}
	for _, e := range events {
		if e.Title == write.Title {
			return e.ID
		}
	}
	t.Fatalf("expected a master titled %q, got %+v", write.Title, events)
	return ""
}

func TestEventService_Create_RejectsSubscribedCalendar(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	subCalendarID := newTestSubscribedCalendar(t, svc, userID)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	_, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: subCalendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("expected ErrCalendarReadOnly, got %v", err)
	}
}

func TestEventService_Update_RejectsSubscribedCalendar(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	subCalendarID := newTestSubscribedCalendar(t, svc, userID)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	masterID := seedSubscribedEvent(t, svc, userID, subCalendarID, SeriesWrite{Title: "Standup", Start: start, End: end})

	_, err := svc.Update(ctx, userID, masterID, EventWrite{CalendarID: subCalendarID, Title: "Renamed", Start: start, End: end})
	if !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("expected ErrCalendarReadOnly, got %v", err)
	}
}

func TestEventService_Update_RejectsMovingEventOutOfSubscribedCalendar(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	subCalendarID := newTestSubscribedCalendar(t, svc, userID)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	masterID := seedSubscribedEvent(t, svc, userID, subCalendarID, SeriesWrite{Title: "Standup", Start: start, End: end})

	_, err := svc.Update(ctx, userID, masterID, EventWrite{CalendarID: calendarID, Title: "Renamed", Start: start, End: end})
	if !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("expected ErrCalendarReadOnly, got %v", err)
	}
}

func TestEventService_Delete_RejectsSubscribedCalendar(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	subCalendarID := newTestSubscribedCalendar(t, svc, userID)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	masterID := seedSubscribedEvent(t, svc, userID, subCalendarID, SeriesWrite{Title: "Standup", Start: start, End: start.Add(time.Hour)})

	err := svc.Delete(ctx, userID, masterID)
	if !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("expected ErrCalendarReadOnly, got %v", err)
	}
}

func TestEventService_AddException_RejectsSubscribedCalendar(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	subCalendarID := newTestSubscribedCalendar(t, svc, userID)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	masterID := seedSubscribedEvent(t, svc, userID, subCalendarID, SeriesWrite{Title: "Standup", Start: start, End: start.Add(time.Hour), Rrule: "FREQ=WEEKLY"})

	err := svc.AddException(ctx, userID, masterID, start)
	if !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("expected ErrCalendarReadOnly, got %v", err)
	}
}

func TestEventService_ReparentFrom_RejectsSubscribedCalendar(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	subCalendarID := newTestSubscribedCalendar(t, svc, userID)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	oldParentID := seedSubscribedEvent(t, svc, userID, subCalendarID, SeriesWrite{Title: "Old", Start: start, End: start.Add(time.Hour), Rrule: "FREQ=WEEKLY"})
	newParentID := seedSubscribedEvent(t, svc, userID, subCalendarID, SeriesWrite{Title: "New", Start: start.AddDate(0, 0, 7), End: start.AddDate(0, 0, 7).Add(time.Hour)})

	err := svc.ReparentFrom(ctx, userID, oldParentID, newParentID, start.AddDate(0, 0, 14))
	if !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("expected ErrCalendarReadOnly, got %v", err)
	}
}

func TestEventService_ImportSeries_RejectsSubscribedCalendar(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	subCalendarID := newTestSubscribedCalendar(t, svc, userID)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	_, err := svc.ImportSeries(ctx, userID, subCalendarID, []SeriesWrite{{Title: "Standup", Start: start, End: start.Add(time.Hour)}})
	if !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("expected ErrCalendarReadOnly, got %v", err)
	}
}

func TestEventService_PutSeries_RejectsSubscribedCalendar(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	subCalendarID := newTestSubscribedCalendar(t, svc, userID)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	_, _, err := svc.PutSeries(ctx, userID, subCalendarID, "evt-1", SeriesWrite{Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("expected ErrCalendarReadOnly, got %v", err)
	}
}

// TestEventService_ImportSubscribedSeries_BypassesGuardAndWrites proves the
// bypass ImportSeries' guard exists to force everything else through: the
// exact write ImportSeries just refused above succeeds via
// ImportSubscribedSeries.
func TestEventService_ImportSubscribedSeries_BypassesGuardAndWrites(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	subCalendarID := newTestSubscribedCalendar(t, svc, userID)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	n, err := svc.ImportSubscribedSeries(ctx, userID, subCalendarID, []SeriesWrite{{Title: "Standup", Start: start, End: start.Add(time.Hour)}})
	if err != nil {
		t.Fatalf("import subscribed series: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 series written, got %d", n)
	}

	events, err := svc.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 || events[0].CalendarID != subCalendarID {
		t.Fatalf("expected 1 event in the subscribed calendar, got %+v", events)
	}
}

// TestEventService_ClearSubscribedCalendarReminders_RemovesAllAndBumpsChangeSeq
// covers #87's immediate-turn-off behavior at the EventService layer: a
// Master and an Override in the same series each carry a Reminder, and
// clearing wipes both and bumps the Master's change_seq once so a CalDAV
// client picks up the alarm-less object.
func TestEventService_ClearSubscribedCalendarReminders_RemovesAllAndBumpsChangeSeq(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	subCalendarID := newTestSubscribedCalendar(t, svc, userID)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	rec := start.AddDate(0, 0, 7)

	write := SeriesWrite{
		Title: "Standup", Start: start, End: start.Add(time.Hour), Rrule: "FREQ=WEEKLY",
		Reminders: []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}},
		Overrides: []OverrideWrite{{
			RecurrenceID: rec, Title: "Standup (moved)", Start: rec, End: rec.Add(time.Hour),
			Reminders: []repository.Reminder{{OffsetMinutes: 15, Channel: "email"}},
		}},
	}
	if _, err := svc.ImportSubscribedSeries(ctx, userID, subCalendarID, []SeriesWrite{write}); err != nil {
		t.Fatalf("seed subscribed series: %v", err)
	}

	master, overridesByParent, err := svc.ListSeriesByCalendar(ctx, userID, subCalendarID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(master) != 1 || len(master[0].Reminders) != 1 {
		t.Fatalf("expected the seeded Master to carry 1 Reminder, got %+v", master)
	}
	override := overridesByParent[master[0].ID][0]
	if len(override.Reminders) != 1 {
		t.Fatalf("expected the seeded Override to carry 1 Reminder, got %+v", override)
	}
	changeSeqBefore := master[0].ChangeSeq

	if err := svc.ClearSubscribedCalendarReminders(ctx, userID, subCalendarID); err != nil {
		t.Fatalf("clear reminders: %v", err)
	}

	after, overridesAfter, err := svc.ListSeriesByCalendar(ctx, userID, subCalendarID)
	if err != nil {
		t.Fatalf("list series after clear: %v", err)
	}
	if len(after) != 1 || len(after[0].Reminders) != 0 {
		t.Fatalf("expected the Master's Reminders cleared, got %+v", after)
	}
	if len(overridesAfter[after[0].ID][0].Reminders) != 0 {
		t.Fatalf("expected the Override's Reminders cleared, got %+v", overridesAfter)
	}
	if after[0].ChangeSeq <= changeSeqBefore {
		t.Fatalf("expected change_seq to bump (was %d, now %d)", changeSeqBefore, after[0].ChangeSeq)
	}
}

func TestEventService_ClearSubscribedCalendarReminders_RejectsUnknownCalendar(t *testing.T) {
	svc, userID, _ := newTestEventService(t)

	err := svc.ClearSubscribedCalendarReminders(context.Background(), userID, "nope")
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, got %v", err)
	}
}

func TestEventService_ReconcileSubscribedSeries_CreatesNewSeries(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	subCalendarID := newTestSubscribedCalendar(t, svc, userID)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	result := ReconcileResult{
		Upserts: []SeriesUpsert{
			{Write: SeriesWrite{Title: "Standup", Start: start, End: start.Add(time.Hour), ExternalUID: "uid-1"}},
		},
	}

	summary, err := svc.ReconcileSubscribedSeries(ctx, userID, subCalendarID, result)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if summary.Created != 1 || summary.Updated != 0 || summary.Tombstoned != 0 {
		t.Fatalf("expected summary {1,0,0}, got %+v", summary)
	}

	masters, err := svc.events.ListMastersByCalendar(ctx, subCalendarID)
	if err != nil {
		t.Fatalf("list masters: %v", err)
	}
	if len(masters) != 1 || masters[0].Title != "Standup" {
		t.Fatalf("expected 1 created master, got %+v", masters)
	}
	if masters[0].ExternalUID == nil || *masters[0].ExternalUID != "uid-1" {
		t.Fatalf("expected ExternalUID uid-1, got %v", masters[0].ExternalUID)
	}
}

func TestEventService_ReconcileSubscribedSeries_UpdatesInPlaceKeepingMasterIDAndExternalUID(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	subCalendarID := newTestSubscribedCalendar(t, svc, userID)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	masterID := seedSubscribedEvent(t, svc, userID, subCalendarID, SeriesWrite{
		Title: "Standup", Start: start, End: start.Add(time.Hour), ExternalUID: "uid-1",
	})

	result := ReconcileResult{
		Upserts: []SeriesUpsert{
			{MasterID: masterID, Write: SeriesWrite{Title: "Standup (renamed)", Start: start, End: start.Add(time.Hour), ExternalUID: "uid-1"}},
		},
	}

	summary, err := svc.ReconcileSubscribedSeries(ctx, userID, subCalendarID, result)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if summary.Updated != 1 || summary.Created != 0 {
		t.Fatalf("expected summary {0,1,0}, got %+v", summary)
	}

	master, err := svc.events.GetByID(ctx, masterID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if master.ID != masterID {
		t.Fatalf("expected the same row id to be kept, got %q", master.ID)
	}
	if master.Title != "Standup (renamed)" {
		t.Fatalf("expected the updated title, got %q", master.Title)
	}
	if master.ExternalUID == nil || *master.ExternalUID != "uid-1" {
		t.Fatalf("expected ExternalUID to remain uid-1, got %v", master.ExternalUID)
	}
}

func TestEventService_ReconcileSubscribedSeries_TombstonesAbsentSeries(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	subCalendarID := newTestSubscribedCalendar(t, svc, userID)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	masterID := seedSubscribedEvent(t, svc, userID, subCalendarID, SeriesWrite{
		Title: "Standup", Start: start, End: start.Add(time.Hour), ExternalUID: "uid-1",
	})

	result := ReconcileResult{Tombstones: []string{masterID}}

	summary, err := svc.ReconcileSubscribedSeries(ctx, userID, subCalendarID, result)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if summary.Tombstoned != 1 {
		t.Fatalf("expected summary {0,0,1}, got %+v", summary)
	}

	if _, err := svc.events.GetByID(ctx, masterID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the tombstoned master to be gone, got %v", err)
	}

	deleted, err := svc.sync.DeletedSince(ctx, subCalendarID, 0)
	if err != nil {
		t.Fatalf("deleted since: %v", err)
	}
	if len(deleted) != 1 || deleted[0].UID != masterID {
		t.Fatalf("expected a tombstone recording %q, got %+v", masterID, deleted)
	}
}

func TestEventService_ReconcileSubscribedSeries_OverridesMatchedByRecurrenceID(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	subCalendarID := newTestSubscribedCalendar(t, svc, userID)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	rec := start.AddDate(0, 0, 7)

	masterID := seedSubscribedEvent(t, svc, userID, subCalendarID, SeriesWrite{
		Title: "Standup", Start: start, End: start.Add(time.Hour), Rrule: "FREQ=WEEKLY", ExternalUID: "uid-1",
		Overrides: []OverrideWrite{
			{RecurrenceID: rec, Title: "Standup (moved)", Start: rec.Add(time.Hour), End: rec.Add(2 * time.Hour), ExternalUID: "uid-1"},
		},
	})

	result := ReconcileResult{
		Upserts: []SeriesUpsert{{
			MasterID: masterID,
			Write: SeriesWrite{
				Title: "Standup", Start: start, End: start.Add(time.Hour), Rrule: "FREQ=WEEKLY", ExternalUID: "uid-1",
				Overrides: []OverrideWrite{
					{RecurrenceID: rec, Title: "Standup (moved again)", Start: rec.Add(time.Hour), End: rec.Add(2 * time.Hour), ExternalUID: "uid-1"},
				},
			},
		}},
	}

	if _, err := svc.ReconcileSubscribedSeries(ctx, userID, subCalendarID, result); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	_, overrides, err := svc.GetSeries(ctx, userID, masterID)
	if err != nil {
		t.Fatalf("get series: %v", err)
	}
	if len(overrides) != 1 || overrides[0].Title != "Standup (moved again)" {
		t.Fatalf("expected the override updated in place, got %+v", overrides)
	}
}

func TestEventService_ReconcileSubscribedSeries_RejectsInvalidTitle(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	subCalendarID := newTestSubscribedCalendar(t, svc, userID)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	result := ReconcileResult{
		Upserts: []SeriesUpsert{{Write: SeriesWrite{Title: "  ", Start: start, End: start.Add(time.Hour), ExternalUID: "uid-1"}}},
	}

	_, err := svc.ReconcileSubscribedSeries(ctx, userID, subCalendarID, result)
	if !errors.Is(err, ErrInvalidTitle) {
		t.Fatalf("expected ErrInvalidTitle, got %v", err)
	}
}
