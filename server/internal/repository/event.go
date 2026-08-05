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
	// Description and Location are free-text fields on an Event, viewable
	// and editable in the web app. Empty when unset (#61).
	Description string
	Location    string
	Start       time.Time
	End         time.Time
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
	Exdates []time.Time
	// Reminders are this Event's Reminders (ADR-0020). Not a column —
	// populated by the service layer from event_reminders when
	// listing/reading/writing an Event.
	Reminders []Reminder
	CreatedAt time.Time
	// ChangeSeq is this row's value of the global change_seq counter, bumped
	// on every write to its series. Meaningful only on a Master row: it is
	// CalDAV's per-object change marker, driving CTag and sync-collection
	// (ADR-0025).
	ChangeSeq int64
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

func (r *EventRepository) Create(ctx context.Context, id string, userID int64, calendarID, title string, start, end time.Time, allDay bool, rrule string, parentID *string, recurrenceID *time.Time, tzid *string, description, location string, changeSeq int64) (Event, error) {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO events (id, user_id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, description, location, change_seq) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, calendarID, title, start, end, allDay, rrule, parentID, recurrenceID, tzid, description, location, changeSeq,
	); err != nil {
		return Event{}, fmt.Errorf("insert event: %w", err)
	}

	return r.GetByID(ctx, userID, id)
}

func (r *EventRepository) GetByID(ctx context.Context, userID int64, id string) (Event, error) {
	return scanEvent(r.db.QueryRowContext(ctx,
		`SELECT id, user_id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, description, location, created_at, change_seq FROM events WHERE user_id = ? AND id = ?`,
		userID, id,
	))
}

// ListByUser returns a user's events ordered by start time. When from/to are
// non-nil, only events overlapping that half-open range are returned; either
// may be nil to leave that side of the range unbounded.
func (r *EventRepository) ListByUser(ctx context.Context, userID int64, from, to *time.Time) ([]Event, error) {
	query := `SELECT id, user_id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, description, location, created_at, change_seq FROM events WHERE user_id = ?`
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

// ListAllWithReminders returns every Event across every user that carries at
// least one Reminder — the firing engine's read path (ADR-0021), which runs
// as a single background process serving every account, unlike ListByUser's
// per-caller scoping.
func (r *EventRepository) ListAllWithReminders(ctx context.Context) ([]Event, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, description, location, created_at, change_seq
		 FROM events
		 WHERE EXISTS (SELECT 1 FROM event_reminders WHERE event_reminders.event_id = events.id)
		 ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list events with reminders: %w", err)
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

func (r *EventRepository) Update(ctx context.Context, userID int64, id, calendarID, title string, start, end time.Time, allDay bool, rrule string, tzid *string, description, location string, changeSeq int64) (Event, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE events SET calendar_id = ?, title = ?, "start" = ?, "end" = ?, all_day = ?, rrule = ?, tzid = ?, description = ?, location = ?, change_seq = ? WHERE user_id = ? AND id = ?`,
		calendarID, title, start, end, allDay, rrule, tzid, description, location, changeSeq, userID, id,
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

// SetChangeSeq stamps id's row with changeSeq directly, without touching any
// other column. Used when a write to a *different* row (an Override's
// create/update/delete, an Exception, a reparent) still changes id's series
// object as CalDAV sees it, so id — always a Master — must record the bump
// too (ADR-0025).
func (r *EventRepository) SetChangeSeq(ctx context.Context, userID int64, id string, changeSeq int64) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE events SET change_seq = ? WHERE user_id = ? AND id = ?`,
		changeSeq, userID, id,
	)
	if err != nil {
		return fmt.Errorf("set change_seq: %w", err)
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

// ListChildrenByParentIDs returns each parent's Overrides, keyed by
// parent_id and ordered by recurrence_id. Parents with no Overrides are
// simply absent from the map. Used to recompose a series' calendar object
// over CalDAV (ADR-0025).
func (r *EventRepository) ListChildrenByParentIDs(ctx context.Context, userID int64, parentIDs []string) (map[string][]Event, error) {
	result := map[string][]Event{}
	if len(parentIDs) == 0 {
		return result, nil
	}

	query := `SELECT id, user_id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, description, location, created_at, change_seq
		 FROM events WHERE user_id = ? AND parent_id IN (` + placeholders(len(parentIDs)) + `) ORDER BY recurrence_id`
	args := make([]any, 0, len(parentIDs)+1)
	args = append(args, userID)
	for _, id := range parentIDs {
		args = append(args, id)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list children: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		result[*e.ParentID] = append(result[*e.ParentID], e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate children: %w", err)
	}

	return result, nil
}

// ListMastersByCalendar returns every Master Event (parent_id IS NULL) in
// calendarID, ordered by id — CalDAV's per-calendar object listing
// (ADR-0025).
func (r *EventRepository) ListMastersByCalendar(ctx context.Context, userID int64, calendarID string) ([]Event, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, description, location, created_at, change_seq
		 FROM events WHERE user_id = ? AND calendar_id = ? AND parent_id IS NULL ORDER BY id`,
		userID, calendarID,
	)
	if err != nil {
		return nil, fmt.Errorf("list masters: %w", err)
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
		return nil, fmt.Errorf("iterate masters: %w", err)
	}

	return events, nil
}

// ListMastersChangedSince returns calendarID's Master Events whose
// change_seq is greater than since, ordered by change_seq — the changed
// half of a sync-collection REPORT's diff (ADR-0025).
func (r *EventRepository) ListMastersChangedSince(ctx context.Context, userID int64, calendarID string, since int64) ([]Event, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, description, location, created_at, change_seq
		 FROM events WHERE user_id = ? AND calendar_id = ? AND parent_id IS NULL AND change_seq > ? ORDER BY change_seq`,
		userID, calendarID, since,
	)
	if err != nil {
		return nil, fmt.Errorf("list changed masters: %w", err)
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
		return nil, fmt.Errorf("iterate changed masters: %w", err)
	}

	return events, nil
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
	var description sql.NullString
	var location sql.NullString
	err := row.Scan(&e.ID, &e.UserID, &e.CalendarID, &e.Title, &e.Start, &e.End, &e.AllDay, &e.Rrule, &parentID, &recurrenceID, &tzid, &description, &location, &e.CreatedAt, &e.ChangeSeq)
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
	e.Description = description.String
	e.Location = location.String
	return e, nil
}
