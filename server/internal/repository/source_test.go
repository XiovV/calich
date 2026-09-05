package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/db"
)

// newTestSourceRepository returns a SourceRepository plus a CalendarRepository
// and two real user/workspace ids to create Calendars against, the same
// shape newTestCalendarRepository already gives calendar_test.go.
func newTestSourceRepository(t *testing.T) (sources *SourceRepository, calendars *CalendarRepository, userID, otherUserID, workspaceID int64) {
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

	return NewSourceRepository(sqlDB), NewCalendarRepository(sqlDB), user.ID, other.ID, workspace.ID
}

// createSubscribedCalendar creates an ordinary Calendar and a Subscription
// Source on it in one step — the shape every subscription-flavored test
// here starts from.
func createSubscribedCalendar(t *testing.T, ctx context.Context, calendars *CalendarRepository, sources *SourceRepository, userID, workspaceID int64, id, sourceURL string) Calendar {
	t.Helper()
	calendar, err := calendars.Create(ctx, userID, workspaceID, id, CalendarFields{Name: "Feed", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	if _, err := sources.Create(ctx, id, SourceFields{Kind: SourceKindSubscription, Mode: SourceModeReadOnly, SourceURL: &sourceURL}); err != nil {
		t.Fatalf("create source: %v", err)
	}
	return calendar
}

func TestSourceRepository_CreateAndGetByCalendarID(t *testing.T) {
	sources, calendars, userID, _, workspaceID := newTestSourceRepository(t)
	ctx := context.Background()
	sourceURL := "https://example.com/feed.ics"

	createSubscribedCalendar(t, ctx, calendars, sources, userID, workspaceID, "cal-1", sourceURL)

	got, err := sources.GetByCalendarID(ctx, "cal-1")
	if err != nil {
		t.Fatalf("get by calendar id: %v", err)
	}
	if got.Kind != SourceKindSubscription {
		t.Fatalf("expected kind subscription, got %q", got.Kind)
	}
	if got.Mode != SourceModeReadOnly {
		t.Fatalf("expected mode read_only, got %q", got.Mode)
	}
	if got.SourceURL == nil || *got.SourceURL != sourceURL {
		t.Fatalf("expected SourceURL %q, got %v", sourceURL, got.SourceURL)
	}
	if got.KeepAlarms {
		t.Fatalf("expected KeepAlarms false by default, got true")
	}
}

func TestSourceRepository_GetByCalendarID_NotFound(t *testing.T) {
	sources, calendars, userID, _, workspaceID := newTestSourceRepository(t)
	ctx := context.Background()

	if _, err := calendars.Create(ctx, userID, workspaceID, "cal-ordinary", CalendarFields{Name: "Ordinary", Color: "peacock"}); err != nil {
		t.Fatalf("create ordinary calendar: %v", err)
	}

	_, err := sources.GetByCalendarID(ctx, "cal-ordinary")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an ordinary calendar, got %v", err)
	}
}

func TestSourceRepository_ListByCalendarIDs(t *testing.T) {
	sources, calendars, userID, _, workspaceID := newTestSourceRepository(t)
	ctx := context.Background()
	sourceURL := "https://example.com/feed.ics"

	createSubscribedCalendar(t, ctx, calendars, sources, userID, workspaceID, "cal-sub", sourceURL)
	if _, err := calendars.Create(ctx, userID, workspaceID, "cal-ordinary", CalendarFields{Name: "Ordinary", Color: "peacock"}); err != nil {
		t.Fatalf("create ordinary calendar: %v", err)
	}

	result, err := sources.ListByCalendarIDs(ctx, []string{"cal-sub", "cal-ordinary", "cal-missing"})
	if err != nil {
		t.Fatalf("list by calendar ids: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected exactly one Source, got %d: %+v", len(result), result)
	}
	if _, ok := result["cal-sub"]; !ok {
		t.Fatalf("expected cal-sub's Source in the result, got %+v", result)
	}
}

func TestSourceRepository_UpdateKeepAlarms(t *testing.T) {
	sources, calendars, userID, _, workspaceID := newTestSourceRepository(t)
	ctx := context.Background()

	createSubscribedCalendar(t, ctx, calendars, sources, userID, workspaceID, "cal-1", "https://example.com/feed.ics")

	if err := sources.UpdateKeepAlarms(ctx, userID, "cal-1", true); err != nil {
		t.Fatalf("update keep_alarms: %v", err)
	}
	got, err := sources.GetByCalendarID(ctx, "cal-1")
	if err != nil {
		t.Fatalf("get by calendar id: %v", err)
	}
	if !got.KeepAlarms {
		t.Fatalf("expected KeepAlarms true, got %+v", got)
	}

	if err := sources.UpdateKeepAlarms(ctx, userID, "cal-1", false); err != nil {
		t.Fatalf("update keep_alarms back to false: %v", err)
	}
	got, err = sources.GetByCalendarID(ctx, "cal-1")
	if err != nil {
		t.Fatalf("get by calendar id: %v", err)
	}
	if got.KeepAlarms {
		t.Fatalf("expected KeepAlarms false, got %+v", got)
	}
}

func TestSourceRepository_UpdateKeepAlarms_NotFound(t *testing.T) {
	sources, _, userID, _, _ := newTestSourceRepository(t)

	err := sources.UpdateKeepAlarms(context.Background(), userID, "nope", true)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSourceRepository_UpdateKeepAlarms_ScopedToUser(t *testing.T) {
	sources, calendars, userID, otherUserID, workspaceID := newTestSourceRepository(t)
	ctx := context.Background()

	createSubscribedCalendar(t, ctx, calendars, sources, userID, workspaceID, "cal-1", "https://example.com/feed.ics")

	err := sources.UpdateKeepAlarms(ctx, otherUserID, "cal-1", true)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound updating another user's calendar's source, got %v", err)
	}
}

func TestSourceRepository_RecordRefreshSuccess(t *testing.T) {
	sources, calendars, userID, _, workspaceID := newTestSourceRepository(t)
	ctx := context.Background()

	createSubscribedCalendar(t, ctx, calendars, sources, userID, workspaceID, "cal-1", "https://example.com/feed.ics")
	if err := sources.RecordRefreshFailure(ctx, userID, "cal-1", RefreshFailure{
		ErrorClass: "retrying", ErrorMessage: "timeout", FailureCount: 2, NextRefreshAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed failure: %v", err)
	}

	etag, lastModified, hash := `"abc"`, "Wed, 21 Oct 2015 07:28:00 GMT", "deadbeef"
	syncedAt := time.Now().UTC().Truncate(time.Second)
	nextRefreshAt := syncedAt.Add(time.Hour)
	intervalSeconds := 3600
	feedName, feedColor := "Team Holidays", "#8E44ADFF"
	if err := sources.RecordRefreshSuccess(ctx, userID, "cal-1", RefreshSuccess{
		SyncedAt: syncedAt, ETag: &etag, LastModified: &lastModified, ContentHash: &hash,
		NextRefreshAt: nextRefreshAt, RefreshIntervalSeconds: &intervalSeconds,
		FeedName: &feedName, FeedColor: &feedColor,
	}); err != nil {
		t.Fatalf("record refresh success: %v", err)
	}

	got, err := sources.GetByCalendarID(ctx, "cal-1")
	if err != nil {
		t.Fatalf("get by calendar id: %v", err)
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
	if got.FeedName == nil || *got.FeedName != feedName || got.FeedColor == nil || *got.FeedColor != feedColor {
		t.Fatalf("expected FeedName/FeedColor to be persisted, got %+v", got)
	}
}

func TestSourceRepository_RecordRefreshSuccess_NotFound(t *testing.T) {
	sources, _, userID, _, _ := newTestSourceRepository(t)

	err := sources.RecordRefreshSuccess(context.Background(), userID, "nope", RefreshSuccess{SyncedAt: time.Now()})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSourceRepository_UpdateSourceURL(t *testing.T) {
	sources, calendars, userID, _, workspaceID := newTestSourceRepository(t)
	ctx := context.Background()

	createSubscribedCalendar(t, ctx, calendars, sources, userID, workspaceID, "cal-1", "https://old.example.com/feed.ics")

	etag, hash := `"v1"`, "deadbeef"
	if err := sources.RecordRefreshSuccess(ctx, userID, "cal-1", RefreshSuccess{
		SyncedAt: time.Now(), ETag: &etag, ContentHash: &hash, NextRefreshAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed refresh success: %v", err)
	}

	newURL := "https://new.example.com/feed.ics"
	if err := sources.UpdateSourceURL(ctx, userID, "cal-1", newURL); err != nil {
		t.Fatalf("update source url: %v", err)
	}

	got, err := sources.GetByCalendarID(ctx, "cal-1")
	if err != nil {
		t.Fatalf("get by calendar id: %v", err)
	}
	if got.SourceURL == nil || *got.SourceURL != newURL {
		t.Fatalf("expected SourceURL updated, got %+v", got.SourceURL)
	}
	if got.ETag != nil || got.LastModified != nil || got.ContentHash != nil {
		t.Fatalf("expected the conditional-GET validators reset, got %+v", got)
	}
}

func TestSourceRepository_UpdateSourceURL_NotFound(t *testing.T) {
	sources, _, userID, _, _ := newTestSourceRepository(t)

	err := sources.UpdateSourceURL(context.Background(), userID, "nope", "https://example.com/feed.ics")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSourceRepository_UpdateSourceURL_ScopedToUser(t *testing.T) {
	sources, calendars, userID, otherUserID, workspaceID := newTestSourceRepository(t)
	ctx := context.Background()

	createSubscribedCalendar(t, ctx, calendars, sources, userID, workspaceID, "cal-1", "https://example.com/feed.ics")

	err := sources.UpdateSourceURL(ctx, otherUserID, "cal-1", "https://new.example.com/feed.ics")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound updating another user's calendar's source, got %v", err)
	}
}

func TestSourceRepository_RecordRefreshFailure(t *testing.T) {
	sources, calendars, userID, _, workspaceID := newTestSourceRepository(t)
	ctx := context.Background()

	createSubscribedCalendar(t, ctx, calendars, sources, userID, workspaceID, "cal-1", "https://example.com/feed.ics")

	next := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	if err := sources.RecordRefreshFailure(ctx, userID, "cal-1", RefreshFailure{
		ErrorClass: "needs_attention", ErrorMessage: "the calendar feed rejected the credentials", FailureCount: 1, NextRefreshAt: next,
	}); err != nil {
		t.Fatalf("record refresh failure: %v", err)
	}

	got, err := sources.GetByCalendarID(ctx, "cal-1")
	if err != nil {
		t.Fatalf("get by calendar id: %v", err)
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

func TestSourceRepository_RecordRefreshFailure_NotFound(t *testing.T) {
	sources, _, userID, _, _ := newTestSourceRepository(t)

	err := sources.RecordRefreshFailure(context.Background(), userID, "nope", RefreshFailure{ErrorClass: "retrying", ErrorMessage: "x", FailureCount: 1, NextRefreshAt: time.Now()})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSourceRepository_ListDueForRefresh(t *testing.T) {
	sources, calendars, userID, _, workspaceID := newTestSourceRepository(t)
	ctx := context.Background()

	due := createSubscribedCalendar(t, ctx, calendars, sources, userID, workspaceID, "cal-due", "https://example.com/feed.ics")
	notYetDue := createSubscribedCalendar(t, ctx, calendars, sources, userID, workspaceID, "cal-not-due", "https://example.com/feed.ics")
	if _, err := calendars.Create(ctx, userID, workspaceID, "cal-ordinary", CalendarFields{Name: "Ordinary", Color: "peacock"}); err != nil {
		t.Fatalf("create ordinary calendar: %v", err)
	}

	now := time.Now().UTC()
	if err := sources.ScheduleNextRefresh(ctx, userID, due.ID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("schedule due: %v", err)
	}
	if err := sources.ScheduleNextRefresh(ctx, userID, notYetDue.ID, now.Add(time.Hour)); err != nil {
		t.Fatalf("schedule not due: %v", err)
	}

	results, err := sources.ListDueForRefresh(ctx, now)
	if err != nil {
		t.Fatalf("list due for refresh: %v", err)
	}
	if len(results) != 1 || results[0].CalendarID != due.ID {
		t.Fatalf("expected only %q due, got %+v", due.ID, results)
	}
	if results[0].UserID != userID {
		t.Fatalf("expected UserID %d, got %d", userID, results[0].UserID)
	}
}

func TestSourceRepository_ScheduleNextRefresh_NotFound(t *testing.T) {
	sources, _, userID, _, _ := newTestSourceRepository(t)

	err := sources.ScheduleNextRefresh(context.Background(), userID, "nope", time.Now())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
