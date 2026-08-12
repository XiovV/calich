package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
)

// newTestOutboxRepository returns an OutboxRepository plus its underlying
// *sql.DB, a real user id and a real event id to satisfy outbox's foreign
// keys.
func newTestOutboxRepository(t *testing.T) (repo *OutboxRepository, sqlDB *sql.DB, userID int64, eventID string) {
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

	return NewOutboxRepository(sqlDB), sqlDB, user.ID, "evt-1"
}

func TestOutboxRepository_EnqueueDefaultsToPendingRequest(t *testing.T) {
	repo, _, userID, eventID := newTestOutboxRepository(t)
	ctx := context.Background()

	msg, err := repo.Enqueue(ctx, eventID, userID)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if msg.ID == 0 {
		t.Fatalf("expected a non-zero id, got %+v", msg)
	}
	if msg.Status != OutboxStatusPending {
		t.Fatalf("expected status %q, got %q", OutboxStatusPending, msg.Status)
	}
	if msg.Method != OutboxMethodRequest {
		t.Fatalf("expected method %q, got %q", OutboxMethodRequest, msg.Method)
	}
	if msg.EventID != eventID || msg.RecipientUserID != userID {
		t.Fatalf("unexpected message identity: %+v", msg)
	}
	if msg.Attempts != 0 {
		t.Fatalf("expected 0 attempts on a fresh message, got %d", msg.Attempts)
	}
	if msg.SentAt != nil {
		t.Fatalf("expected a nil SentAt on a fresh message, got %v", msg.SentAt)
	}
}

func TestOutboxRepository_ListPendingOrdersOldestFirstAndExcludesResolved(t *testing.T) {
	repo, _, userID, eventID := newTestOutboxRepository(t)
	ctx := context.Background()

	first, err := repo.Enqueue(ctx, eventID, userID)
	if err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	second, err := repo.Enqueue(ctx, eventID, userID)
	if err != nil {
		t.Fatalf("enqueue second: %v", err)
	}
	third, err := repo.Enqueue(ctx, eventID, userID)
	if err != nil {
		t.Fatalf("enqueue third: %v", err)
	}

	if err := repo.MarkSent(ctx, second.ID, time.Now()); err != nil {
		t.Fatalf("mark second sent: %v", err)
	}
	if err := repo.MarkFailed(ctx, third.ID, 5, "smtp: boom"); err != nil {
		t.Fatalf("mark third failed: %v", err)
	}

	pending, err := repo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != first.ID {
		t.Fatalf("expected only the first message still pending, got %+v", pending)
	}
}

func TestOutboxRepository_ListPendingRespectsLimit(t *testing.T) {
	repo, _, userID, eventID := newTestOutboxRepository(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := repo.Enqueue(ctx, eventID, userID); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	pending, err := repo.ListPending(ctx, 2)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected limit of 2, got %d", len(pending))
	}
}

func TestOutboxRepository_MarkRetryStaysPendingAndRecordsBackoff(t *testing.T) {
	repo, _, userID, eventID := newTestOutboxRepository(t)
	ctx := context.Background()

	msg, err := repo.Enqueue(ctx, eventID, userID)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	next := time.Date(2026, 1, 1, 9, 5, 0, 0, time.UTC)
	if err := repo.MarkRetry(ctx, msg.ID, 1, next, "smtp: connection refused"); err != nil {
		t.Fatalf("mark retry: %v", err)
	}

	got, err := repo.Get(ctx, msg.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != OutboxStatusPending {
		t.Fatalf("expected status to stay %q after a retry, got %q", OutboxStatusPending, got.Status)
	}
	if got.Attempts != 1 {
		t.Fatalf("expected 1 attempt recorded, got %d", got.Attempts)
	}
	if !got.NextAttemptAt.Equal(next) {
		t.Fatalf("expected next attempt at %v, got %v", next, got.NextAttemptAt)
	}
	if got.LastError != "smtp: connection refused" {
		t.Fatalf("expected the failure reason to be recorded, got %q", got.LastError)
	}
}

func TestOutboxRepository_MarkFailedIsTerminal(t *testing.T) {
	repo, _, userID, eventID := newTestOutboxRepository(t)
	ctx := context.Background()

	msg, err := repo.Enqueue(ctx, eventID, userID)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if err := repo.MarkFailed(ctx, msg.ID, 5, "smtp: giving up"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	got, err := repo.Get(ctx, msg.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != OutboxStatusFailed {
		t.Fatalf("expected status %q, got %q", OutboxStatusFailed, got.Status)
	}
	if got.Attempts != 5 {
		t.Fatalf("expected 5 attempts recorded, got %d", got.Attempts)
	}

	pending, err := repo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected a failed message to never resurface as pending, got %+v", pending)
	}
}

func TestOutboxRepository_MarkSentRecordsSentAt(t *testing.T) {
	repo, _, userID, eventID := newTestOutboxRepository(t)
	ctx := context.Background()

	msg, err := repo.Enqueue(ctx, eventID, userID)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	sentAt := time.Date(2026, 1, 1, 9, 0, 30, 0, time.UTC)
	if err := repo.MarkSent(ctx, msg.ID, sentAt); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	got, err := repo.Get(ctx, msg.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != OutboxStatusSent {
		t.Fatalf("expected status %q, got %q", OutboxStatusSent, got.Status)
	}
	if got.SentAt == nil || !got.SentAt.Equal(sentAt) {
		t.Fatalf("expected SentAt %v, got %v", sentAt, got.SentAt)
	}
}

// TestOutboxRepository_EnqueueRollsBackWithItsTransaction covers the
// ADR-0060 atomicity guarantee at the repository level: a caller that
// enqueues inside a transaction it then rolls back is left with no outbox
// row at all — never a dangling Pending one behind a create that never
// happened.
func TestOutboxRepository_EnqueueRollsBackWithItsTransaction(t *testing.T) {
	repo, sqlDB, userID, eventID := newTestOutboxRepository(t)
	ctx := context.Background()

	rollbackErr := errors.New("deliberate rollback")
	err := WithTx(ctx, sqlDB, func(tx *sql.Tx) error {
		if _, err := repo.WithTx(tx).Enqueue(ctx, eventID, userID); err != nil {
			t.Fatalf("enqueue in tx: %v", err)
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("expected WithTx to surface the rollback error, got %v", err)
	}

	pending, err := repo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected the rolled-back enqueue to leave nothing queued, got %+v", pending)
	}
}
