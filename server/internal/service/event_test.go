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

	user, err := repository.NewUserRepository(sqlDB).Create(context.Background(), "user-a", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	calendarRepo := repository.NewCalendarRepository(sqlDB)
	cal, err := calendarRepo.Create(context.Background(), user.ID, "cal-1", "Personal", "peacock")
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	return NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), NewCalendarService(calendarRepo)), user.ID, cal.ID
}

func TestEventService_Create(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	event, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil)
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
	created, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, rrule, nil, nil, nil)
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

	created, err := svc.Create(ctx, userID, "evt-1", calendarID, "Holiday", start, end, true, "", nil, nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !created.AllDay {
		t.Fatalf("expected created event to be all-day, got %+v", created)
	}

	updated, err := svc.Update(ctx, userID, "evt-1", calendarID, "Holiday", start, end, false, "", nil)
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

	created, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Tzid != nil {
		t.Fatalf("expected nil tzid, got %+v", created)
	}

	zone := "Europe/Berlin"
	updated, err := svc.Update(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", &zone)
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

	_, err := svc.Create(context.Background(), userID, "evt-1", calendarID, "Standup", start, end, false, "not a rule", nil, nil, nil)
	if !errors.Is(err, ErrInvalidRecurrenceRule) {
		t.Fatalf("expected ErrInvalidRecurrenceRule, got %v", err)
	}
}

func TestEventService_Update_RoundTripsRrule(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil); err != nil {
		t.Fatalf("create: %v", err)
	}

	rrule := "FREQ=DAILY"
	updated, err := svc.Update(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, rrule, nil)
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

	_, err := svc.Create(context.Background(), userID, "evt-1", calendarID, "   ", start, end, false, "", nil, nil, nil)
	if !errors.Is(err, ErrInvalidTitle) {
		t.Fatalf("expected ErrInvalidTitle, got %v", err)
	}
}

func TestEventService_Create_RejectsEndNotAfterStart(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil)
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("expected ErrInvalidTimeRange, got %v", err)
	}
}

func TestEventService_Create_RejectsEqualStartAndEnd(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	at := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", calendarID, "Standup", at, at, false, "", nil, nil, nil)
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("expected ErrInvalidTimeRange, got %v", err)
	}
}

func TestEventService_Create_RejectsUnknownCalendar(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", "does-not-exist", "Standup", start, end, false, "", nil, nil, nil)
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, got %v", err)
	}
}

func TestEventService_Create_RejectsAnotherUsersCalendar(t *testing.T) {
	svc, _, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), 99999, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil)
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound for another user's calendar, got %v", err)
	}
}

func TestEventService_List(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil); err != nil {
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

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil); err != nil {
		t.Fatalf("create: %v", err)
	}

	newStart := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)
	newEnd := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	updated, err := svc.Update(ctx, userID, "evt-1", calendarID, "Renamed", newStart, newEnd, false, "", nil)
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

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, userID, "evt-1", calendarID, "Standup", end, start, false, "", nil)
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("expected ErrInvalidTimeRange, got %v", err)
	}
}

func TestEventService_Update_NotFound(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Update(context.Background(), userID, "nope", calendarID, "Standup", start, end, false, "", nil)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEventService_Update_RejectsUnknownCalendar(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, userID, "evt-1", "does-not-exist", "Standup", start, end, false, "", nil)
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, got %v", err)
	}
}

func TestEventService_Update_RejectsAnotherUsersCalendar(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, 99999, "evt-1", calendarID, "Standup", start, end, false, "", nil)
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, since the calendar ownership check runs before the event lookup, got %v", err)
	}
}

func TestEventService_Delete(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil); err != nil {
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

func TestEventService_CreateOverride(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "FREQ=DAILY", nil, nil, nil)
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), false,
		"", &master.ID, &recurrenceID, nil)
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "FREQ=DAILY", nil, nil, nil)
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	_, err = svc.Create(ctx, userID, "override", calendarID, "Standup (moved)", start, end, false, "FREQ=DAILY", &master.ID, &recurrenceID, nil)
	if !errors.Is(err, ErrInvalidOverride) {
		t.Fatalf("expected ErrInvalidOverride, got %v", err)
	}
}

func TestEventService_CreateOverride_RejectsOverridingAnOverride(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "FREQ=DAILY", nil, nil, nil)
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), false,
		"", &master.ID, &recurrenceID, nil)
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	anotherRecurrenceID := time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)
	_, err = svc.Create(ctx, userID, "double-override", calendarID, "Nope", start, end, false, "", &override.ID, &anotherRecurrenceID, nil)
	if !errors.Is(err, ErrParentIsOverride) {
		t.Fatalf("expected ErrParentIsOverride, got %v", err)
	}
}

func TestEventService_AddException(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "FREQ=DAILY", nil, nil, nil)
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "", nil, nil, nil)
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

func TestEventService_Update_DiscardsChildrenOnRuleChange(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "FREQ=DAILY", nil, nil, nil)
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), false,
		"", &master.ID, &recurrenceID, nil)
	if err != nil {
		t.Fatalf("create override: %v", err)
	}
	if err := svc.AddException(ctx, userID, master.ID, time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	if _, err := svc.Update(ctx, userID, master.ID, calendarID, "Standup", start, end, false, "FREQ=WEEKLY;BYDAY=TH", nil); err != nil {
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "FREQ=DAILY", nil, nil, nil)
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), false,
		"", &master.ID, &recurrenceID, nil)
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	if _, err := svc.Update(ctx, userID, master.ID, calendarID, "Renamed", start, end, false, "FREQ=DAILY", nil); err != nil {
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "FREQ=DAILY", nil, nil, nil)
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), false,
		"", &master.ID, &recurrenceID, nil)
	if err != nil {
		t.Fatalf("create override: %v", err)
	}
	if err := svc.AddException(ctx, userID, master.ID, time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	if _, err := svc.Update(ctx, userID, master.ID, calendarID, "Standup", start, end, false, "FREQ=DAILY;UNTIL=20260101T085959Z", nil); err != nil {
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "FREQ=DAILY", nil, nil, nil)
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), false,
		"", &master.ID, &recurrenceID, nil)
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

	if _, err := svc.Update(ctx, userID, master.ID, calendarID, "Standup", start, end, false, "FREQ=WEEKLY;BYDAY=TH", nil); err == nil {
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

	oldMaster, err := svc.Create(ctx, userID, "old-master", calendarID, "Standup",
		time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC), false,
		"FREQ=DAILY", nil, nil, nil)
	if err != nil {
		t.Fatalf("create old master: %v", err)
	}
	newMaster, err := svc.Create(ctx, userID, "new-master", calendarID, "Standup",
		time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), time.Date(2026, 1, 5, 9, 30, 0, 0, time.UTC), false,
		"FREQ=DAILY", nil, nil, nil)
	if err != nil {
		t.Fatalf("create new master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 5, 10, 30, 0, 0, time.UTC), false,
		"", &oldMaster.ID, &recurrenceID, nil)
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

	oldMaster, err := svc.Create(ctx, userID, "old-master", calendarID, "Standup",
		time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC), false,
		"FREQ=DAILY", nil, nil, nil)
	if err != nil {
		t.Fatalf("create old master: %v", err)
	}
	newMaster, err := svc.Create(ctx, userID, "new-master", calendarID, "Standup",
		time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), time.Date(2026, 1, 5, 9, 30, 0, 0, time.UTC), false,
		"FREQ=DAILY", nil, nil, nil)
	if err != nil {
		t.Fatalf("create new master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 5, 10, 30, 0, 0, time.UTC), false,
		"", &oldMaster.ID, &recurrenceID, nil)
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
