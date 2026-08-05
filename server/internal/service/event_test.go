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

	return NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewSyncRepository(sqlDB), NewCalendarService(calendarRepo)), user.ID, cal.ID
}

func TestEventService_Create(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	event, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", "")
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
	created, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, rrule, nil, nil, nil, nil, "", "")
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

	created, err := svc.Create(ctx, userID, "evt-1", calendarID, "Holiday", start, end, true, "", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !created.AllDay {
		t.Fatalf("expected created event to be all-day, got %+v", created)
	}

	updated, err := svc.Update(ctx, userID, "evt-1", calendarID, "Holiday", start, end, false, "", nil, nil, "", "")
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

	created, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Tzid != nil {
		t.Fatalf("expected nil tzid, got %+v", created)
	}

	zone := "Europe/Berlin"
	updated, err := svc.Update(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", &zone, nil, "", "")
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

	_, err := svc.Create(context.Background(), userID, "evt-1", calendarID, "Standup", start, end, false, "not a rule", nil, nil, nil, nil, "", "")
	if !errors.Is(err, ErrInvalidRecurrenceRule) {
		t.Fatalf("expected ErrInvalidRecurrenceRule, got %v", err)
	}
}

func TestEventService_Update_RoundTripsRrule(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	rrule := "FREQ=DAILY"
	updated, err := svc.Update(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, rrule, nil, nil, "", "")
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

	_, err := svc.Create(context.Background(), userID, "evt-1", calendarID, "   ", start, end, false, "", nil, nil, nil, nil, "", "")
	if !errors.Is(err, ErrInvalidTitle) {
		t.Fatalf("expected ErrInvalidTitle, got %v", err)
	}
}

func TestEventService_Create_RejectsEndNotAfterStart(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", "")
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("expected ErrInvalidTimeRange, got %v", err)
	}
}

func TestEventService_Create_RejectsEqualStartAndEnd(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	at := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", calendarID, "Standup", at, at, false, "", nil, nil, nil, nil, "", "")
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("expected ErrInvalidTimeRange, got %v", err)
	}
}

func TestEventService_Create_RejectsUnknownCalendar(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", "does-not-exist", "Standup", start, end, false, "", nil, nil, nil, nil, "", "")
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, got %v", err)
	}
}

func TestEventService_Create_RejectsAnotherUsersCalendar(t *testing.T) {
	svc, _, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), 99999, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", "")
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound for another user's calendar, got %v", err)
	}
}

func TestEventService_List(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", ""); err != nil {
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

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	newStart := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)
	newEnd := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	updated, err := svc.Update(ctx, userID, "evt-1", calendarID, "Renamed", newStart, newEnd, false, "", nil, nil, "", "")
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

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, userID, "evt-1", calendarID, "Standup", end, start, false, "", nil, nil, "", "")
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("expected ErrInvalidTimeRange, got %v", err)
	}
}

func TestEventService_Update_NotFound(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Update(context.Background(), userID, "nope", calendarID, "Standup", start, end, false, "", nil, nil, "", "")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEventService_Update_RejectsUnknownCalendar(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, userID, "evt-1", "does-not-exist", "Standup", start, end, false, "", nil, nil, "", "")
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, got %v", err)
	}
}

func TestEventService_Update_RejectsAnotherUsersCalendar(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, 99999, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, "", "")
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, since the calendar ownership check runs before the event lookup, got %v", err)
	}
}

func TestEventService_Delete(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", ""); err != nil {
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "FREQ=DAILY", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), false,
		"", &master.ID, &recurrenceID, nil, nil, "", "")
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "FREQ=DAILY", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	_, err = svc.Create(ctx, userID, "override", calendarID, "Standup (moved)", start, end, false, "FREQ=DAILY", &master.ID, &recurrenceID, nil, nil, "", "")
	if !errors.Is(err, ErrInvalidOverride) {
		t.Fatalf("expected ErrInvalidOverride, got %v", err)
	}
}

func TestEventService_CreateOverride_RejectsOverridingAnOverride(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "FREQ=DAILY", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), false,
		"", &master.ID, &recurrenceID, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	anotherRecurrenceID := time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)
	_, err = svc.Create(ctx, userID, "double-override", calendarID, "Nope", start, end, false, "", &override.ID, &anotherRecurrenceID, nil, nil, "", "")
	if !errors.Is(err, ErrParentIsOverride) {
		t.Fatalf("expected ErrParentIsOverride, got %v", err)
	}
}

func TestEventService_AddException(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "FREQ=DAILY", nil, nil, nil, nil, "", "")
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", "")
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "FREQ=DAILY", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), false,
		"", &master.ID, &recurrenceID, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create override: %v", err)
	}
	if err := svc.AddException(ctx, userID, master.ID, time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	if _, err := svc.Update(ctx, userID, master.ID, calendarID, "Standup", start, end, false, "FREQ=WEEKLY;BYDAY=TH", nil, nil, "", ""); err != nil {
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "FREQ=DAILY", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), false,
		"", &master.ID, &recurrenceID, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	if _, err := svc.Update(ctx, userID, master.ID, calendarID, "Renamed", start, end, false, "FREQ=DAILY", nil, nil, "", ""); err != nil {
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "FREQ=DAILY", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), false,
		"", &master.ID, &recurrenceID, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create override: %v", err)
	}
	if err := svc.AddException(ctx, userID, master.ID, time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	if _, err := svc.Update(ctx, userID, master.ID, calendarID, "Standup", start, end, false, "FREQ=DAILY;UNTIL=20260101T085959Z", nil, nil, "", ""); err != nil {
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, false, "FREQ=DAILY", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC), false,
		"", &master.ID, &recurrenceID, nil, nil, "", "")
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

	if _, err := svc.Update(ctx, userID, master.ID, calendarID, "Standup", start, end, false, "FREQ=WEEKLY;BYDAY=TH", nil, nil, "", ""); err == nil {
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
		"FREQ=DAILY", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create old master: %v", err)
	}
	newMaster, err := svc.Create(ctx, userID, "new-master", calendarID, "Standup",
		time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), time.Date(2026, 1, 5, 9, 30, 0, 0, time.UTC), false,
		"FREQ=DAILY", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create new master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 5, 10, 30, 0, 0, time.UTC), false,
		"", &oldMaster.ID, &recurrenceID, nil, nil, "", "")
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
		"FREQ=DAILY", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create old master: %v", err)
	}
	newMaster, err := svc.Create(ctx, userID, "new-master", calendarID, "Standup",
		time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), time.Date(2026, 1, 5, 9, 30, 0, 0, time.UTC), false,
		"FREQ=DAILY", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create new master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 5, 10, 30, 0, 0, time.UTC), false,
		"", &oldMaster.ID, &recurrenceID, nil, nil, "", "")
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
	created, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, reminders, "", "")
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

	created, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", "")
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

	_, err := svc.Create(context.Background(), userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil,
		[]repository.Reminder{{OffsetMinutes: 10, Channel: "sms"}}, "", "")
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

	_, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil,
		[]repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}, "", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := svc.Update(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil,
		[]repository.Reminder{{OffsetMinutes: 30, Channel: "email"}}, "", "")
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

	_, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil,
		[]repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}, "", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := svc.Update(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, "", "")
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

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil,
		[]repository.Reminder{{OffsetMinutes: 10, Channel: "sms"}}, "", "")
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

	_, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil,
		[]repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}, "", "")
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

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := svc.db.ExecContext(ctx,
		`CREATE TRIGGER poison_reminder_insert BEFORE INSERT ON event_reminders WHEN NEW.event_id = 'evt-1' BEGIN SELECT RAISE(ABORT, 'boom'); END`,
	); err != nil {
		t.Fatalf("install poison trigger: %v", err)
	}

	if _, err := svc.Update(ctx, userID, "evt-1", calendarID, "Renamed", start, end, false, "",
		nil, []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}, "", ""); err == nil {
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

	master, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "FREQ=WEEKLY;BYDAY=TU",
		nil, nil, nil, []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}, "", "")
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := start.AddDate(0, 0, 7)
	if _, err := svc.Create(ctx, userID, "evt-1-override", calendarID, "Standup (moved)",
		recurrenceID.Add(2*time.Hour), recurrenceID.Add(2*time.Hour+30*time.Minute), false, "",
		&master.ID, &recurrenceID, nil, []repository.Reminder{{OffsetMinutes: 5, Channel: "email"}}, "", "",
	); err != nil {
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

	master, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "FREQ=WEEKLY;BYDAY=TU", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := start.AddDate(0, 0, 7)
	if _, err := svc.Create(ctx, userID, "evt-1-override", calendarID, "Standup (moved)",
		recurrenceID, recurrenceID.Add(30*time.Minute), false, "", &master.ID, &recurrenceID, nil, nil, "", "",
	); err != nil {
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
	if _, err := svc.Create(ctx, userID, "evt-standalone", calendarID, "One-off", standaloneStart, standaloneStart.Add(time.Hour), false, "", nil, nil, nil, nil, "", ""); err != nil {
		t.Fatalf("create standalone: %v", err)
	}

	masterStart := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	master, err := svc.Create(ctx, userID, "evt-master", calendarID, "Standup", masterStart, masterStart.Add(30*time.Minute), false, "FREQ=WEEKLY;BYDAY=TU", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := masterStart.AddDate(0, 0, 7)
	if _, err := svc.Create(ctx, userID, "evt-override", calendarID, "Standup (moved)",
		recurrenceID.Add(time.Hour), recurrenceID.Add(90*time.Minute), false, "", &master.ID, &recurrenceID, nil, nil, "", "",
	); err != nil {
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

// masterChangeSeq is a small helper for the change_seq tests below: it reads
// masterID's own change_seq back out of the repository, bypassing the
// service layer's Get (which doesn't expose it).
func masterChangeSeq(t *testing.T, svc *EventService, userID int64, masterID string) int64 {
	t.Helper()
	e, err := svc.events.GetByID(context.Background(), userID, masterID)
	if err != nil {
		t.Fatalf("get master %q: %v", masterID, err)
	}
	return e.ChangeSeq
}

func TestEventService_Create_Override_BumpsParentChangeSeq(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	masterStart := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	master, err := svc.Create(ctx, userID, "evt-master", calendarID, "Standup", masterStart, masterStart.Add(30*time.Minute), false, "FREQ=WEEKLY;BYDAY=TU", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	before := masterChangeSeq(t, svc, userID, master.ID)

	recurrenceID := masterStart.AddDate(0, 0, 7)
	if _, err := svc.Create(ctx, userID, "evt-override", calendarID, "Standup (moved)",
		recurrenceID.Add(time.Hour), recurrenceID.Add(90*time.Minute), false, "", &master.ID, &recurrenceID, nil, nil, "", "",
	); err != nil {
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
	master, err := svc.Create(ctx, userID, "evt-master", calendarID, "Standup", masterStart, masterStart.Add(30*time.Minute), false, "FREQ=WEEKLY;BYDAY=TU", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := masterStart.AddDate(0, 0, 7)
	override, err := svc.Create(ctx, userID, "evt-override", calendarID, "Standup (moved)",
		recurrenceID.Add(time.Hour), recurrenceID.Add(90*time.Minute), false, "", &master.ID, &recurrenceID, nil, nil, "", "",
	)
	if err != nil {
		t.Fatalf("create override: %v", err)
	}
	before := masterChangeSeq(t, svc, userID, master.ID)

	if _, err := svc.Update(ctx, userID, override.ID, calendarID, "Standup (moved again)",
		override.Start, override.End, false, "", nil, nil, "", "",
	); err != nil {
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
	master, err := svc.Create(ctx, userID, "evt-master", calendarID, "Standup", masterStart, masterStart.Add(30*time.Minute), false, "FREQ=WEEKLY;BYDAY=TU", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := masterStart.AddDate(0, 0, 7)
	override, err := svc.Create(ctx, userID, "evt-override", calendarID, "Standup (moved)",
		recurrenceID.Add(time.Hour), recurrenceID.Add(90*time.Minute), false, "", &master.ID, &recurrenceID, nil, nil, "", "",
	)
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

	master, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	beforeCTag, err := svc.CalendarCTag(ctx, calendarID)
	if err != nil {
		t.Fatalf("ctag: %v", err)
	}

	if err := svc.Delete(ctx, userID, master.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	afterCTag, err := svc.CalendarCTag(ctx, calendarID)
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
	master, err := svc.Create(ctx, userID, "evt-master", calendarID, "Standup", masterStart, masterStart.Add(30*time.Minute), false, "FREQ=WEEKLY;BYDAY=TU", nil, nil, nil, nil, "", "")
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
	oldParent, err := svc.Create(ctx, userID, "evt-old", calendarID, "Standup", masterStart, masterStart.Add(30*time.Minute), false, "FREQ=WEEKLY;BYDAY=TU", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create old parent: %v", err)
	}
	newParentStart := masterStart.AddDate(0, 0, 14)
	newParent, err := svc.Create(ctx, userID, "evt-new", calendarID, "Standup", newParentStart, newParentStart.Add(30*time.Minute), false, "FREQ=WEEKLY;BYDAY=TU", nil, nil, nil, nil, "", "")
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
	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", ""); err != nil {
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
	if _, err := svc.Create(ctx, userID, "evt-master", calendarID, "Standup", masterStart, masterEnd, false, "FREQ=WEEKLY;BYDAY=TU", nil, nil, nil, nil, "", ""); err != nil {
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
	master, err := svc.Create(ctx, userID, "evt-master", calendarID, "Standup", masterStart, masterEnd, false, "FREQ=WEEKLY;BYDAY=TU", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := masterStart.AddDate(0, 0, 7)
	override, err := svc.Create(ctx, userID, "evt-override", calendarID, "Standup (moved)", recurrenceID.Add(2*time.Hour), recurrenceID.Add(2*time.Hour+30*time.Minute), false, "", &master.ID, &recurrenceID, nil, nil, "", "")
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
	master, err := svc.Create(ctx, userID, "evt-master", calendarID, "Standup", masterStart, masterEnd, false, "FREQ=WEEKLY;BYDAY=TU", nil, nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := masterStart.AddDate(0, 0, 7)
	if _, err := svc.Create(ctx, userID, "evt-override", calendarID, "Standup (moved)", recurrenceID.Add(2*time.Hour), recurrenceID.Add(2*time.Hour+30*time.Minute), false, "", &master.ID, &recurrenceID, nil, nil, "", ""); err != nil {
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
	if _, err := svc.Create(ctx, userID, "evt-master", calendarID, "Standup", masterStart, masterEnd, false, "FREQ=WEEKLY;BYDAY=TU", nil, nil, nil, nil, "", ""); err != nil {
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
	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, false, "", nil, nil, nil, nil, "", ""); err != nil {
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
