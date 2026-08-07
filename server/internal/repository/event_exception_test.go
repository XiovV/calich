package repository

import (
	"context"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
)

// newTestEventExceptionRepository returns an EventRepository and
// EventExceptionRepository sharing one in-memory database, plus a real user id
// and calendar id to satisfy events' foreign keys.
func newTestEventExceptionRepository(t *testing.T) (repo *EventRepository, exceptions *EventExceptionRepository, userID int64, calendarID string) {
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

	calendars := NewCalendarRepository(sqlDB)
	cal, err := calendars.Create(context.Background(), user.ID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	return NewEventRepository(sqlDB), NewEventExceptionRepository(sqlDB), user.ID, cal.ID
}

func TestEventExceptionRepository_AddAndListByParentIDs(t *testing.T) {
	repo, exceptions, userID, calendarID := newTestEventExceptionRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "master", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	occurrenceStart := mustParseTime(t, "2026-01-02T09:00:00Z")
	if err := exceptions.Add(ctx, "master", occurrenceStart); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	byParent, err := exceptions.ListByParentIDs(ctx, []string{"master"})
	if err != nil {
		t.Fatalf("list by parent ids: %v", err)
	}
	if len(byParent["master"]) != 1 || !byParent["master"][0].Equal(occurrenceStart) {
		t.Fatalf("expected one exception for master, got %+v", byParent)
	}
}

func TestEventExceptionRepository_ListByParentIDs_EmptyInput(t *testing.T) {
	_, exceptions, _, _ := newTestEventExceptionRepository(t)

	byParent, err := exceptions.ListByParentIDs(context.Background(), nil)
	if err != nil {
		t.Fatalf("list by parent ids: %v", err)
	}
	if len(byParent) != 0 {
		t.Fatalf("expected no exceptions, got %+v", byParent)
	}
}

func TestEventExceptionRepository_ReparentFrom(t *testing.T) {
	repo, exceptions, userID, calendarID := newTestEventExceptionRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "old-master", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	mustCreateEvent(t, repo, "new-master", userID, calendarID, "2026-01-05T09:00:00Z", "2026-01-05T10:00:00Z")

	beforeSplit := mustParseTime(t, "2026-01-02T09:00:00Z")
	afterSplit := mustParseTime(t, "2026-01-06T09:00:00Z")
	if err := exceptions.Add(ctx, "old-master", beforeSplit); err != nil {
		t.Fatalf("add exception: %v", err)
	}
	if err := exceptions.Add(ctx, "old-master", afterSplit); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	if err := exceptions.ReparentFrom(ctx, "old-master", "new-master", mustParseTime(t, "2026-01-05T00:00:00Z")); err != nil {
		t.Fatalf("reparent: %v", err)
	}

	byParent, err := exceptions.ListByParentIDs(ctx, []string{"old-master", "new-master"})
	if err != nil {
		t.Fatalf("list by parent ids: %v", err)
	}
	if len(byParent["old-master"]) != 1 || !byParent["old-master"][0].Equal(beforeSplit) {
		t.Fatalf("expected old-master to keep only the before-split exception, got %+v", byParent["old-master"])
	}
	if len(byParent["new-master"]) != 1 || !byParent["new-master"][0].Equal(afterSplit) {
		t.Fatalf("expected new-master to gain the after-split exception, got %+v", byParent["new-master"])
	}
}

func TestEventExceptionRepository_DeleteByParentID(t *testing.T) {
	repo, exceptions, userID, calendarID := newTestEventExceptionRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "master", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	if err := exceptions.Add(ctx, "master", mustParseTime(t, "2026-01-02T09:00:00Z")); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	if err := exceptions.DeleteByParentID(ctx, "master"); err != nil {
		t.Fatalf("delete by parent id: %v", err)
	}

	byParent, err := exceptions.ListByParentIDs(ctx, []string{"master"})
	if err != nil {
		t.Fatalf("list by parent ids: %v", err)
	}
	if len(byParent["master"]) != 0 {
		t.Fatalf("expected no exceptions after delete, got %+v", byParent["master"])
	}
}

func TestEventExceptionRepository_CascadeDeletesWhenParentDeleted(t *testing.T) {
	repo, exceptions, userID, calendarID := newTestEventExceptionRepository(t)
	ctx := context.Background()

	mustCreateEvent(t, repo, "master", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	if err := exceptions.Add(ctx, "master", mustParseTime(t, "2026-01-02T09:00:00Z")); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	if err := repo.Delete(ctx, "master"); err != nil {
		t.Fatalf("delete master: %v", err)
	}

	byParent, err := exceptions.ListByParentIDs(ctx, []string{"master"})
	if err != nil {
		t.Fatalf("list by parent ids: %v", err)
	}
	if len(byParent["master"]) != 0 {
		t.Fatalf("expected exceptions to be cascade-deleted with their master, got %+v", byParent["master"])
	}
}
