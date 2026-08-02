package service

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// newTestCalendarService returns a CalendarService plus a real user id to
// satisfy calendars.user_id's foreign key (SQLite enforces it).
func newTestCalendarService(t *testing.T) (svc *CalendarService, userID int64) {
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

	return NewCalendarService(repository.NewCalendarRepository(sqlDB)), user.ID
}

func TestCalendarService_Create(t *testing.T) {
	svc, userID := newTestCalendarService(t)
	ctx := context.Background()

	calendar, err := svc.Create(ctx, userID, "cal-1", "Personal", "peacock")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if calendar.Name != "Personal" || calendar.Color != "peacock" {
		t.Fatalf("unexpected calendar: %+v", calendar)
	}
}

func TestCalendarService_Create_RejectsInvalidColor(t *testing.T) {
	svc, userID := newTestCalendarService(t)

	_, err := svc.Create(context.Background(), userID, "cal-1", "Personal", "not-a-real-color")
	if !errors.Is(err, ErrInvalidColor) {
		t.Fatalf("expected ErrInvalidColor, got %v", err)
	}
}

func TestCalendarService_Create_RejectsEmptyName(t *testing.T) {
	svc, userID := newTestCalendarService(t)

	_, err := svc.Create(context.Background(), userID, "cal-1", "  ", "peacock")
	if !errors.Is(err, ErrInvalidName) {
		t.Fatalf("expected ErrInvalidName, got %v", err)
	}
}

func TestCalendarService_List(t *testing.T) {
	svc, userID := newTestCalendarService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, userID, "cal-1", "Personal", "peacock"); err != nil {
		t.Fatalf("create: %v", err)
	}

	calendars, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(calendars) != 1 {
		t.Fatalf("expected 1 calendar, got %d", len(calendars))
	}
}

func TestCalendarService_Update(t *testing.T) {
	svc, userID := newTestCalendarService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, userID, "cal-1", "Personal", "peacock"); err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := svc.Update(ctx, userID, "cal-1", "Renamed", "tomato")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "Renamed" || updated.Color != "tomato" {
		t.Fatalf("unexpected calendar: %+v", updated)
	}
}

func TestCalendarService_Update_RejectsInvalidColor(t *testing.T) {
	svc, userID := newTestCalendarService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, userID, "cal-1", "Personal", "peacock"); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, userID, "cal-1", "Personal", "not-a-real-color")
	if !errors.Is(err, ErrInvalidColor) {
		t.Fatalf("expected ErrInvalidColor, got %v", err)
	}
}

func TestCalendarService_Update_NotFound(t *testing.T) {
	svc, userID := newTestCalendarService(t)

	_, err := svc.Update(context.Background(), userID, "nope", "Renamed", "tomato")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarService_Delete(t *testing.T) {
	svc, userID := newTestCalendarService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, userID, "cal-1", "Personal", "peacock"); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.Delete(ctx, userID, "cal-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	calendars, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(calendars) != 0 {
		t.Fatalf("expected 0 calendars after delete, got %d", len(calendars))
	}
}

func TestCalendarService_Delete_NotFound(t *testing.T) {
	svc, userID := newTestCalendarService(t)

	err := svc.Delete(context.Background(), userID, "nope")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarService_IsValidColor(t *testing.T) {
	svc, _ := newTestCalendarService(t)

	if !svc.IsValidColor("graphite") {
		t.Fatalf("expected graphite to be a valid color")
	}
	if svc.IsValidColor("not-a-real-color") {
		t.Fatalf("expected not-a-real-color to be invalid")
	}
}
