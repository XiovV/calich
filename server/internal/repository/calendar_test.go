package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/db"
)

// newTestCalendarRepository returns a CalendarRepository plus two real user
// ids to satisfy calendars.user_id's foreign key — SQLite enforces it, so
// tests can't use arbitrary literal ids the way they could before FK
// enforcement was turned on.
func newTestCalendarRepository(t *testing.T) (repo *CalendarRepository, userID, otherUserID, workspaceID, otherWorkspaceID int64) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	ctx := context.Background()

	users := NewUserRepository(sqlDB)
	user, err := users.Create(ctx, "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	other, err := users.Create(ctx, "user-b", "user-b@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	workspaces := NewWorkspaceRepository(sqlDB)
	workspace, err := workspaces.Create(ctx, "workspace-a", user.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaces.AddMember(ctx, workspace.ID, user.ID, WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}
	otherWorkspace, err := workspaces.Create(ctx, "workspace-b", other.ID)
	if err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	if err := workspaces.AddMember(ctx, otherWorkspace.ID, other.ID, WorkspaceRoleOwner); err != nil {
		t.Fatalf("add other workspace member: %v", err)
	}

	return NewCalendarRepository(sqlDB), user.ID, other.ID, workspace.ID, otherWorkspace.ID
}

func TestCalendarRepository_CreateAndGetByID(t *testing.T) {
	repo, userID, _, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	if created != (Calendar{ID: "cal-1", UserID: userID, WorkspaceID: workspaceID, Name: "Personal", Color: "peacock", CreatedAt: created.CreatedAt}) {
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
	repo, userID, _, _, _ := newTestCalendarRepository(t)

	_, err := repo.GetByID(context.Background(), userID, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_GetByID_ScopedToUser(t *testing.T) {
	repo, userID, otherUserID, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	_, err := repo.GetByID(ctx, otherUserID, "cal-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for another user's calendar, got %v", err)
	}
}

func TestCalendarRepository_ListByUser(t *testing.T) {
	repo, userID, otherUserID, workspaceID, otherWorkspaceID := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
		t.Fatalf("create calendar 1: %v", err)
	}
	if _, err := repo.Create(ctx, userID, workspaceID, "cal-2", CalendarFields{Name: "Work", Color: "tomato"}); err != nil {
		t.Fatalf("create calendar 2: %v", err)
	}
	if _, err := repo.Create(ctx, otherUserID, otherWorkspaceID, "cal-3", CalendarFields{Name: "Other user", Color: "sage"}); err != nil {
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
	repo, userID, _, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
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
	repo, userID, _, _, _ := newTestCalendarRepository(t)

	_, err := repo.Update(context.Background(), userID, "nope", CalendarFields{Name: "Renamed", Color: "tomato"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_Update_ScopedToUser(t *testing.T) {
	repo, userID, otherUserID, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	_, err := repo.Update(ctx, otherUserID, "cal-1", CalendarFields{Name: "Renamed", Color: "tomato"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound updating another user's calendar, got %v", err)
	}
}

func TestCalendarRepository_UpdateKeepAlarms(t *testing.T) {
	repo, userID, _, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()
	sourceURL := "https://example.com/feed.ics"

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Feed", Color: "peacock", SourceURL: &sourceURL}); err != nil {
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
	repo, userID, _, _, _ := newTestCalendarRepository(t)

	_, err := repo.UpdateKeepAlarms(context.Background(), userID, "nope", true)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_UpdateKeepAlarms_ScopedToUser(t *testing.T) {
	repo, userID, otherUserID, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()
	sourceURL := "https://example.com/feed.ics"

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Feed", Color: "peacock", SourceURL: &sourceURL}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	_, err := repo.UpdateKeepAlarms(ctx, otherUserID, "cal-1", true)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound updating another user's calendar, got %v", err)
	}
}

func TestCalendarRepository_RecordRefreshSuccess(t *testing.T) {
	repo, userID, _, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Feed", Color: "peacock"}); err != nil {
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
	feedName, feedColor := "Team Holidays", "#8E44ADFF"
	if err := repo.RecordRefreshSuccess(ctx, userID, "cal-1", RefreshSuccess{
		SyncedAt: syncedAt, ETag: &etag, LastModified: &lastModified, ContentHash: &hash,
		NextRefreshAt: nextRefreshAt, RefreshIntervalSeconds: &intervalSeconds,
		Name: "Team Holidays", Color: "#8E44ADFF", FeedName: &feedName, FeedColor: &feedColor,
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
	if got.Name != "Team Holidays" || got.Color != "#8E44ADFF" {
		t.Fatalf("expected Name/Color to be persisted, got %+v", got)
	}
	if got.FeedName == nil || *got.FeedName != feedName || got.FeedColor == nil || *got.FeedColor != feedColor {
		t.Fatalf("expected FeedName/FeedColor to be persisted, got %+v", got)
	}
}

func TestCalendarRepository_UpdateSourceURL(t *testing.T) {
	repo, userID, _, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()
	sourceURL := "https://old.example.com/feed.ics"

	created, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Feed", Color: "peacock", SourceURL: &sourceURL})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	etag, hash := `"v1"`, "deadbeef"
	if err := repo.RecordRefreshSuccess(ctx, userID, created.ID, RefreshSuccess{
		SyncedAt: time.Now(), ETag: &etag, ContentHash: &hash, NextRefreshAt: time.Now().Add(time.Hour),
		Name: "Feed", Color: "peacock",
	}); err != nil {
		t.Fatalf("seed refresh success: %v", err)
	}

	newURL := "https://new.example.com/feed.ics"
	updated, err := repo.UpdateSourceURL(ctx, userID, created.ID, newURL)
	if err != nil {
		t.Fatalf("update source url: %v", err)
	}
	if updated.SourceURL == nil || *updated.SourceURL != newURL {
		t.Fatalf("expected SourceURL updated, got %+v", updated.SourceURL)
	}
	if updated.ETag != nil || updated.LastModified != nil || updated.ContentHash != nil {
		t.Fatalf("expected the conditional-GET validators reset, got %+v", updated)
	}
}

func TestCalendarRepository_UpdateSourceURL_NotFound(t *testing.T) {
	repo, userID, _, _, _ := newTestCalendarRepository(t)

	_, err := repo.UpdateSourceURL(context.Background(), userID, "nope", "https://example.com/feed.ics")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_UpdateSourceURL_ScopedToUser(t *testing.T) {
	repo, userID, otherUserID, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()
	sourceURL := "https://example.com/feed.ics"

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Feed", Color: "peacock", SourceURL: &sourceURL}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	_, err := repo.UpdateSourceURL(ctx, otherUserID, "cal-1", "https://new.example.com/feed.ics")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound updating another user's calendar, got %v", err)
	}
}

func TestCalendarRepository_RecordRefreshSuccess_NotFound(t *testing.T) {
	repo, userID, _, _, _ := newTestCalendarRepository(t)

	err := repo.RecordRefreshSuccess(context.Background(), userID, "nope", RefreshSuccess{SyncedAt: time.Now()})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_RecordRefreshFailure(t *testing.T) {
	repo, userID, _, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Feed", Color: "peacock"}); err != nil {
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
	repo, userID, _, _, _ := newTestCalendarRepository(t)

	err := repo.RecordRefreshFailure(context.Background(), userID, "nope", RefreshFailure{ErrorClass: "retrying", ErrorMessage: "x", FailureCount: 1, NextRefreshAt: time.Now()})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_ListDueForRefresh(t *testing.T) {
	repo, userID, _, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	sourceURL := "https://example.com/feed.ics"
	due, err := repo.Create(ctx, userID, workspaceID, "cal-due", CalendarFields{Name: "Due", Color: "peacock", SourceURL: &sourceURL})
	if err != nil {
		t.Fatalf("create due calendar: %v", err)
	}
	notYetDue, err := repo.Create(ctx, userID, workspaceID, "cal-not-due", CalendarFields{Name: "Not due", Color: "peacock", SourceURL: &sourceURL})
	if err != nil {
		t.Fatalf("create not-due calendar: %v", err)
	}
	if _, err := repo.Create(ctx, userID, workspaceID, "cal-ordinary", CalendarFields{Name: "Ordinary", Color: "peacock"}); err != nil {
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
	repo, userID, _, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
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
	repo, userID, _, _, _ := newTestCalendarRepository(t)

	err := repo.Delete(context.Background(), userID, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_Delete_ScopedToUser(t *testing.T) {
	repo, userID, otherUserID, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
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

func TestCalendarRepository_TransferOwnershipOne(t *testing.T) {
	repo, userID, otherUserID, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
		t.Fatalf("create cal-1: %v", err)
	}
	if _, err := repo.Create(ctx, userID, workspaceID, "cal-2", CalendarFields{Name: "Family", Color: "blue"}); err != nil {
		t.Fatalf("create cal-2: %v", err)
	}

	if err := repo.TransferOwnershipOne(ctx, userID, "cal-1", otherUserID); err != nil {
		t.Fatalf("transfer ownership: %v", err)
	}

	if _, err := repo.GetByID(ctx, userID, "cal-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cal-1 to no longer belong to the old owner, got %v", err)
	}
	transferred, err := repo.GetByID(ctx, otherUserID, "cal-1")
	if err != nil {
		t.Fatalf("expected cal-1 to belong to the new owner: %v", err)
	}
	if transferred.Name != "Personal" {
		t.Fatalf("expected the calendar's other fields to survive the transfer, got %+v", transferred)
	}
	if _, err := repo.GetByID(ctx, userID, "cal-2"); err != nil {
		t.Fatalf("expected cal-2, untouched by the single-calendar transfer, to still belong to the old owner: %v", err)
	}
}

func TestCalendarRepository_TransferOwnershipOne_UnknownCalendar_ReturnsErrNotFound(t *testing.T) {
	repo, userID, otherUserID, _, _ := newTestCalendarRepository(t)

	if err := repo.TransferOwnershipOne(context.Background(), userID, "ghost", otherUserID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound transferring a nonexistent calendar, got %v", err)
	}
}

func TestCalendarRepository_ListSharedWithUser(t *testing.T) {
	ctx := context.Background()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := NewUserRepository(sqlDB)
	owner, err := users.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := users.Create(ctx, "other", "other@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	workspaces := NewWorkspaceRepository(sqlDB)
	workspace, err := workspaces.Create(ctx, "workspace-a", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaces.AddMember(ctx, workspace.ID, owner.ID, WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}

	repo := NewCalendarRepository(sqlDB)
	owned, err := repo.Create(ctx, owner.ID, workspace.ID, "cal-owned", CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create owned calendar: %v", err)
	}
	shared, err := repo.Create(ctx, owner.ID, workspace.ID, "cal-shared", CalendarFields{Name: "Family", Color: "sage"})
	if err != nil {
		t.Fatalf("create shared calendar: %v", err)
	}

	shares := NewCalendarShareRepository(sqlDB)
	if _, err := shares.Upsert(ctx, shared.ID, other.ID, RoleEditor); err != nil {
		t.Fatalf("upsert share: %v", err)
	}

	result, err := repo.ListSharedWithUser(ctx, other.ID)
	if err != nil {
		t.Fatalf("list shared with user: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected exactly one shared calendar, got %d: %+v", len(result), result)
	}
	if result[0].ID != shared.ID || result[0].Role != RoleEditor {
		t.Fatalf("unexpected result: %+v", result[0])
	}

	// owned isn't otherUserID's own and carries no Share, so it must not
	// appear — sanity-checking the join doesn't leak every Calendar.
	for _, c := range result {
		if c.ID == owned.ID {
			t.Fatalf("owner-only calendar %q leaked into another user's shared list", owned.ID)
		}
	}
}
