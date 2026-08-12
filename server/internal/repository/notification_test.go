package repository

import (
	"context"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
)

// newTestNotificationRepository returns a NotificationRepository plus two
// real user ids and a real event id to satisfy notifications' foreign keys.
func newTestNotificationRepository(t *testing.T) (repo *NotificationRepository, userID, otherUserID int64, eventID string) {
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

	calendars := NewCalendarRepository(sqlDB)
	cal, err := calendars.Create(ctx, user.ID, workspace.ID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	events := NewEventRepository(sqlDB)
	mustCreateEvent(t, events, "evt-1", user.ID, cal.ID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	return NewNotificationRepository(sqlDB), user.ID, other.ID, "evt-1"
}

func TestNotificationRepository_InsertAndListRecentByUser(t *testing.T) {
	repo, userID, otherUserID, eventID := newTestNotificationRepository(t)
	ctx := context.Background()

	occurrenceStart := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	firedAt := time.Date(2026, 1, 1, 8, 50, 0, 0, time.UTC)

	created, err := repo.Insert(ctx, userID, eventID, occurrenceStart, "Standup", firedAt)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("expected a non-zero id, got %+v", created)
	}
	if created.Seen {
		t.Fatalf("expected a freshly inserted Notification to be unseen")
	}
	if created.Kind != KindReminder {
		t.Fatalf("expected kind %q, got %q", KindReminder, created.Kind)
	}
	if created.OccurrenceStart == nil || !created.OccurrenceStart.Equal(occurrenceStart) {
		t.Fatalf("expected occurrence start %v, got %+v", occurrenceStart, created.OccurrenceStart)
	}

	// A second user's Notification must not leak into the first user's feed.
	if _, err := repo.Insert(ctx, otherUserID, eventID, occurrenceStart, "Standup", firedAt); err != nil {
		t.Fatalf("insert for other user: %v", err)
	}

	list, err := repo.ListRecentByUser(ctx, userID, 10)
	if err != nil {
		t.Fatalf("list recent: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 notification for userID, got %d: %+v", len(list), list)
	}
	if list[0].Title != "Standup" || list[0].EventID != eventID {
		t.Fatalf("unexpected notification content: %+v", list[0])
	}
}

func TestNotificationRepository_ListRecentByUserOrdersNewestFirstAndRespectsLimit(t *testing.T) {
	repo, userID, _, eventID := newTestNotificationRepository(t)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if _, err := repo.Insert(ctx, userID, eventID, base, "Reminder", base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	list, err := repo.ListRecentByUser(ctx, userID, 2)
	if err != nil {
		t.Fatalf("list recent: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected limit of 2, got %d", len(list))
	}
	if !list[0].FiredAt.After(list[1].FiredAt) {
		t.Fatalf("expected newest-first order, got %+v", list)
	}
}

func TestNotificationRepository_MarkAllSeen(t *testing.T) {
	repo, userID, otherUserID, eventID := newTestNotificationRepository(t)
	ctx := context.Background()

	occurrenceStart := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	firedAt := time.Date(2026, 1, 1, 8, 50, 0, 0, time.UTC)

	if _, err := repo.Insert(ctx, userID, eventID, occurrenceStart, "Standup", firedAt); err != nil {
		t.Fatalf("insert: %v", err)
	}
	otherNotification, err := repo.Insert(ctx, otherUserID, eventID, occurrenceStart, "Standup", firedAt)
	if err != nil {
		t.Fatalf("insert for other user: %v", err)
	}

	if err := repo.MarkAllSeen(ctx, userID); err != nil {
		t.Fatalf("mark all seen: %v", err)
	}

	list, err := repo.ListRecentByUser(ctx, userID, 10)
	if err != nil {
		t.Fatalf("list recent: %v", err)
	}
	if !list[0].Seen {
		t.Fatalf("expected notification to be marked seen: %+v", list[0])
	}

	// Marking one user's Notifications seen must not affect another user's.
	otherList, err := repo.ListRecentByUser(ctx, otherUserID, 10)
	if err != nil {
		t.Fatalf("list recent for other user: %v", err)
	}
	if otherList[0].Seen {
		t.Fatalf("expected other user's notification %d to remain unseen", otherNotification.ID)
	}
}

// TestNotificationRepository_InsertInviteHasNilOccurrenceStart covers
// ADR-0061: an invite Notification concerns the Event series rather than
// one Occurrence, so occurrence_start round-trips as nil rather than a
// zero-value time.
func TestNotificationRepository_InsertInviteHasNilOccurrenceStart(t *testing.T) {
	repo, userID, _, eventID := newTestNotificationRepository(t)
	ctx := context.Background()

	invitedAt := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	created, err := repo.InsertInvite(ctx, userID, eventID, "Standup", invitedAt)
	if err != nil {
		t.Fatalf("insert invite: %v", err)
	}
	if created.Kind != KindInvite {
		t.Fatalf("expected kind %q, got %q", KindInvite, created.Kind)
	}
	if created.OccurrenceStart != nil {
		t.Fatalf("expected a nil occurrence start, got %v", created.OccurrenceStart)
	}
	if created.Seen {
		t.Fatalf("expected a freshly inserted invite Notification to be unseen")
	}

	list, err := repo.ListRecentByUser(ctx, userID, 10)
	if err != nil {
		t.Fatalf("list recent: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 notification, got %d: %+v", len(list), list)
	}
	if list[0].Kind != KindInvite || list[0].OccurrenceStart != nil {
		t.Fatalf("expected a nil-occurrence-start invite notification, got %+v", list[0])
	}
}

// TestNotificationRepository_ListRecentByUserMixesReminderAndInviteKinds
// covers ADR-0061's "one feed" decision: reminder and invite Notifications
// share the same table, list, and newest-first ordering.
func TestNotificationRepository_ListRecentByUserMixesReminderAndInviteKinds(t *testing.T) {
	repo, userID, _, eventID := newTestNotificationRepository(t)
	ctx := context.Background()

	occurrenceStart := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	if _, err := repo.Insert(ctx, userID, eventID, occurrenceStart, "Standup", time.Date(2026, 1, 1, 8, 50, 0, 0, time.UTC)); err != nil {
		t.Fatalf("insert reminder: %v", err)
	}
	if _, err := repo.InsertInvite(ctx, userID, eventID, "Standup", time.Date(2026, 1, 1, 8, 55, 0, 0, time.UTC)); err != nil {
		t.Fatalf("insert invite: %v", err)
	}

	list, err := repo.ListRecentByUser(ctx, userID, 10)
	if err != nil {
		t.Fatalf("list recent: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 notifications, got %d: %+v", len(list), list)
	}
	if list[0].Kind != KindInvite {
		t.Fatalf("expected the more recently written invite first, got %+v", list)
	}
	if list[1].Kind != KindReminder {
		t.Fatalf("expected the reminder second, got %+v", list)
	}
}
