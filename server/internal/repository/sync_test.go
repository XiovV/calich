package repository

import (
	"context"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
)

func newTestSyncRepository(t *testing.T) (sync *SyncRepository, events *EventRepository, userID int64, calendarID string) {
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
	cal, err := calendars.Create(context.Background(), user.ID, "cal-1", "Personal", "peacock")
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	return NewSyncRepository(sqlDB), NewEventRepository(sqlDB), user.ID, cal.ID
}

func TestSyncRepository_NextChangeSeq_Increments(t *testing.T) {
	sync, _, _, _ := newTestSyncRepository(t)
	ctx := context.Background()

	first, err := sync.NextChangeSeq(ctx)
	if err != nil {
		t.Fatalf("next change seq: %v", err)
	}
	second, err := sync.NextChangeSeq(ctx)
	if err != nil {
		t.Fatalf("next change seq: %v", err)
	}
	if second != first+1 {
		t.Fatalf("expected consecutive values, got %d then %d", first, second)
	}
}

func TestSyncRepository_CTag_ReflectsLatestWriteAcrossLiveAndTombstoned(t *testing.T) {
	sync, events, userID, calendarID := newTestSyncRepository(t)
	ctx := context.Background()

	if ctag, err := sync.CTag(ctx, calendarID); err != nil || ctag != 0 {
		t.Fatalf("expected ctag 0 for an untouched calendar, got %d, err %v", ctag, err)
	}

	seq1, err := sync.NextChangeSeq(ctx)
	if err != nil {
		t.Fatalf("next change seq: %v", err)
	}
	if _, err := events.Create(ctx, "evt-1", userID, calendarID, "Standup",
		time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		false, "", nil, nil, nil, "", "", seq1); err != nil {
		t.Fatalf("create event: %v", err)
	}
	if ctag, err := sync.CTag(ctx, calendarID); err != nil || ctag != seq1 {
		t.Fatalf("expected ctag %d after create, got %d, err %v", seq1, ctag, err)
	}

	seq2, err := sync.NextChangeSeq(ctx)
	if err != nil {
		t.Fatalf("next change seq: %v", err)
	}
	if err := sync.Tombstone(ctx, calendarID, "evt-deleted", seq2); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	if ctag, err := sync.CTag(ctx, calendarID); err != nil || ctag != seq2 {
		t.Fatalf("expected ctag %d after tombstone, got %d, err %v", seq2, ctag, err)
	}
}

func TestSyncRepository_DeletedSince_OnlyReturnsTombstonesAfterToken(t *testing.T) {
	sync, _, _, calendarID := newTestSyncRepository(t)
	ctx := context.Background()

	if err := sync.Tombstone(ctx, calendarID, "old", 1); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	if err := sync.Tombstone(ctx, calendarID, "new", 2); err != nil {
		t.Fatalf("tombstone: %v", err)
	}

	deleted, err := sync.DeletedSince(ctx, calendarID, 1)
	if err != nil {
		t.Fatalf("deleted since: %v", err)
	}
	if len(deleted) != 1 || deleted[0].UID != "new" {
		t.Fatalf("expected only the tombstone after change_seq 1, got %+v", deleted)
	}
}

func TestEventRepository_ListMastersChangedSince_OnlyReturnsMastersPastToken(t *testing.T) {
	_, events, userID, calendarID := newTestSyncRepository(t)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := events.Create(ctx, "evt-old", userID, calendarID, "Old", start, end, false, "", nil, nil, nil, "", "", 1); err != nil {
		t.Fatalf("create old event: %v", err)
	}
	if _, err := events.Create(ctx, "evt-new", userID, calendarID, "New", start, end, false, "", nil, nil, nil, "", "", 2); err != nil {
		t.Fatalf("create new event: %v", err)
	}

	changed, err := events.ListMastersChangedSince(ctx, userID, calendarID, 1)
	if err != nil {
		t.Fatalf("list changed masters: %v", err)
	}
	if len(changed) != 1 || changed[0].ID != "evt-new" {
		t.Fatalf("expected only evt-new, got %+v", changed)
	}
}

func TestEventRepository_SetChangeSeq(t *testing.T) {
	_, events, userID, calendarID := newTestSyncRepository(t)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	if _, err := events.Create(ctx, "evt-1", userID, calendarID, "Standup", start, end, false, "", nil, nil, nil, "", "", 1); err != nil {
		t.Fatalf("create event: %v", err)
	}

	if err := events.SetChangeSeq(ctx, userID, "evt-1", 7); err != nil {
		t.Fatalf("set change seq: %v", err)
	}

	got, err := events.GetByID(ctx, userID, "evt-1")
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.ChangeSeq != 7 {
		t.Fatalf("expected change_seq 7, got %d", got.ChangeSeq)
	}

	if err := events.SetChangeSeq(ctx, userID, "does-not-exist", 1); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
