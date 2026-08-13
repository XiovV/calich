package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/XiovV/calendar/server/internal/recurrence"
)

type Event struct {
	ID         string
	CalendarID string
	Title      string
	// Description and Location are free-text fields on an Event, viewable
	// and editable in the web app. Empty when unset (#61).
	Description string
	Location    string
	// URL is the Event's optional link (iCalendar URL, ADR-0063), stored
	// exactly as submitted — never validated or rewritten. Empty when
	// unset, mirroring Description/Location.
	URL string
	// Color is this Event's own color override, nullable: nil means
	// "inherit the Calendar's color" (ADR-0043). Unlike Description/Location
	// it is never coerced to an empty string — absent and "" are the same
	// thing for free text, but "no color" and "color unset" must stay
	// distinguishable from any valid hex value.
	Color *string
	Start time.Time
	End   time.Time
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
	// ExternalUID is the foreign iCalendar UID a Refresh reconciles against
	// (#83, ADR-0033), set only on Events belonging to a Subscribed
	// Calendar. The row id stays a minted UUID regardless — this is a
	// separate column, not an alternate id. Unique per Calendar among
	// Masters; an Override shares its Master's value.
	ExternalUID *string
	// CreatedBy is who created this Event, for attribution only — never
	// consulted for authorization, which resolves through CalendarID
	// instead (ADR-0034). Nil when the creating User has since been
	// deleted (ON DELETE SET NULL), or for rows written before this column
	// existed.
	CreatedBy *int64
	// CreatedByName is CreatedBy's display Name (#118). Not a
	// column — populated by the service layer from users, mirroring how
	// Exdates and Reminders are attached. Empty whenever CreatedBy is nil.
	CreatedByName string
	// CreatedByEmail is CreatedBy's Email, populated alongside
	// CreatedByName — the Organizer's mailto address the codec's ORGANIZER
	// property needs (ADR-0055, ADR-0062). Empty whenever CreatedBy is nil.
	CreatedByEmail string
	// Attendees are this Event's own Attendees with their Name and Email
	// (ADR-0046, ADR-0062), for the codec's ATTENDEE emission. Not a
	// column — populated by the service layer from the attendees table,
	// mirroring Reminders. Scoped to this row alone, not the whole series:
	// an Override can carry Attendees distinct from its Master's (#193).
	Attendees []AttendeeWithName
	// Attachments are this Event's Attachments (#132, ADR-0040) — set only
	// on a Master; an Override never carries its own. Not a column —
	// populated by the service layer from event_attachments, mirroring
	// Reminders.
	Attachments []Attachment
	// CalendarName and CalendarColor are CalendarID's Name and resolved
	// display Color, for display only — never consulted for authorization.
	// Not a column — populated by the service layer (EventService's
	// attachCalendarMeta), mirroring CreatedByName. Every List/Get caller
	// already has independent visibility into this Event (Calendar Access,
	// or an Attendee invite with none at all, ADR-0046) even when that
	// visibility carries no Calendar row of its own to read a name/color
	// from otherwise.
	CalendarName  string
	CalendarColor string
	// AttendeeCount is this Event's number of Attendees (ADR-0046). Not a
	// column — populated by the service layer from the attendees table,
	// mirroring CreatedByName. Exists so a caller deciding whether an Event
	// "has Attendees" (#193's progressive-disclosure rule) has a synchronous
	// answer at List/Get time, rather than needing a separate round-trip to
	// the Attendees endpoint.
	AttendeeCount int
	// Sequence is this row's own iTIP SEQUENCE (ADR-0059, #201) — distinct
	// from ChangeSeq (CalDAV's per-object marker, bumped on every write) and
	// never touched by one. It only increments when start, end, rrule or
	// all_day changes (EventService's material-change set), which is what
	// makes a recipient's client reset their PARTSTAT and re-ask them (RFC
	// 5546). Scoped to this row alone: an Override tracks its own SEQUENCE
	// independently of its Master's, since each is its own VEVENT on the
	// wire.
	Sequence int64
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

// EventFields is an Event's column values for a write — everything except its
// identity (id, user_id) and its change_seq stamp, which are passed separately
// because they are not the caller's to vary freely.
//
// ParentID and RecurrenceID anchor an Override to the Occurrence it replaces.
// They are set on insert only: Update leaves those columns alone, since an
// Override's anchoring never moves (ADR-0016).
type EventFields struct {
	CalendarID   string
	Title        string
	Start, End   time.Time
	AllDay       bool
	Rrule        string
	ParentID     *string
	RecurrenceID *time.Time
	Tzid         *string
	Description  string
	Location     string
	URL          string
	// Color mirrors Event.Color — nil means "inherit the Calendar's color".
	Color *string
	// ExternalUID is set on insert only (like ParentID/RecurrenceID) — a
	// Refresh reconciles by it rather than ever updating it in place.
	ExternalUID *string
}

func (r *EventRepository) Create(ctx context.Context, id string, createdBy *int64, f EventFields, changeSeq int64) (Event, error) {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO events (id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, description, location, url, color, external_uid, created_by, change_seq) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, f.CalendarID, f.Title, f.Start, f.End, f.AllDay, f.Rrule, f.ParentID, f.RecurrenceID, f.Tzid, f.Description, f.Location, f.URL, f.Color, f.ExternalUID, createdBy, changeSeq,
	); err != nil {
		return Event{}, fmt.Errorf("insert event: %w", err)
	}

	return r.GetByID(ctx, id)
}

func (r *EventRepository) GetByID(ctx context.Context, id string) (Event, error) {
	return scanEvent(r.db.QueryRowContext(ctx,
		`SELECT id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, description, location, url, color, external_uid, created_by, created_at, change_seq, sequence FROM events WHERE id = ?`,
		id,
	))
}

// ListByCalendarIDs returns every Event in any of calendarIDs, ordered by
// start time. When from/to are non-nil, only events overlapping that
// half-open range are returned; either may be nil to leave that side of the
// range unbounded. Returns no rows (not an error) for an empty calendarIDs —
// the caller has access to no Calendar.
//
// A recurring Master's own start/end columns are just its first Occurrence —
// windowing on those directly would wrongly exclude e.g. a 2009 Master with
// an open-ended rrule from a 2026 window despite it still generating
// Occurrences there (#80). So a Master (rrule != "") is only cheaply
// pre-filtered by SQL on its lower bound — Occurrences never precede a
// series' own start, so "start" >= to rules it out on its own — and then
// checked in Go via recurrence.ExpandOccurrences, the same query-time
// expander CalDAV and ICS export use, to decide whether it actually
// produces an Occurrence in [from, to) — a widened, effectively-unbounded
// window on whichever side has no caller-given bound, so e.g. a from-only
// query still excludes a Master whose rule ended before from. A plain Event
// or an Override (both rrule == "") has no such ambiguity: its stored
// start/end IS the Occurrence, so the SQL column filter alone is exact.
func (r *EventRepository) ListByCalendarIDs(ctx context.Context, calendarIDs []string, from, to *time.Time) ([]Event, error) {
	if len(calendarIDs) == 0 {
		return []Event{}, nil
	}

	query := `SELECT id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, description, location, url, color, external_uid, created_by, created_at, change_seq, sequence FROM events WHERE calendar_id IN (` + placeholders(len(calendarIDs)) + `)`
	args := make([]any, len(calendarIDs))
	for i, id := range calendarIDs {
		args[i] = id
	}

	return r.listWindowed(ctx, query, args, from, to)
}

// ListByAttendeeUserID returns every Event userID is an Attendee of, with
// the same from/to windowing ListByCalendarIDs applies — the Attendee half
// of a User's visible Events (ADR-0046), which EventService.List unions
// with ListByCalendarIDs' Calendar-Access half.
func (r *EventRepository) ListByAttendeeUserID(ctx context.Context, userID int64, from, to *time.Time) ([]Event, error) {
	query := `SELECT id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, description, location, url, color, external_uid, created_by, created_at, change_seq, sequence FROM events WHERE id IN (SELECT event_id FROM attendees WHERE user_id = ?)`
	return r.listWindowed(ctx, query, []any{userID}, from, to)
}

// listWindowed runs baseQuery (already scoped to the caller's visible set by
// its WHERE clause) plus this package's shared from/to windowing —
// ListByCalendarIDs and ListByAttendeeUserID's common tail, including the
// recurrence-aware Go-side filter neither SQL alone can express (#80).
func (r *EventRepository) listWindowed(ctx context.Context, baseQuery string, baseArgs []any, from, to *time.Time) ([]Event, error) {
	query := baseQuery
	args := append([]any{}, baseArgs...)

	switch {
	case from != nil && to != nil:
		query += ` AND ((rrule = '' AND "end" > ? AND "start" < ?) OR (rrule != '' AND "start" < ?))`
		args = append(args, *from, *to, *to)
	case from != nil:
		query += ` AND (rrule != '' OR "end" > ?)`
		args = append(args, *from)
	case to != nil:
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

	if from == nil && to == nil {
		return events, nil
	}
	// One-sided windows still need the Go-side recurrence check — a
	// from-only query must still exclude a Master whose rule ended before
	// from, not just one starting after it — so a missing bound is widened
	// to an effectively unbounded sentinel rather than skipped outright.
	windowFrom, windowTo := openWindowStart, openWindowEnd
	if from != nil {
		windowFrom = *from
	}
	if to != nil {
		windowTo = *to
	}
	return filterRecurringByWindow(events, windowFrom, windowTo)
}

// openWindowStart and openWindowEnd stand in for a missing from/to bound in
// ListByUser's Go-side recurrence check, wide enough to never themselves
// exclude a real Occurrence.
var (
	openWindowStart = time.Time{}
	openWindowEnd   = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
)

// filterRecurringByWindow drops a recurring Master (rrule != "") from events
// when its rrule produces no Occurrence in [from, to) — SQL already excluded
// what it safely could; this is the recurrence-aware half of the filter
// SQL can't express (#80).
func filterRecurringByWindow(events []Event, from, to time.Time) ([]Event, error) {
	filtered := make([]Event, 0, len(events))
	for _, e := range events {
		if e.Rrule == "" {
			filtered = append(filtered, e)
			continue
		}

		occurrences, err := recurrence.ExpandOccurrences(e.Rrule, e.Tzid, e.Start, from, to)
		if err != nil {
			return nil, fmt.Errorf("expand occurrences for event %q: %w", e.ID, err)
		}
		if len(occurrences) > 0 {
			filtered = append(filtered, e)
		}
	}
	return filtered, nil
}

// EventWithOwner pairs an Event with every User's own Reminders on it — the
// firing engine's read path (ADR-0021, ADR-0064). Not a repository.Event
// field: this is purely ListAllWithReminders' join result. The name is
// historical (ADR-0036's Owner-fan-out); kept rather than churned across
// every caller for what's now just "an Event plus its Reminders".
type EventWithOwner struct {
	Event
	// RemindersByUser is every User who has at least one Reminder on this
	// Event, each keyed to their own rows — a User with none is simply
	// absent, never substituted or fanned out to (ADR-0064).
	RemindersByUser map[int64][]Reminder
}

// ListAllWithReminders returns every Event across every Calendar that
// carries at least one Reminder, each paired with every User's own Reminder
// rows on it — the firing engine's read path (ADR-0021, ADR-0064), which
// runs as a single background process serving every account, unlike
// ListByCalendarIDs' per-caller scoping.
func (r *EventRepository) ListAllWithReminders(ctx context.Context) ([]EventWithOwner, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, description, location, url, color, external_uid, created_by, created_at, change_seq, sequence
		 FROM events
		 WHERE EXISTS (SELECT 1 FROM event_reminders WHERE event_reminders.event_id = events.id)
		 ORDER BY events.id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list events with reminders: %w", err)
	}
	defer rows.Close()

	events := []EventWithOwner{}
	for rows.Next() {
		var e EventWithOwner
		if err := scanEventRow(rows, &e.Event); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}

	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}
	remindersByEvent, err := bindEventReminderRepository(r.db).ListAllByEventIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list reminders: %w", err)
	}

	for i := range events {
		events[i].RemindersByUser = remindersByEvent[events[i].ID]
	}

	return events, nil
}

// Update rewrites id's columns from f. f.ParentID and f.RecurrenceID are
// ignored — see EventFields. sequence is the row's new iTIP SEQUENCE
// (ADR-0059, #201) — the caller decides its value (bumped on a material
// change, left as-is otherwise), since only EventService knows whether f
// differs from the row being replaced.
func (r *EventRepository) Update(ctx context.Context, id string, f EventFields, changeSeq, sequence int64) (Event, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE events SET calendar_id = ?, title = ?, "start" = ?, "end" = ?, all_day = ?, rrule = ?, tzid = ?, description = ?, location = ?, url = ?, color = ?, change_seq = ?, sequence = ? WHERE id = ?`,
		f.CalendarID, f.Title, f.Start, f.End, f.AllDay, f.Rrule, f.Tzid, f.Description, f.Location, f.URL, f.Color, changeSeq, sequence, id,
	)
	if err != nil {
		return Event{}, fmt.Errorf("update event: %w", err)
	}

	if err := requireAffected(res); err != nil {
		return Event{}, err
	}

	return r.GetByID(ctx, id)
}

// SetChangeSeq stamps id's row with changeSeq directly, without touching any
// other column. Used when a write to a *different* row (an Override's
// create/update/delete, an Exception, a reparent) still changes id's series
// object as CalDAV sees it, so id — always a Master — must record the bump
// too (ADR-0025).
func (r *EventRepository) SetChangeSeq(ctx context.Context, id string, changeSeq int64) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE events SET change_seq = ? WHERE id = ?`,
		changeSeq, id,
	)
	if err != nil {
		return fmt.Errorf("set change_seq: %w", err)
	}
	return requireAffected(res)
}

func (r *EventRepository) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM events WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete event: %w", err)
	}

	return requireAffected(res)
}

// ReparentOverridesFrom moves every Override of oldParentID whose
// recurrence_id is at-or-after fromStart to belong to newParentID instead —
// the "this and following" split reparenting overrides at the boundary
// (ADR-0016).
func (r *EventRepository) ReparentOverridesFrom(ctx context.Context, oldParentID, newParentID string, fromStart time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE events SET parent_id = ? WHERE parent_id = ? AND recurrence_id >= ?`,
		newParentID, oldParentID, fromStart,
	)
	if err != nil {
		return fmt.Errorf("reparent overrides: %w", err)
	}
	return nil
}

// DeleteChildrenOf deletes every Override belonging to parentID (the master
// keeps its own row). Used when a rule change forces the "All events" scope
// and discards per-Occurrence edits (ADR-0016).
func (r *EventRepository) DeleteChildrenOf(ctx context.Context, parentID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM events WHERE parent_id = ?`, parentID)
	if err != nil {
		return fmt.Errorf("delete children: %w", err)
	}
	return nil
}

// ListChildrenByParentIDs returns each parent's Overrides, keyed by
// parent_id and ordered by recurrence_id. Parents with no Overrides are
// simply absent from the map. Used to recompose a series' calendar object
// over CalDAV (ADR-0025).
func (r *EventRepository) ListChildrenByParentIDs(ctx context.Context, parentIDs []string) (map[string][]Event, error) {
	result := map[string][]Event{}
	if len(parentIDs) == 0 {
		return result, nil
	}

	query := `SELECT id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, description, location, url, color, external_uid, created_by, created_at, change_seq, sequence
		 FROM events WHERE parent_id IN (` + placeholders(len(parentIDs)) + `) ORDER BY recurrence_id`
	args := make([]any, 0, len(parentIDs))
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
func (r *EventRepository) ListMastersByCalendar(ctx context.Context, calendarID string) ([]Event, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, description, location, url, color, external_uid, created_by, created_at, change_seq, sequence
		 FROM events WHERE calendar_id = ? AND parent_id IS NULL ORDER BY id`,
		calendarID,
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
func (r *EventRepository) ListMastersChangedSince(ctx context.Context, calendarID string, since int64) ([]Event, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, calendar_id, title, "start", "end", all_day, rrule, parent_id, recurrence_id, tzid, description, location, url, color, external_uid, created_by, created_at, change_seq, sequence
		 FROM events WHERE calendar_id = ? AND parent_id IS NULL AND change_seq > ? ORDER BY change_seq`,
		calendarID, since,
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

// scanEventRow scans an Event's columns into e.
func scanEventRow(row scanner, e *Event) error {
	var parentID sql.NullString
	var recurrenceID sql.NullTime
	var tzid sql.NullString
	var description sql.NullString
	var location sql.NullString
	var url sql.NullString
	var color sql.NullString
	var externalUID sql.NullString
	var createdBy sql.NullInt64
	if err := row.Scan(&e.ID, &e.CalendarID, &e.Title, &e.Start, &e.End, &e.AllDay, &e.Rrule, &parentID, &recurrenceID, &tzid, &description, &location, &url, &color, &externalUID, &createdBy, &e.CreatedAt, &e.ChangeSeq, &e.Sequence); err != nil {
		return err
	}
	if externalUID.Valid {
		e.ExternalUID = &externalUID.String
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
	if createdBy.Valid {
		e.CreatedBy = &createdBy.Int64
	}
	if color.Valid {
		e.Color = &color.String
	}
	e.Description = description.String
	e.Location = location.String
	e.URL = url.String
	return nil
}

func scanEvent(row scanner) (Event, error) {
	var e Event
	err := scanEventRow(row, &e)
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	if err != nil {
		return Event{}, fmt.Errorf("scan event: %w", err)
	}
	return e, nil
}
