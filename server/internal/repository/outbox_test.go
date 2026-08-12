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
	if msg.EventID != eventID || msg.RecipientUserID == nil || *msg.RecipientUserID != userID {
		t.Fatalf("unexpected message identity: %+v", msg)
	}
	if msg.RecipientEmail != nil {
		t.Fatalf("expected a nil RecipientEmail on a User-backed message, got %v", *msg.RecipientEmail)
	}
	if msg.Attempts != 0 {
		t.Fatalf("expected 0 attempts on a fresh message, got %d", msg.Attempts)
	}
	if msg.SentAt != nil {
		t.Fatalf("expected a nil SentAt on a fresh message, got %v", msg.SentAt)
	}
}

// TestOutboxRepository_EnqueueEmailDefaultsToPendingRequest covers
// EnqueueEmail (#200, ADR-0058) — Enqueue's email-shaped counterpart, for a
// recipient with no account on this instance.
func TestOutboxRepository_EnqueueEmailDefaultsToPendingRequest(t *testing.T) {
	repo, _, _, eventID := newTestOutboxRepository(t)
	ctx := context.Background()

	msg, err := repo.EnqueueEmail(ctx, eventID, "guest@example.com")
	if err != nil {
		t.Fatalf("enqueue email: %v", err)
	}
	if msg.Status != OutboxStatusPending {
		t.Fatalf("expected status %q, got %q", OutboxStatusPending, msg.Status)
	}
	if msg.RecipientUserID != nil {
		t.Fatalf("expected a nil RecipientUserID on an email-shaped message, got %v", *msg.RecipientUserID)
	}
	if msg.RecipientEmail == nil || *msg.RecipientEmail != "guest@example.com" {
		t.Fatalf("expected RecipientEmail %q, got %+v", "guest@example.com", msg)
	}

	fetched, err := repo.Get(ctx, msg.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.RecipientEmail == nil || *fetched.RecipientEmail != "guest@example.com" {
		t.Fatalf("expected Get to round-trip RecipientEmail, got %+v", fetched)
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

// testCancelSnapshot returns a representative OutboxCancelSnapshot for
// EnqueueCancel/EnqueueCancelEmail tests.
func testCancelSnapshot() OutboxCancelSnapshot {
	return OutboxCancelSnapshot{
		UID:            "evt-1",
		AllDay:         false,
		Start:          time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		End:            time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		Title:          "Standup",
		Sequence:       2,
		OrganizerName:  "Owner",
		OrganizerEmail: "owner@example.com",
		RecipientEmail: "member@example.com",
		RecipientName:  "Member",
	}
}

func TestOutboxRepository_EnqueueCancelRoundTripsSnapshot(t *testing.T) {
	repo, _, userID, eventID := newTestOutboxRepository(t)
	ctx := context.Background()

	recurrenceID := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	snapshot := testCancelSnapshot()
	snapshot.RecurrenceID = &recurrenceID
	zone := "Europe/Berlin"
	snapshot.Tzid = &zone

	msg, err := repo.EnqueueCancel(ctx, eventID, userID, snapshot)
	if err != nil {
		t.Fatalf("enqueue cancel: %v", err)
	}
	if msg.Method != OutboxMethodCancel {
		t.Fatalf("expected method %q, got %q", OutboxMethodCancel, msg.Method)
	}
	if msg.RecipientUserID == nil || *msg.RecipientUserID != userID {
		t.Fatalf("expected RecipientUserID %d, got %+v", userID, msg)
	}
	if msg.Snapshot == nil {
		t.Fatalf("expected a non-nil snapshot, got %+v", msg)
	}
	got := *msg.Snapshot
	if got.UID != snapshot.UID || got.Title != snapshot.Title || got.Sequence != snapshot.Sequence {
		t.Fatalf("expected snapshot to round-trip UID/Title/Sequence, got %+v", got)
	}
	if got.RecurrenceID == nil || !got.RecurrenceID.Equal(recurrenceID) {
		t.Fatalf("expected RecurrenceID %v, got %+v", recurrenceID, got.RecurrenceID)
	}
	if got.Tzid == nil || *got.Tzid != zone {
		t.Fatalf("expected Tzid %q, got %+v", zone, got.Tzid)
	}
	if got.OrganizerEmail != snapshot.OrganizerEmail || got.RecipientEmail != snapshot.RecipientEmail {
		t.Fatalf("expected organizer/recipient email to round-trip, got %+v", got)
	}

	// Get, not just the Enqueue return value, must also round-trip it —
	// this is what the outbox Worker actually reads back via ListPending.
	fetched, err := repo.Get(ctx, msg.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Snapshot == nil || fetched.Snapshot.UID != snapshot.UID {
		t.Fatalf("expected Get to round-trip the snapshot, got %+v", fetched)
	}
}

func TestOutboxRepository_EnqueueCancelEmailRoundTripsSnapshot(t *testing.T) {
	repo, _, _, eventID := newTestOutboxRepository(t)
	ctx := context.Background()

	snapshot := testCancelSnapshot()
	msg, err := repo.EnqueueCancelEmail(ctx, eventID, "guest@example.com", snapshot)
	if err != nil {
		t.Fatalf("enqueue cancel email: %v", err)
	}
	if msg.Method != OutboxMethodCancel {
		t.Fatalf("expected method %q, got %q", OutboxMethodCancel, msg.Method)
	}
	if msg.RecipientUserID != nil {
		t.Fatalf("expected a nil RecipientUserID on an email-shaped cancel, got %v", *msg.RecipientUserID)
	}
	if msg.RecipientEmail == nil || *msg.RecipientEmail != "guest@example.com" {
		t.Fatalf("expected RecipientEmail %q, got %+v", "guest@example.com", msg)
	}
	if msg.Snapshot == nil || msg.Snapshot.RecipientEmail != snapshot.RecipientEmail {
		t.Fatalf("expected snapshot to round-trip, got %+v", msg.Snapshot)
	}
}

// TestOutboxRepository_RequestMessageCarriesNoSnapshot covers the other
// half of the CANCEL/REQUEST split: a REQUEST enqueued the ordinary way
// carries no snapshot at all — sendInvitation always rebuilds from live
// state instead (InvitationSender.Send's own contract).
func TestOutboxRepository_RequestMessageCarriesNoSnapshot(t *testing.T) {
	repo, _, userID, eventID := newTestOutboxRepository(t)
	ctx := context.Background()

	msg, err := repo.Enqueue(ctx, eventID, userID)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if msg.Snapshot != nil {
		t.Fatalf("expected a nil snapshot on a REQUEST message, got %+v", msg.Snapshot)
	}
}

// TestOutboxRepository_CancelSurvivesEventDeletion covers the schema-level
// reason event_id carries no foreign key (#201): a CANCEL is queued for
// exactly the moment its Event row disappears, so it must still be there —
// snapshot intact — after that row is gone, in the very same transaction
// that deleted it.
func TestOutboxRepository_CancelSurvivesEventDeletion(t *testing.T) {
	repo, sqlDB, userID, eventID := newTestOutboxRepository(t)
	ctx := context.Background()

	events := NewEventRepository(sqlDB)
	snapshot := testCancelSnapshot()

	err := WithTx(ctx, sqlDB, func(tx *sql.Tx) error {
		if _, err := repo.WithTx(tx).EnqueueCancel(ctx, eventID, userID, snapshot); err != nil {
			return err
		}
		return events.WithTx(tx).Delete(ctx, eventID)
	})
	if err != nil {
		t.Fatalf("enqueue cancel + delete event in one tx: %v", err)
	}

	pending, err := repo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected the cancel message to survive its event's deletion, got %+v", pending)
	}
	if pending[0].Snapshot == nil || pending[0].Snapshot.UID != snapshot.UID {
		t.Fatalf("expected the snapshot to still be intact, got %+v", pending[0].Snapshot)
	}

	if _, err := events.GetByID(ctx, eventID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected the event to actually be gone, got %v", err)
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
