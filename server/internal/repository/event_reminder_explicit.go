package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// EventReminderExplicitRepository stores ADR-0064's one bit of resolution
// state: a (event_id, user_id) row means that User's Reminder list on that
// Event is explicit — even when currently empty — so their Calendar default
// no longer applies to it. Written by a Reminder save (EventService.SetReminders),
// by a CalDAV PUT (EventService.PutSeries), whose VALARM set is the PUTting
// principal's own Reminder save, and by copying a Master's markers onto a new
// Override or split Master (mirroring EventReminderRepository.CopyByEventID).
// Never by an Event field write that carries no Reminder intent of its own —
// Create, Update and ICS import alike.
type EventReminderExplicitRepository struct {
	db DBTX
}

func NewEventReminderExplicitRepository(db *sql.DB) *EventReminderExplicitRepository {
	return &EventReminderExplicitRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018).
func (r *EventReminderExplicitRepository) WithTx(tx *sql.Tx) *EventReminderExplicitRepository {
	return &EventReminderExplicitRepository{db: tx}
}

// Mark records that userID's Reminder list on eventID is explicit —
// idempotent, since saving an already-explicit list again marks nothing new.
func (r *EventReminderExplicitRepository) Mark(ctx context.Context, userID int64, eventID string) error {
	if _, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO event_reminders_explicit (event_id, user_id) VALUES (?, ?)`,
		eventID, userID,
	); err != nil {
		return fmt.Errorf("mark reminders explicit: %w", err)
	}
	return nil
}

// ListByEventIDs returns, for the given eventIDs, which of userIDs have
// explicitly saved a Reminder list on each (even an empty one) — resolution's
// "don't apply the default here" check, answered for one viewer or for every
// User over one query. A nil userIDs asks for every User; a (Event, User) pair
// with no marker is simply absent.
func (r *EventReminderExplicitRepository) ListByEventIDs(ctx context.Context, eventIDs []string, userIDs []int64) (map[string]map[int64]bool, error) {
	result := make(map[string]map[int64]bool)
	if len(eventIDs) == 0 {
		return result, nil
	}

	users, userArgs := userFilter(userIDs)
	query := `SELECT event_id, user_id FROM event_reminders_explicit WHERE event_id IN (` + placeholders(len(eventIDs)) + `)` + users
	args := make([]any, 0, len(eventIDs)+len(userArgs))
	for _, id := range eventIDs {
		args = append(args, id)
	}
	args = append(args, userArgs...)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list explicit reminder markers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var eventID string
		var userID int64
		if err := rows.Scan(&eventID, &userID); err != nil {
			return nil, fmt.Errorf("scan explicit reminder marker: %w", err)
		}
		if result[eventID] == nil {
			result[eventID] = make(map[int64]bool)
		}
		result[eventID][userID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate explicit reminder markers: %w", err)
	}
	return result, nil
}

// CopyByEventID copies every User's explicit marker from fromEventID onto
// toEventID — mirrors EventReminderRepository.CopyByEventID so an explicit
// opt-out on a Master (an empty saved list) survives onto a new Override or
// a "This and following" split's new Master, rather than silently
// reactivating that User's Calendar default on the new row.
func (r *EventReminderExplicitRepository) CopyByEventID(ctx context.Context, fromEventID, toEventID string) error {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO event_reminders_explicit (event_id, user_id)
		 SELECT ?, user_id FROM event_reminders_explicit WHERE event_id = ?`,
		toEventID, fromEventID,
	); err != nil {
		return fmt.Errorf("copy explicit reminder markers: %w", err)
	}
	return nil
}

// DeleteByUserAndCalendar clears every explicit marker userID holds on any
// Event in calendarID — a User losing Access to the Calendar takes their
// opt-outs with them (ADR-0064), mirroring
// EventReminderRepository.DeleteByUserAndCalendar, so regaining Access later
// resolves fresh against the current default rather than staying frozen at
// an old explicit-empty state.
func (r *EventReminderExplicitRepository) DeleteByUserAndCalendar(ctx context.Context, userID int64, calendarID string) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM event_reminders_explicit WHERE user_id = ? AND event_id IN (SELECT id FROM events WHERE calendar_id = ?)`,
		userID, calendarID,
	); err != nil {
		return fmt.Errorf("delete explicit reminder markers for calendar: %w", err)
	}
	return nil
}
