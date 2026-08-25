package repository

import (
	"context"
	"database/sql"
	"encoding/json"
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

// OutboxMethodRequest is a METHOD:REQUEST Invitation — a fresh invite or a
// re-issued one (ADR-0059). OutboxMethodCancel is a METHOD:CANCEL
// withdrawal, queued on Attendee removal or Event deletion (#201).
const (
	OutboxMethodRequest = "REQUEST"
	OutboxMethodCancel  = "CANCEL"
)

// OutboxCancelSnapshot is a CANCEL OutboxMessage's self-contained payload
// (ADR-0059, ADR-0060, #201): everything InvitationSender needs to render a
// METHOD:CANCEL, captured by EventService at the moment the row it withdraws
// (or the Attendee row on that still-live Event) still exists. Unlike a
// REQUEST — which InvitationSender always rebuilds from live state, per its
// own doc comment — a CANCEL's very purpose is to outlive what it withdraws,
// so it cannot re-read anything at send time.
type OutboxCancelSnapshot struct {
	// UID is the withdrawn row's iTIP UID: its own id on a Master or a
	// standalone Event, its Master's id on an Override
	// (icalendar.InvitationToICal's own contract).
	UID string `json:"uid"`
	// RecurrenceID is set only when the withdrawn row was an Override.
	RecurrenceID *time.Time `json:"recurrenceId,omitempty"`
	AllDay       bool       `json:"allDay"`
	Tzid         *string    `json:"tzid,omitempty"`
	Start        time.Time  `json:"start"`
	End          time.Time  `json:"end"`
	Title        string     `json:"title"`
	// Sequence is the row's iTIP SEQUENCE at the moment it was withdrawn —
	// never bumped further by a cancellation itself.
	Sequence int64 `json:"sequence"`
	// OrganizerName/OrganizerEmail are the withdrawn row's own Organizer
	// (#193, ADR-0055) — CN on the wire; the instance mailbox itself
	// (ADR-0059's ORGANIZER split) is resolved by InvitationSender at send
	// time from its own configured from address, same as a REQUEST.
	OrganizerName  string `json:"organizerName"`
	OrganizerEmail string `json:"organizerEmail"`
	// RecipientEmail/RecipientName are this one recipient's own address and
	// CN — resolved once here since the Attendee row itself may already be
	// gone by send time.
	RecipientEmail string `json:"recipientEmail"`
	RecipientName  string `json:"recipientName"`
}

// OutboxMessage is a queued Invitation or Cancellation email (ADR-0059,
// ADR-0060, #201): written in the same transaction as the Attendee row it
// accompanies (or, for a CANCEL, the row/Attendee-row it withdraws), so a
// failed send never loses the Attendee and a rolled-back create queues
// nothing. RecipientUserID and RecipientEmail are nullable with exactly one
// set (#200, ADR-0058), mirroring Attendee's own two shapes: a User-backed
// Attendee queues the former, an email-shaped one — no account to notify
// in-app, but still an Invitation to send — the latter. Snapshot is non-nil
// only when Method is OutboxMethodCancel.
type OutboxMessage struct {
	ID              int64
	EventID         string
	RecipientUserID *int64
	RecipientEmail  *string
	// ActorUserID names who queued a brand-new Invitation (#204, ADR-0058) —
	// set only by EnqueueWithActor/EnqueueEmailWithActor, the two brand-new-
	// invite entry points; nil on every re-send and every CANCEL, which
	// never charge anyone's hourly ceiling.
	ActorUserID   *int64
	Method        string
	Snapshot      *OutboxCancelSnapshot
	Status        string
	Attempts      int
	NextAttemptAt time.Time
	LastError     string
	CreatedAt     time.Time
	SentAt        *time.Time
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

// EnqueueWithActor is Enqueue's charged counterpart (#204, ADR-0058):
// inviteUser's own brand-new invite, naming actorUserID so
// CountByActorSince can attribute it to their hourly ceiling. Every other
// caller of Enqueue — a re-send to an Event's existing Attendees — is
// lifecycle mail, not a new invite, and stays uncharged.
func (r *OutboxRepository) EnqueueWithActor(ctx context.Context, eventID string, recipientUserID, actorUserID int64) (OutboxMessage, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO outbox (event_id, recipient_user_id, actor_user_id, method) VALUES (?, ?, ?, ?)`,
		eventID, recipientUserID, actorUserID, OutboxMethodRequest,
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

// EnqueueEmailWithActor is EnqueueWithActor's email-shaped counterpart
// (#204, ADR-0058), mirroring EnqueueEmail's relationship to Enqueue.
func (r *OutboxRepository) EnqueueEmailWithActor(ctx context.Context, eventID string, recipientEmail string, actorUserID int64) (OutboxMessage, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO outbox (event_id, recipient_email, actor_user_id, method) VALUES (?, ?, ?, ?)`,
		eventID, recipientEmail, actorUserID, OutboxMethodRequest,
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

// CountByActorSince counts actorUserID's own charged Invitations (rows
// EnqueueWithActor/EnqueueEmailWithActor wrote) created at or after since,
// regardless of status — a message counts against the ceiling the moment
// it's queued, not once it's actually sent (#204, ADR-0058). The caller
// (chargeInviteRateLimit) is expected to pass since = time.Now().Add(-time.Hour)
// for the rolling hourly window. since is normalized to UTC before binding:
// created_at's CURRENT_TIMESTAMP default is always UTC with no offset
// suffix, and a local-zone time.Time bound as-is serializes with one,
// silently breaking every row out of the lexical comparison SQLite is
// doing against that TEXT column — this is not a comparison SQL can get
// wrong by construction, so the normalization has to happen here.
func (r *OutboxRepository) CountByActorSince(ctx context.Context, actorUserID int64, since time.Time) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM outbox WHERE actor_user_id = ? AND created_at >= ?`,
		actorUserID, since.UTC(),
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count outbox messages by actor: %w", err)
	}
	return count, nil
}

// EnqueueCancel writes a pending CANCEL OutboxMessage for recipientUserID on
// eventID, carrying snapshot — everything InvitationSender needs to render
// it, since the row (or Attendee row) it withdraws may be gone by send time
// (ADR-0059, ADR-0060, #201).
func (r *OutboxRepository) EnqueueCancel(ctx context.Context, eventID string, recipientUserID int64, snapshot OutboxCancelSnapshot) (OutboxMessage, error) {
	return r.enqueueCancel(ctx, eventID, &recipientUserID, nil, snapshot)
}

// EnqueueCancelEmail is EnqueueCancel's email-shaped counterpart (#200,
// ADR-0058), mirroring EnqueueEmail's relationship to Enqueue.
func (r *OutboxRepository) EnqueueCancelEmail(ctx context.Context, eventID string, recipientEmail string, snapshot OutboxCancelSnapshot) (OutboxMessage, error) {
	return r.enqueueCancel(ctx, eventID, nil, &recipientEmail, snapshot)
}

func (r *OutboxRepository) enqueueCancel(ctx context.Context, eventID string, recipientUserID *int64, recipientEmail *string, snapshot OutboxCancelSnapshot) (OutboxMessage, error) {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return OutboxMessage{}, fmt.Errorf("marshal cancel snapshot: %w", err)
	}

	res, err := r.db.ExecContext(ctx,
		`INSERT INTO outbox (event_id, recipient_user_id, recipient_email, method, snapshot) VALUES (?, ?, ?, ?, ?)`,
		eventID, recipientUserID, recipientEmail, OutboxMethodCancel, string(encoded),
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
		`SELECT id, event_id, recipient_user_id, recipient_email, actor_user_id, method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at
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
	var actorUserID sql.NullInt64
	var snapshot sql.NullString
	var lastError sql.NullString
	var sentAt sql.NullTime
	if err := row.Scan(&m.ID, &m.EventID, &recipientUserID, &recipientEmail, &actorUserID, &m.Method, &snapshot, &m.Status, &m.Attempts, &m.NextAttemptAt, &lastError, &m.CreatedAt, &sentAt); err != nil {
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
	if actorUserID.Valid {
		id := actorUserID.Int64
		m.ActorUserID = &id
	}
	if snapshot.Valid {
		var s OutboxCancelSnapshot
		if err := json.Unmarshal([]byte(snapshot.String), &s); err != nil {
			return OutboxMessage{}, fmt.Errorf("unmarshal cancel snapshot: %w", err)
		}
		m.Snapshot = &s
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
		`SELECT id, event_id, recipient_user_id, recipient_email, actor_user_id, method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at
		 FROM outbox WHERE status = ? ORDER BY id ASC LIMIT ?`,
		OutboxStatusPending, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending outbox messages: %w", err)
	}
	return collectRows(rows, scanOutboxMessage)
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
