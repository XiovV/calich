package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Event struct {
	ID         string
	UserID     int64
	CalendarID string
	Title      string
	Start      time.Time
	End        time.Time
	// AllDay flags this Event as occupying whole dates rather than a time
	// range. Start/end still hold the half-open date range (start = the date,
	// end = the exclusive next day). See ADR-0017.
	AllDay bool
	// Tzid is the IANA zone the Event's wall-clock is anchored to (the
	// Anchor zone): a named zone is a zoned Event, "Etc/UTC" is an absolute
	// instant, and nil is a Floating Event that renders/expands in the
	// Viewer zone. Start/end are always stored as UTC instants regardless.
	// See ADR-0019.
	Tzid *string
	// Rrule is the event's iCalendar RRULE as opaque text, empty when the event
	// does not recur. Stored and returned verbatim; expansion happens on the
	// frontend (ADR-0016).
	Rrule string
	// ParentID and RecurrenceID are set together and only on an Override: a
	// standalone Event that replaces one Occurrence of its parent's series.
	// Both are nil on a Master (recurring or not). See ADR-0016.
	ParentID     *string
	RecurrenceID *time.Time
	// Exdates lists the cancelled Occurrence starts (Exceptions) for a Master.
	// Not a column — populated by the service layer from event_exceptions when
	// listing/reading a Master, so it is always empty on an Override.
	Exdates   []time.Time
	CreatedAt time.Time
}

type EventRepository struct {
	db DBTX
}

func NewEventRepository(db *sql.DB) *EventRepository {
	return &EventRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018).
func (r *EventRepository) WithTx(tx *sql.Tx) *EventRepository {
	return &EventRepository{db: tx}
}

func (r *EventRepository) Create(ctx context.Context, id string, userID int64, calendarID, title string, start, end time.Time, allDay bool, rrule string, parentID *string, recurrenceID *time.Time, tzid *string) (Event, error) {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO events (id, user_id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, calendarID, title, start, end, allDay, rrule, parentID, recurrenceID, tzid,
	); err != nil {
		return Event{}, fmt.Errorf("insert event: %w", err)
	}

	return r.GetByID(ctx, userID, id)
}

func (r *EventRepository) GetByID(ctx context.Context, userID int64, id string) (Event, error) {
	return scanEvent(r.db.QueryRowContext(ctx,
		`SELECT id, user_id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, created_at FROM events WHERE user_id = ? AND id = ?`,
		userID, id,
	))
}

// ListByUser returns a user's events ordered by start time. When from/to are
// non-nil, only events overlapping that half-open range are returned; either
// may be nil to leave that side of the range unbounded.
func (r *EventRepository) ListByUser(ctx context.Context, userID int64, from, to *time.Time) ([]Event, error) {
	query := `SELECT id, user_id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, created_at FROM events WHERE user_id = ?`
	args := []any{userID}

	if from != nil {
		query += ` AND "end" > ?`
		args = append(args, *from)
	}
	if to != nil {
		query += ` AND "start" < ?`
		args = append(args, *to)
	}
	query += ` ORDER BY "start", id`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	events := []Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}

	return events, nil
}

func (r *EventRepository) Update(ctx context.Context, userID int64, id, calendarID, title string, start, end time.Time, allDay bool, rrule string, tzid *string) (Event, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE events SET calendar_id = ?, title = ?, "start" = ?, "end" = ?, all_day = ?, rrule = ?, tzid = ? WHERE user_id = ? AND id = ?`,
		calendarID, title, start, end, allDay, rrule, tzid, userID, id,
	)
	if err != nil {
		return Event{}, fmt.Errorf("update event: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return Event{}, fmt.Errorf("get rows affected: %w", err)
	}
	if affected == 0 {
		return Event{}, ErrNotFound
	}

	return r.GetByID(ctx, userID, id)
}

func (r *EventRepository) Delete(ctx context.Context, userID int64, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM events WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return fmt.Errorf("delete event: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}

	return nil
}

// ReparentOverridesFrom moves every Override of oldParentID whose
// recurrence_id is at-or-after fromStart to belong to newParentID instead —
// the "this and following" split reparenting overrides at the boundary
// (ADR-0016).
func (r *EventRepository) ReparentOverridesFrom(ctx context.Context, userID int64, oldParentID, newParentID string, fromStart time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE events SET parent_id = ? WHERE user_id = ? AND parent_id = ? AND recurrence_id >= ?`,
		newParentID, userID, oldParentID, fromStart,
	)
	if err != nil {
		return fmt.Errorf("reparent overrides: %w", err)
	}
	return nil
}

// DeleteChildrenOf deletes every Override belonging to parentID (the master
// keeps its own row). Used when a rule change forces the "All events" scope
// and discards per-Occurrence edits (ADR-0016).
func (r *EventRepository) DeleteChildrenOf(ctx context.Context, userID int64, parentID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM events WHERE user_id = ? AND parent_id = ?`, userID, parentID)
	if err != nil {
		return fmt.Errorf("delete children: %w", err)
	}
	return nil
}

// scanner is satisfied by both *sql.Row and *sql.Rows, so scanEvent can hydrate
// an Event from either a single-row query or a row within an iteration.
type scanner interface {
	Scan(dest ...any) error
}

func scanEvent(row scanner) (Event, error) {
	var e Event
	var parentID sql.NullString
	var recurrenceID sql.NullTime
	var tzid sql.NullString
	err := row.Scan(&e.ID, &e.UserID, &e.CalendarID, &e.Title, &e.Start, &e.End, &e.AllDay, &e.Rrule, &parentID, &recurrenceID, &tzid, &e.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	if err != nil {
		return Event{}, fmt.Errorf("scan event: %w", err)
	}
	if parentID.Valid {
		e.ParentID = &parentID.String
	}
	if recurrenceID.Valid {
		e.RecurrenceID = &recurrenceID.Time
	}
	if tzid.Valid {
		e.Tzid = &tzid.String
	}
	return e, nil
}
