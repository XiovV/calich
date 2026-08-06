package repository

import (
	"context"
	"errors"
	"testing"
	"time"

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

	created, err := repo.Create(ctx, userID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"})
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

	if _, err := repo.Create(ctx, userID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
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

	if _, err := repo.Create(ctx, userID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
		t.Fatalf("create calendar 1: %v", err)
	}
	if _, err := repo.Create(ctx, userID, "cal-2", CalendarFields{Name: "Work", Color: "tomato"}); err != nil {
		t.Fatalf("create calendar 2: %v", err)
	}
	if _, err := repo.Create(ctx, otherUserID, "cal-3", CalendarFields{Name: "Other user", Color: "sage"}); err != nil {
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

	if _, err := repo.Create(ctx, userID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	updated, err := repo.Update(ctx, userID, "cal-1", CalendarFields{Name: "Renamed", Color: "tomato"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "Renamed" || updated.Color != "tomato" {
		t.Fatalf("expected updated fields, got %+v", updated)
	}
}

func TestCalendarRepository_Update_NotFound(t *testing.T) {
	repo, userID, _ := newTestCalendarRepository(t)

	_, err := repo.Update(context.Background(), userID, "nope", CalendarFields{Name: "Renamed", Color: "tomato"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_Update_ScopedToUser(t *testing.T) {
	repo, userID, otherUserID := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	_, err := repo.Update(ctx, otherUserID, "cal-1", CalendarFields{Name: "Renamed", Color: "tomato"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound updating another user's calendar, got %v", err)
	}
}

func TestCalendarRepository_UpdateKeepAlarms(t *testing.T) {
	repo, userID, _ := newTestCalendarRepository(t)
	ctx := context.Background()
	sourceURL := "https://example.com/feed.ics"

	if _, err := repo.Create(ctx, userID, "cal-1", CalendarFields{Name: "Feed", Color: "peacock", SourceURL: &sourceURL}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	updated, err := repo.UpdateKeepAlarms(ctx, userID, "cal-1", true)
	if err != nil {
		t.Fatalf("update keep_alarms: %v", err)
	}
	if !updated.KeepAlarms {
		t.Fatalf("expected KeepAlarms true, got %+v", updated)
	}

	updated, err = repo.UpdateKeepAlarms(ctx, userID, "cal-1", false)
	if err != nil {
		t.Fatalf("update keep_alarms back to false: %v", err)
	}
	if updated.KeepAlarms {
		t.Fatalf("expected KeepAlarms false, got %+v", updated)
	}
}

func TestCalendarRepository_UpdateKeepAlarms_NotFound(t *testing.T) {
	repo, userID, _ := newTestCalendarRepository(t)

	_, err := repo.UpdateKeepAlarms(context.Background(), userID, "nope", true)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_UpdateKeepAlarms_ScopedToUser(t *testing.T) {
	repo, userID, otherUserID := newTestCalendarRepository(t)
	ctx := context.Background()
	sourceURL := "https://example.com/feed.ics"

	if _, err := repo.Create(ctx, userID, "cal-1", CalendarFields{Name: "Feed", Color: "peacock", SourceURL: &sourceURL}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	_, err := repo.UpdateKeepAlarms(ctx, otherUserID, "cal-1", true)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound updating another user's calendar, got %v", err)
	}
}

func TestCalendarRepository_RecordRefreshSuccess(t *testing.T) {
	repo, userID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, "cal-1", CalendarFields{Name: "Feed", Color: "peacock"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	if err := repo.RecordRefreshFailure(ctx, userID, "cal-1", RefreshFailure{
		ErrorClass: "retrying", ErrorMessage: "timeout", FailureCount: 2, NextRefreshAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed failure: %v", err)
	}

	etag, lastModified, hash := `"abc"`, "Wed, 21 Oct 2015 07:28:00 GMT", "deadbeef"
	syncedAt := time.Now().UTC().Truncate(time.Second)
	nextRefreshAt := syncedAt.Add(time.Hour)
	intervalSeconds := 3600
	if err := repo.RecordRefreshSuccess(ctx, userID, "cal-1", RefreshSuccess{
		SyncedAt: syncedAt, ETag: &etag, LastModified: &lastModified, ContentHash: &hash,
		NextRefreshAt: nextRefreshAt, RefreshIntervalSeconds: &intervalSeconds,
	}); err != nil {
		t.Fatalf("record refresh success: %v", err)
	}

	got, err := repo.GetByID(ctx, userID, "cal-1")
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.LastSyncedAt == nil || !got.LastSyncedAt.Equal(syncedAt) {
		t.Fatalf("expected LastSyncedAt %v, got %v", syncedAt, got.LastSyncedAt)
	}
	if got.ETag == nil || *got.ETag != etag {
		t.Fatalf("expected ETag %q, got %v", etag, got.ETag)
	}
	if got.LastModified == nil || *got.LastModified != lastModified {
		t.Fatalf("expected LastModified %q, got %v", lastModified, got.LastModified)
	}
	if got.ContentHash == nil || *got.ContentHash != hash {
		t.Fatalf("expected ContentHash %q, got %v", hash, got.ContentHash)
	}
	if got.NextRefreshAt == nil || !got.NextRefreshAt.Equal(nextRefreshAt) {
		t.Fatalf("expected NextRefreshAt %v, got %v", nextRefreshAt, got.NextRefreshAt)
	}
	if got.RefreshIntervalSeconds == nil || *got.RefreshIntervalSeconds != intervalSeconds {
		t.Fatalf("expected RefreshIntervalSeconds %d, got %v", intervalSeconds, got.RefreshIntervalSeconds)
	}
	if got.FailureCount != 0 {
		t.Fatalf("expected a success to reset FailureCount to 0, got %d", got.FailureCount)
	}
	if got.ErrorClass != nil || got.ErrorMessage != nil {
		t.Fatalf("expected a success to clear the error state, got class=%v message=%v", got.ErrorClass, got.ErrorMessage)
	}
}

func TestCalendarRepository_RecordRefreshSuccess_NotFound(t *testing.T) {
	repo, userID, _ := newTestCalendarRepository(t)

	err := repo.RecordRefreshSuccess(context.Background(), userID, "nope", RefreshSuccess{SyncedAt: time.Now()})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_RecordRefreshFailure(t *testing.T) {
	repo, userID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, "cal-1", CalendarFields{Name: "Feed", Color: "peacock"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	next := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	if err := repo.RecordRefreshFailure(ctx, userID, "cal-1", RefreshFailure{
		ErrorClass: "needs_attention", ErrorMessage: "the calendar feed rejected the credentials", FailureCount: 1, NextRefreshAt: next,
	}); err != nil {
		t.Fatalf("record refresh failure: %v", err)
	}

	got, err := repo.GetByID(ctx, userID, "cal-1")
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.FailureCount != 1 {
		t.Fatalf("expected FailureCount 1, got %d", got.FailureCount)
	}
	if got.ErrorClass == nil || *got.ErrorClass != "needs_attention" {
		t.Fatalf("expected ErrorClass needs_attention, got %v", got.ErrorClass)
	}
	if got.NextRefreshAt == nil || !got.NextRefreshAt.Equal(next) {
		t.Fatalf("expected NextRefreshAt %v, got %v", next, got.NextRefreshAt)
	}
	if got.LastSyncedAt != nil {
		t.Fatalf("expected a failure to leave LastSyncedAt untouched, got %v", got.LastSyncedAt)
	}
}

func TestCalendarRepository_RecordRefreshFailure_NotFound(t *testing.T) {
	repo, userID, _ := newTestCalendarRepository(t)

	err := repo.RecordRefreshFailure(context.Background(), userID, "nope", RefreshFailure{ErrorClass: "retrying", ErrorMessage: "x", FailureCount: 1, NextRefreshAt: time.Now()})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_ListDueForRefresh(t *testing.T) {
	repo, userID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	sourceURL := "https://example.com/feed.ics"
	due, err := repo.Create(ctx, userID, "cal-due", CalendarFields{Name: "Due", Color: "peacock", SourceURL: &sourceURL})
	if err != nil {
		t.Fatalf("create due calendar: %v", err)
	}
	notYetDue, err := repo.Create(ctx, userID, "cal-not-due", CalendarFields{Name: "Not due", Color: "peacock", SourceURL: &sourceURL})
	if err != nil {
		t.Fatalf("create not-due calendar: %v", err)
	}
	if _, err := repo.Create(ctx, userID, "cal-ordinary", CalendarFields{Name: "Ordinary", Color: "peacock"}); err != nil {
		t.Fatalf("create ordinary calendar: %v", err)
	}

	now := time.Now().UTC()
	if err := repo.ScheduleNextRefresh(ctx, userID, due.ID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("schedule due: %v", err)
	}
	if err := repo.ScheduleNextRefresh(ctx, userID, notYetDue.ID, now.Add(time.Hour)); err != nil {
		t.Fatalf("schedule not due: %v", err)
	}

	results, err := repo.ListDueForRefresh(ctx, now)
	if err != nil {
		t.Fatalf("list due for refresh: %v", err)
	}
	if len(results) != 1 || results[0].ID != due.ID {
		t.Fatalf("expected only %q due, got %+v", due.ID, results)
	}
}

func TestCalendarRepository_Delete(t *testing.T) {
	repo, userID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
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

	if _, err := repo.Create(ctx, userID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
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
