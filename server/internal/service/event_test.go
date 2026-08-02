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

	return NewEventService(repository.NewEventRepository(sqlDB), NewCalendarService(calendarRepo)), user.ID, cal.ID
}

func TestEventService_Create(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	event, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if event.Title != "Standup" {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func TestEventService_Create_RejectsEmptyTitle(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", calendarID, "   ", start, end)
	if !errors.Is(err, ErrInvalidTitle) {
		t.Fatalf("expected ErrInvalidTitle, got %v", err)
	}
}

func TestEventService_Create_RejectsEndNotAfterStart(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", calendarID, "Standup", start, end)
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("expected ErrInvalidTimeRange, got %v", err)
	}
}

func TestEventService_Create_RejectsEqualStartAndEnd(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	at := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", calendarID, "Standup", at, at)
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("expected ErrInvalidTimeRange, got %v", err)
	}
}

func TestEventService_Create_RejectsUnknownCalendar(t *testing.T) {
	svc, userID, _ := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), userID, "evt-1", "does-not-exist", "Standup", start, end)
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, got %v", err)
	}
}

func TestEventService_Create_RejectsAnotherUsersCalendar(t *testing.T) {
	svc, _, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(context.Background(), 99999, "evt-1", calendarID, "Standup", start, end)
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound for another user's calendar, got %v", err)
	}
}

func TestEventService_List(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end); err != nil {
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

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end); err != nil {
		t.Fatalf("create: %v", err)
	}

	newStart := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)
	newEnd := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	updated, err := svc.Update(ctx, userID, "evt-1", calendarID, "Renamed", newStart, newEnd)
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

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, userID, "evt-1", calendarID, "Standup", end, start)
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("expected ErrInvalidTimeRange, got %v", err)
	}
}

func TestEventService_Update_NotFound(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Update(context.Background(), userID, "nope", calendarID, "Standup", start, end)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEventService_Update_RejectsUnknownCalendar(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, userID, "evt-1", "does-not-exist", "Standup", start, end)
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, got %v", err)
	}
}

func TestEventService_Update_RejectsAnotherUsersCalendar(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, 99999, "evt-1", calendarID, "Standup", start, end)
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, since the calendar ownership check runs before the event lookup, got %v", err)
	}
}

func TestEventService_Delete(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, userID, "evt-1", calendarID, "Standup", start, end); err != nil {
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
