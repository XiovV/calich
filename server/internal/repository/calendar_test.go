package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
)

func newTestCalendarRepository(t *testing.T) *CalendarRepository {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	return NewCalendarRepository(sqlDB)
}

func TestCalendarRepository_CreateAndGetByID(t *testing.T) {
	repo := newTestCalendarRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, 1, "cal-1", "Personal", "peacock")
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	if created != (Calendar{ID: "cal-1", UserID: 1, Name: "Personal", Color: "peacock", CreatedAt: created.CreatedAt}) {
		t.Fatalf("unexpected created calendar: %+v", created)
	}

	fetched, err := repo.GetByID(ctx, 1, "cal-1")
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if fetched != created {
		t.Fatalf("expected fetched calendar %+v to equal created calendar %+v", fetched, created)
	}
}

func TestCalendarRepository_GetByID_NotFound(t *testing.T) {
	repo := newTestCalendarRepository(t)

	_, err := repo.GetByID(context.Background(), 1, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_GetByID_ScopedToUser(t *testing.T) {
	repo := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, 1, "cal-1", "Personal", "peacock"); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	_, err := repo.GetByID(ctx, 2, "cal-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for another user's calendar, got %v", err)
	}
}

func TestCalendarRepository_ListByUser(t *testing.T) {
	repo := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, 1, "cal-1", "Personal", "peacock"); err != nil {
		t.Fatalf("create calendar 1: %v", err)
	}
	if _, err := repo.Create(ctx, 1, "cal-2", "Work", "tomato"); err != nil {
		t.Fatalf("create calendar 2: %v", err)
	}
	if _, err := repo.Create(ctx, 2, "cal-3", "Other user", "sage"); err != nil {
		t.Fatalf("create calendar for other user: %v", err)
	}

	calendars, err := repo.ListByUser(ctx, 1)
	if err != nil {
		t.Fatalf("list by user: %v", err)
	}

	if len(calendars) != 2 {
		t.Fatalf("expected 2 calendars, got %d", len(calendars))
	}
	if calendars[0].ID != "cal-1" || calendars[1].ID != "cal-2" {
		t.Fatalf("expected calendars in creation order, got %+v", calendars)
	}
}

func TestCalendarRepository_Update(t *testing.T) {
	repo := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, 1, "cal-1", "Personal", "peacock"); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	updated, err := repo.Update(ctx, 1, "cal-1", "Renamed", "tomato")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "Renamed" || updated.Color != "tomato" {
		t.Fatalf("expected updated fields, got %+v", updated)
	}
}

func TestCalendarRepository_Update_NotFound(t *testing.T) {
	repo := newTestCalendarRepository(t)

	_, err := repo.Update(context.Background(), 1, "nope", "Renamed", "tomato")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_Update_ScopedToUser(t *testing.T) {
	repo := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, 1, "cal-1", "Personal", "peacock"); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	_, err := repo.Update(ctx, 2, "cal-1", "Renamed", "tomato")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound updating another user's calendar, got %v", err)
	}
}

func TestCalendarRepository_Delete(t *testing.T) {
	repo := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, 1, "cal-1", "Personal", "peacock"); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	if err := repo.Delete(ctx, 1, "cal-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := repo.GetByID(ctx, 1, "cal-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestCalendarRepository_Delete_NotFound(t *testing.T) {
	repo := newTestCalendarRepository(t)

	err := repo.Delete(context.Background(), 1, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_Delete_ScopedToUser(t *testing.T) {
	repo := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, 1, "cal-1", "Personal", "peacock"); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	err := repo.Delete(ctx, 2, "cal-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting another user's calendar, got %v", err)
	}

	if _, err := repo.GetByID(ctx, 1, "cal-1"); err != nil {
		t.Fatalf("expected calendar to still exist, got %v", err)
	}
}
