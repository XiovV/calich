package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Outbox status values (ADR-0060): Pending is queued and not yet delivered
// (whether never attempted or backing off after a failure), Sent is
// delivered, Failed is a terminal give-up after exhausting every retry.
const (
	OutboxStatusPending = "pending"
	OutboxStatusSent    = "sent"
	OutboxStatusFailed  = "failed"
)

// OutboxMethodRequest is the only Method an OutboxMessage carries today — a
// METHOD:REQUEST Invitation. CANCEL (re-issue on change, withdrawal on
// removal/delete) is #201's.
const OutboxMethodRequest = "REQUEST"

// OutboxMessage is a queued Invitation email (ADR-0059, ADR-0060): written
// in the same transaction as the Attendee row it accompanies, so a failed
// send never loses the Attendee and a rolled-back create queues nothing.
// RecipientUserID and RecipientEmail are nullable with exactly one set
// (#200, ADR-0058), mirroring Attendee's own two shapes: a User-backed
// Attendee queues the former, an email-shaped one — no account to notify
// in-app, but still an Invitation to send — the latter.
type OutboxMessage struct {
	ID              int64
	EventID         string
	RecipientUserID *int64
	RecipientEmail  *string
	Method          string
	Status          string
	Attempts        int
	NextAttemptAt   time.Time
	LastError       string
	CreatedAt       time.Time
	SentAt          *time.Time
}

// OutboxRepository stores queued Invitation emails, drained by the
// background outbox Worker (ADR-0060).
type OutboxRepository struct {
	db DBTX
}

func NewOutboxRepository(db *sql.DB) *OutboxRepository {
	return &OutboxRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx — the write path
// (EventService's inviteUser/expandGroupMembers) always calls Enqueue
// through this, alongside the Attendee row, in the same transaction.
func (r *OutboxRepository) WithTx(tx *sql.Tx) *OutboxRepository {
	return &OutboxRepository{db: tx}
}

// Enqueue writes a pending REQUEST OutboxMessage for recipientUserID on
// eventID, ready to send immediately (NextAttemptAt defaults to now).
func (r *OutboxRepository) Enqueue(ctx context.Context, eventID string, recipientUserID int64) (OutboxMessage, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO outbox (event_id, recipient_user_id, method) VALUES (?, ?, ?)`,
		eventID, recipientUserID, OutboxMethodRequest,
	)
	if err != nil {
		return OutboxMessage{}, fmt.Errorf("insert outbox message: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return OutboxMessage{}, fmt.Errorf("get last insert id: %w", err)
	}

	return r.Get(ctx, id)
}

// EnqueueEmail is Enqueue's email-shaped counterpart (#200, ADR-0058): a
// pending REQUEST OutboxMessage for recipientEmail, who has no account on
// this instance to key a recipient_user_id on. recipientEmail is expected
// already normalized (trimmed, lowercased) by the caller, same as
// AttendeeRepository.AddEmail — it's what makes
// EventService.LoadInvitationForEmail's plain Go string comparison against
// the stored attendees.email agree with this column's value, rather than
// relying on both sides happening to be typed identically.
func (r *OutboxRepository) EnqueueEmail(ctx context.Context, eventID string, recipientEmail string) (OutboxMessage, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO outbox (event_id, recipient_email, method) VALUES (?, ?, ?)`,
		eventID, recipientEmail, OutboxMethodRequest,
	)
	if err != nil {
		return OutboxMessage{}, fmt.Errorf("insert outbox message: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return OutboxMessage{}, fmt.Errorf("get last insert id: %w", err)
	}

	return r.Get(ctx, id)
}

// Get returns one OutboxMessage by id, or ErrNotFound.
func (r *OutboxRepository) Get(ctx context.Context, id int64) (OutboxMessage, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, event_id, recipient_user_id, recipient_email, method, status, attempts, next_attempt_at, last_error, created_at, sent_at
		 FROM outbox WHERE id = ?`,
		id,
	)
	msg, err := scanOutboxMessage(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return OutboxMessage{}, ErrNotFound
		}
		return OutboxMessage{}, fmt.Errorf("scan outbox message: %w", err)
	}
	return msg, nil
}

// scanOutboxMessage uses rowScanner (app_password.go), satisfied by both
// *sql.Row and *sql.Rows, so it serves Get and ListPending alike.
func scanOutboxMessage(row rowScanner) (OutboxMessage, error) {
	var m OutboxMessage
	var recipientUserID sql.NullInt64
	var recipientEmail sql.NullString
	var lastError sql.NullString
	var sentAt sql.NullTime
	if err := row.Scan(&m.ID, &m.EventID, &recipientUserID, &recipientEmail, &m.Method, &m.Status, &m.Attempts, &m.NextAttemptAt, &lastError, &m.CreatedAt, &sentAt); err != nil {
		return OutboxMessage{}, err
	}
	if recipientUserID.Valid {
		id := recipientUserID.Int64
		m.RecipientUserID = &id
	}
	if recipientEmail.Valid {
		email := recipientEmail.String
		m.RecipientEmail = &email
	}
	m.LastError = lastError.String
	if sentAt.Valid {
		m.SentAt = &sentAt.Time
	}
	return m, nil
}

// ListPending returns up to limit Pending messages ordered oldest first
// (ascending id) — the Worker's own drain order (ADR-0060), regardless of
// whether NextAttemptAt has come due yet; Tick decides that per message so
// it can stop at the first recipient still backing off without skipping
// past them to a later message for someone else.
func (r *OutboxRepository) ListPending(ctx context.Context, limit int) ([]OutboxMessage, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, event_id, recipient_user_id, recipient_email, method, status, attempts, next_attempt_at, last_error, created_at, sent_at
		 FROM outbox WHERE status = ? ORDER BY id ASC LIMIT ?`,
		OutboxStatusPending, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending outbox messages: %w", err)
	}
	defer rows.Close()

	messages := []OutboxMessage{}
	for rows.Next() {
		msg, err := scanOutboxMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan outbox message: %w", err)
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox messages: %w", err)
	}
	return messages, nil
}

// MarkSent records a successful delivery.
func (r *OutboxRepository) MarkSent(ctx context.Context, id int64, sentAt time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE outbox SET status = ?, sent_at = ? WHERE id = ?`,
		OutboxStatusSent, sentAt, id,
	)
	if err != nil {
		return fmt.Errorf("mark outbox message sent: %w", err)
	}
	return requireAffected(res)
}

// MarkRetry records a failed attempt that hasn't exhausted its retries yet —
// the message stays Pending, due again at nextAttemptAt (ADR-0060's backoff).
func (r *OutboxRepository) MarkRetry(ctx context.Context, id int64, attempts int, nextAttemptAt time.Time, lastErr string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE outbox SET attempts = ?, next_attempt_at = ?, last_error = ? WHERE id = ? AND status = ?`,
		attempts, nextAttemptAt, lastErr, id, OutboxStatusPending,
	)
	if err != nil {
		return fmt.Errorf("mark outbox message retry: %w", err)
	}
	return requireAffected(res)
}

// MarkFailed records a message that has exhausted every retry — the
// terminal state (ADR-0060): backoff cannot retry forever.
func (r *OutboxRepository) MarkFailed(ctx context.Context, id int64, attempts int, lastErr string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE outbox SET status = ?, attempts = ?, last_error = ? WHERE id = ?`,
		OutboxStatusFailed, attempts, lastErr, id,
	)
	if err != nil {
		return fmt.Errorf("mark outbox message failed: %w", err)
	}
	return requireAffected(res)
}
