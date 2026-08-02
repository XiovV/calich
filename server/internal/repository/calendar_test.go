package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
)

// newTestCalendarRepository returns a CalendarRepository plus two real user
// ids to satisfy calendars.user_id's foreign key — SQLite enforces it, so
// tests can't use arbitrary literal ids the way they could before FK
// enforcement was turned on.
func newTestCalendarRepository(t *testing.T) (repo *CalendarRepository, userID, otherUserID int64) {
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

	return NewCalendarRepository(sqlDB), user.ID, other.ID
}

func TestCalendarRepository_CreateAndGetByID(t *testing.T) {
	repo, userID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, userID, "cal-1", "Personal", "peacock")
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	if created != (Calendar{ID: "cal-1", UserID: userID, Name: "Personal", Color: "peacock", CreatedAt: created.CreatedAt}) {
		t.Fatalf("unexpected created calendar: %+v", created)
	}

	fetched, err := repo.GetByID(ctx, userID, "cal-1")
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if fetched != created {
		t.Fatalf("expected fetched calendar %+v to equal created calendar %+v", fetched, created)
	}
}

func TestCalendarRepository_GetByID_NotFound(t *testing.T) {
	repo, userID, _ := newTestCalendarRepository(t)

	_, err := repo.GetByID(context.Background(), userID, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_GetByID_ScopedToUser(t *testing.T) {
	repo, userID, otherUserID := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, "cal-1", "Personal", "peacock"); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	_, err := repo.GetByID(ctx, otherUserID, "cal-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for another user's calendar, got %v", err)
	}
}

func TestCalendarRepository_ListByUser(t *testing.T) {
	repo, userID, otherUserID := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, "cal-1", "Personal", "peacock"); err != nil {
		t.Fatalf("create calendar 1: %v", err)
	}
	if _, err := repo.Create(ctx, userID, "cal-2", "Work", "tomato"); err != nil {
		t.Fatalf("create calendar 2: %v", err)
	}
	if _, err := repo.Create(ctx, otherUserID, "cal-3", "Other user", "sage"); err != nil {
		t.Fatalf("create calendar for other user: %v", err)
	}

	calendars, err := repo.ListByUser(ctx, userID)
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
	repo, userID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, "cal-1", "Personal", "peacock"); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	updated, err := repo.Update(ctx, userID, "cal-1", "Renamed", "tomato")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "Renamed" || updated.Color != "tomato" {
		t.Fatalf("expected updated fields, got %+v", updated)
	}
}

func TestCalendarRepository_Update_NotFound(t *testing.T) {
	repo, userID, _ := newTestCalendarRepository(t)

	_, err := repo.Update(context.Background(), userID, "nope", "Renamed", "tomato")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_Update_ScopedToUser(t *testing.T) {
	repo, userID, otherUserID := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, "cal-1", "Personal", "peacock"); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	_, err := repo.Update(ctx, otherUserID, "cal-1", "Renamed", "tomato")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound updating another user's calendar, got %v", err)
	}
}

func TestCalendarRepository_Delete(t *testing.T) {
	repo, userID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, "cal-1", "Personal", "peacock"); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	if err := repo.Delete(ctx, userID, "cal-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := repo.GetByID(ctx, userID, "cal-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestCalendarRepository_Delete_NotFound(t *testing.T) {
	repo, userID, _ := newTestCalendarRepository(t)

	err := repo.Delete(context.Background(), userID, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_Delete_ScopedToUser(t *testing.T) {
	repo, userID, otherUserID := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, "cal-1", "Personal", "peacock"); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	err := repo.Delete(ctx, otherUserID, "cal-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting another user's calendar, got %v", err)
	}

	if _, err := repo.GetByID(ctx, userID, "cal-1"); err != nil {
		t.Fatalf("expected calendar to still exist, got %v", err)
	}
}
