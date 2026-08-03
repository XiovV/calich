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

	return NewEventService(repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), NewCalendarService(calendarRepo)), user.ID, cal.ID
}

func TestEventService_Create(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	event, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, "", nil, nil)
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
	created, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, rrule, nil, nil)
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

func TestEventService_Create_RejectsMalformedRrule(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", calendarID, "Standup", start, end, "not a rule", nil, nil)
	if !errors.Is(err, ErrInvalidRecurrenceRule) {
		t.Fatalf("expected ErrInvalidRecurrenceRule, got %v", err)
	}
}

func TestEventService_Update_RoundTripsRrule(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, "", nil, nil); err != nil {
		t.Fatalf("create: %v", err)
	}

	rrule := "FREQ=DAILY"
	updated, err := svc.Update(ctx, userID, "evt-1", calendarID, "Standup", start, end, rrule)
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

	_, err := svc.Create(context.Background(), userID, "evt-1", calendarID, "   ", start, end, "", nil, nil)
	if !errors.Is(err, ErrInvalidTitle) {
		t.Fatalf("expected ErrInvalidTitle, got %v", err)
	}
}

func TestEventService_Create_RejectsEndNotAfterStart(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", calendarID, "Standup", start, end, "", nil, nil)
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("expected ErrInvalidTimeRange, got %v", err)
	}
}

func TestEventService_Create_RejectsEqualStartAndEnd(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	at := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", calendarID, "Standup", at, at, "", nil, nil)
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("expected ErrInvalidTimeRange, got %v", err)
	}
}

func TestEventService_Create_RejectsUnknownCalendar(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", "does-not-exist", "Standup", start, end, "", nil, nil)
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, got %v", err)
	}
}

func TestEventService_Create_RejectsAnotherUsersCalendar(t *testing.T) {
	svc, _, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), 99999, "evt-1", calendarID, "Standup", start, end, "", nil, nil)
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound for another user's calendar, got %v", err)
	}
}

func TestEventService_List(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, "", nil, nil); err != nil {
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

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, "", nil, nil); err != nil {
		t.Fatalf("create: %v", err)
	}

	newStart := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)
	newEnd := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	updated, err := svc.Update(ctx, userID, "evt-1", calendarID, "Renamed", newStart, newEnd, "")
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

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, "", nil, nil); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, userID, "evt-1", calendarID, "Standup", end, start, "")
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("expected ErrInvalidTimeRange, got %v", err)
	}
}

func TestEventService_Update_NotFound(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Update(context.Background(), userID, "nope", calendarID, "Standup", start, end, "")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEventService_Update_RejectsUnknownCalendar(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, "", nil, nil); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, userID, "evt-1", "does-not-exist", "Standup", start, end, "")
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, got %v", err)
	}
}

func TestEventService_Update_RejectsAnotherUsersCalendar(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, "", nil, nil); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, 99999, "evt-1", calendarID, "Standup", start, end, "")
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, since the calendar ownership check runs before the event lookup, got %v", err)
	}
}

func TestEventService_Delete(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end, "", nil, nil); err != nil {
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, "FREQ=DAILY", nil, nil)
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC),
		"", &master.ID, &recurrenceID)
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, "FREQ=DAILY", nil, nil)
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	_, err = svc.Create(ctx, userID, "override", calendarID, "Standup (moved)", start, end, "FREQ=DAILY", &master.ID, &recurrenceID)
	if !errors.Is(err, ErrInvalidOverride) {
		t.Fatalf("expected ErrInvalidOverride, got %v", err)
	}
}

func TestEventService_CreateOverride_RejectsOverridingAnOverride(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, "FREQ=DAILY", nil, nil)
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC),
		"", &master.ID, &recurrenceID)
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	anotherRecurrenceID := time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)
	_, err = svc.Create(ctx, userID, "double-override", calendarID, "Nope", start, end, "", &override.ID, &anotherRecurrenceID)
	if !errors.Is(err, ErrParentIsOverride) {
		t.Fatalf("expected ErrParentIsOverride, got %v", err)
	}
}

func TestEventService_AddException(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, "FREQ=DAILY", nil, nil)
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, "", nil, nil)
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, "FREQ=DAILY", nil, nil)
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC),
		"", &master.ID, &recurrenceID)
	if err != nil {
		t.Fatalf("create override: %v", err)
	}
	if err := svc.AddException(ctx, userID, master.ID, time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	if _, err := svc.Update(ctx, userID, master.ID, calendarID, "Standup", start, end, "FREQ=WEEKLY;BYDAY=TH"); err != nil {
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, "FREQ=DAILY", nil, nil)
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC),
		"", &master.ID, &recurrenceID)
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	if _, err := svc.Update(ctx, userID, master.ID, calendarID, "Renamed", start, end, "FREQ=DAILY"); err != nil {
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

	master, err := svc.Create(ctx, userID, "master", calendarID, "Standup", start, end, "FREQ=DAILY", nil, nil)
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC),
		"", &master.ID, &recurrenceID)
	if err != nil {
		t.Fatalf("create override: %v", err)
	}
	if err := svc.AddException(ctx, userID, master.ID, time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	if _, err := svc.Update(ctx, userID, master.ID, calendarID, "Standup", start, end, "FREQ=DAILY;UNTIL=20260101T085959Z"); err != nil {
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

func TestEventService_ReparentFrom(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()

	oldMaster, err := svc.Create(ctx, userID, "old-master", calendarID, "Standup",
		time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC),
		"FREQ=DAILY", nil, nil)
	if err != nil {
		t.Fatalf("create old master: %v", err)
	}
	newMaster, err := svc.Create(ctx, userID, "new-master", calendarID, "Standup",
		time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), time.Date(2026, 1, 5, 9, 30, 0, 0, time.UTC),
		"FREQ=DAILY", nil, nil)
	if err != nil {
		t.Fatalf("create new master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", calendarID, "Standup (moved)",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 5, 10, 30, 0, 0, time.UTC),
		"", &oldMaster.ID, &recurrenceID)
	if err != nil {
		t.Fatalf("create override: %v", err)
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
}

func TestEventService_ReparentFrom_NotFound(t *testing.T) {
	svc, userID, _ := newTestEventService(t)

	err := svc.ReparentFrom(context.Background(), userID, "nope", "also-nope", time.Now())
	if !errors.Is(err, ErrParentNotFound) {
		t.Fatalf("expected ErrParentNotFound, got %v", err)
	}
}
