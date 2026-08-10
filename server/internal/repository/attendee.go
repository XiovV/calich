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

// AttendeeWithUsername pairs an Attendee with the Username of the User it
// belongs to — ListByEventID's row, mirroring GroupMember's plain shape but
// with the Username a caller displaying an Attendee list actually needs.
type AttendeeWithUsername struct {
	Attendee
	Username string
}

// ListByEventID returns every Attendee of eventID with their Username,
// ordered by Username.
func (r *AttendeeRepository) ListByEventID(ctx context.Context, eventID string) ([]AttendeeWithUsername, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT a.event_id, a.user_id, a.response, a.created_at, u.username
		 FROM attendees a
		 JOIN users u ON u.id = a.user_id
		 WHERE a.event_id = ?
		 ORDER BY u.username`,
		eventID,
	)
	if err != nil {
		return nil, fmt.Errorf("list attendees: %w", err)
	}
	defer rows.Close()

	attendees := []AttendeeWithUsername{}
	for rows.Next() {
		var a AttendeeWithUsername
		if err := rows.Scan(&a.EventID, &a.UserID, &a.Response, &a.CreatedAt, &a.Username); err != nil {
			return nil, fmt.Errorf("scan attendee: %w", err)
		}
		attendees = append(attendees, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attendees: %w", err)
	}
	return attendees, nil
}
