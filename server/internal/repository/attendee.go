package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Attendee is an Attendee (ADR-0046): a User invited to one specific Event,
// independent of Calendar Access — the invite itself grants them visibility
// to that Event alone. Response is the iCalendar PARTSTAT they've set on
// their own invite.
type Attendee struct {
	EventID   string
	UserID    int64
	Response  string
	CreatedAt time.Time
}

// Attendee Response values — iCalendar's PARTSTAT set (ADR-0046).
const (
	ResponseNeedsAction = "needs-action"
	ResponseAccepted    = "accepted"
	ResponseDeclined    = "declined"
	ResponseTentative   = "tentative"
)

type AttendeeRepository struct {
	db DBTX
}

func NewAttendeeRepository(db *sql.DB) *AttendeeRepository {
	return &AttendeeRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx.
func (r *AttendeeRepository) WithTx(tx *sql.Tx) *AttendeeRepository {
	return &AttendeeRepository{db: tx}
}

// bindAttendeeRepository shares an already-open DBTX with a new
// AttendeeRepository — for a same-package caller that already holds one
// (EventRepository.ListAllWithReminders' fan-out join, ADR-0046) rather than
// a caller with its own *sql.DB to construct one from scratch.
func bindAttendeeRepository(db DBTX) *AttendeeRepository {
	return &AttendeeRepository{db: db}
}

// ErrAlreadyAttendee is returned by Add when userID is already an Attendee
// of eventID — the attendees primary key is (event_id, user_id).
var ErrAlreadyAttendee = errors.New("user is already an attendee of this event")

// Add inserts an attendees row binding userID to eventID, defaulting their
// response to needs-action (ADR-0046).
func (r *AttendeeRepository) Add(ctx context.Context, eventID string, userID int64) (Attendee, error) {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO attendees (event_id, user_id) VALUES (?, ?)`,
		eventID, userID,
	); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return Attendee{}, ErrAlreadyAttendee
		}
		return Attendee{}, fmt.Errorf("insert attendee: %w", err)
	}
	return r.Get(ctx, eventID, userID)
}

// Get returns userID's Attendee row on eventID, or ErrNotFound if they
// aren't an Attendee of it — both the invite-visibility check and the
// seam SetResponse resolves through to confirm the caller is the Attendee
// they claim to be.
func (r *AttendeeRepository) Get(ctx context.Context, eventID string, userID int64) (Attendee, error) {
	var a Attendee
	err := r.db.QueryRowContext(ctx,
		`SELECT event_id, user_id, response, created_at FROM attendees WHERE event_id = ? AND user_id = ?`,
		eventID, userID,
	).Scan(&a.EventID, &a.UserID, &a.Response, &a.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Attendee{}, ErrNotFound
		}
		return Attendee{}, fmt.Errorf("scan attendee: %w", err)
	}
	return a, nil
}

// Remove deletes userID's attendees row from eventID — revoking their
// visibility to it (ADR-0046). Their historical response is not retained;
// what happens to it is left to implementation by ADR-0046, and simple row
// deletion is the implementation chosen here.
func (r *AttendeeRepository) Remove(ctx context.Context, eventID string, userID int64) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM attendees WHERE event_id = ? AND user_id = ?`,
		eventID, userID,
	)
	if err != nil {
		return fmt.Errorf("delete attendee: %w", err)
	}
	return requireAffected(res)
}

// SetResponse updates userID's own response on eventID.
func (r *AttendeeRepository) SetResponse(ctx context.Context, eventID string, userID int64, response string) (Attendee, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE attendees SET response = ? WHERE event_id = ? AND user_id = ?`,
		response, eventID, userID,
	)
	if err != nil {
		return Attendee{}, fmt.Errorf("update attendee response: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return Attendee{}, err
	}
	return r.Get(ctx, eventID, userID)
}

// SetResponseByEmail updates email's own response on eventID — SetResponse's
// email-shaped counterpart, reached by the reply poller (#202, ADR-0059)
// rather than the in-app SetResponse endpoint: an email-shaped Attendee has no
// session to authenticate a User-backed SetResponse call through, and no
// user_id to key on regardless.
func (r *AttendeeRepository) SetResponseByEmail(ctx context.Context, eventID, email, response string) (AttendeeWithName, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE attendees SET response = ? WHERE event_id = ? AND email = ?`,
		response, eventID, email,
	)
	if err != nil {
		return AttendeeWithName{}, fmt.Errorf("update email attendee response: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return AttendeeWithName{}, err
	}
	return r.getByEmail(ctx, eventID, email)
}

// AddEmail inserts an attendees row binding email to eventID with no
// account behind it (ADR-0058, #200): an Attendee who is not a Member, on an
// instance that couldn't resolve the typed address against one. email is
// expected already normalized (trimmed, lowercased) by the caller — the
// column's own COLLATE NOCASE makes the UNIQUE constraint case-insensitive
// regardless, but a consistent stored form is what lets a plain Go string
// comparison (LoadInvitationForEmail) agree with it.
func (r *AttendeeRepository) AddEmail(ctx context.Context, eventID, email string) (AttendeeWithName, error) {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO attendees (event_id, email) VALUES (?, ?)`,
		eventID, email,
	); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return AttendeeWithName{}, ErrAlreadyAttendee
		}
		return AttendeeWithName{}, fmt.Errorf("insert email attendee: %w", err)
	}
	return r.getByEmail(ctx, eventID, email)
}

// getByEmail returns email's Attendee row on eventID — AddEmail's read-back,
// mirroring Get's role for the User-backed shape. UserID is always nil and
// Name always empty: there is no User row to join against.
func (r *AttendeeRepository) getByEmail(ctx context.Context, eventID, email string) (AttendeeWithName, error) {
	var a AttendeeWithName
	err := r.db.QueryRowContext(ctx,
		`SELECT event_id, email, response, created_at FROM attendees WHERE event_id = ? AND email = ?`,
		eventID, email,
	).Scan(&a.EventID, &a.Email, &a.Response, &a.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AttendeeWithName{}, ErrNotFound
		}
		return AttendeeWithName{}, fmt.Errorf("scan email attendee: %w", err)
	}
	return a, nil
}

// RemoveEmail deletes email's attendees row from eventID — the email-shaped
// counterpart to Remove.
func (r *AttendeeRepository) RemoveEmail(ctx context.Context, eventID, email string) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM attendees WHERE event_id = ? AND email = ?`,
		eventID, email,
	)
	if err != nil {
		return fmt.Errorf("delete email attendee: %w", err)
	}
	return requireAffected(res)
}

// ListUserIDsByEventIDs returns every User-backed Attendee's user_id on any
// of eventIDs, keyed by event id — the reminder fan-out's batched read path
// (ADR-0021, ADR-0046), unioned onto RecipientUserIDs alongside each Event's
// Calendar Access-holders rather than replacing them. An email-shaped
// Attendee (ADR-0058, #200) has no user_id and is filtered out here rather
// than in the caller: they get no Reminders on any Channel, since they hold
// the Invitation in their own calendar and their own client reminds them.
func (r *AttendeeRepository) ListUserIDsByEventIDs(ctx context.Context, eventIDs []string) (map[string][]int64, error) {
	result := make(map[string][]int64)
	if len(eventIDs) == 0 {
		return result, nil
	}

	args := make([]any, len(eventIDs))
	for i, id := range eventIDs {
		args[i] = id
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT event_id, user_id FROM attendees WHERE user_id IS NOT NULL AND event_id IN (`+placeholders(len(eventIDs))+`)`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list attendee user ids: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var eventID string
		var userID int64
		if err := rows.Scan(&eventID, &userID); err != nil {
			return nil, fmt.Errorf("scan attendee user id: %w", err)
		}
		result[eventID] = append(result[eventID], userID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attendee user ids: %w", err)
	}

	return result, nil
}

// AttendeeWithName is one Attendee row hydrated for display and for the
// codec's ATTENDEE emission (ADR-0062) — ListByEventID's row, mirroring
// GroupMember's plain shape but with the Name a caller displaying an
// Attendee list actually needs. Not an embedded Attendee: UserID is nullable
// here (ADR-0058, #200), since a row can be User-backed or email-shaped, which
// Attendee itself (Get/Add/SetResponse/Remove's shape) never is — those only
// ever operate on the User-backed half. UserID nil means email-shaped: Name
// is then always "" (no User row to join against) and Email is the
// attendees.email column verbatim rather than a joined user's; the wire
// handler still whitelists which fields it exposes.
type AttendeeWithName struct {
	EventID   string
	UserID    *int64
	Response  string
	CreatedAt time.Time
	Name      string
	Email     string
}

// ListByEventID returns every Attendee of eventID with their Name (empty for
// an email-shaped Attendee), ordered by Name then Email so User-backed rows
// sort together ahead of email-shaped ones.
func (r *AttendeeRepository) ListByEventID(ctx context.Context, eventID string) ([]AttendeeWithName, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT a.event_id, a.user_id, a.response, a.created_at, COALESCE(u.name, ''), COALESCE(u.email, a.email)
		 FROM attendees a
		 LEFT JOIN users u ON u.id = a.user_id
		 WHERE a.event_id = ?
		 ORDER BY u.name, a.email`,
		eventID,
	)
	if err != nil {
		return nil, fmt.Errorf("list attendees: %w", err)
	}
	defer rows.Close()

	attendees := []AttendeeWithName{}
	for rows.Next() {
		a, err := scanAttendeeWithName(rows)
		if err != nil {
			return nil, fmt.Errorf("scan attendee: %w", err)
		}
		attendees = append(attendees, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attendees: %w", err)
	}
	return attendees, nil
}

// ListWithNamesByEventIDs returns every Attendee of any of eventIDs with
// their Name and Email, keyed by event id and ordered within each the same
// way ListByEventID is — the batched counterpart to ListByEventID,
// ListUserIDsByEventIDs' shape, for the codec's ATTENDEE emission across a
// whole series (master plus overrides) in one query rather than one per
// VEVENT (ADR-0062).
func (r *AttendeeRepository) ListWithNamesByEventIDs(ctx context.Context, eventIDs []string) (map[string][]AttendeeWithName, error) {
	result := make(map[string][]AttendeeWithName)
	if len(eventIDs) == 0 {
		return result, nil
	}

	args := make([]any, len(eventIDs))
	for i, id := range eventIDs {
		args[i] = id
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT a.event_id, a.user_id, a.response, a.created_at, COALESCE(u.name, ''), COALESCE(u.email, a.email)
		 FROM attendees a
		 LEFT JOIN users u ON u.id = a.user_id
		 WHERE a.event_id IN (`+placeholders(len(eventIDs))+`)
		 ORDER BY a.event_id, u.name, a.email`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list attendees: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		a, err := scanAttendeeWithName(rows)
		if err != nil {
			return nil, fmt.Errorf("scan attendee: %w", err)
		}
		result[a.EventID] = append(result[a.EventID], a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attendees: %w", err)
	}
	return result, nil
}

// scanAttendeeWithName scans one row of ListByEventID/ListWithNamesByEventIDs'
// shared column list, satisfied by both *sql.Row and *sql.Rows.
func scanAttendeeWithName(row rowScanner) (AttendeeWithName, error) {
	var a AttendeeWithName
	var userID sql.NullInt64
	if err := row.Scan(&a.EventID, &userID, &a.Response, &a.CreatedAt, &a.Name, &a.Email); err != nil {
		return AttendeeWithName{}, err
	}
	if userID.Valid {
		id := userID.Int64
		a.UserID = &id
	}
	return a, nil
}
