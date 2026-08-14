package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// Reminder is a trigger offset (minutes before Occurrence start) plus a
// delivery Channel ("notification" or "email"), projecting an iCalendar
// VALARM (ADR-0020).
type Reminder struct {
	// ID identifies this Reminder's own row — the firing engine's
	// fired-ledger keys exactly-once tracking on (ID, occurrence start), so
	// a duplicate Reminder (same offset+channel, ADR-0020 allows no dedupe)
	// still fires independently of its twin, and a wholesale Replace (an
	// Event update) starts every Reminder's fired history over, since it's a
	// new row (ADR-0021). Zero on a Reminder not yet read back from storage.
	ID            int64
	OffsetMinutes int
	Channel       string
	// DefaultReminderID is set only when this value was resolved from a
	// Calendar default rather than read from the User's own explicit
	// event_reminders row (ADR-0064) — zero on every explicit Reminder, and
	// the resolving calendar_default_reminders row's own id otherwise. ID is
	// always zero alongside it: a resolved Reminder was never one of the
	// User's own rows on the Event it resolved onto.
	DefaultReminderID int64
}

// EventReminderRepository stores Reminders (ADR-0020) — many per (User,
// Event), unconstrained, replaced wholesale on Event update.
type EventReminderRepository struct {
	db DBTX
}

func NewEventReminderRepository(db *sql.DB) *EventReminderRepository {
	return &EventReminderRepository{db: db}
}

// bindEventReminderRepository shares an already-open DBTX with a new
// EventReminderRepository — for a same-package caller that already holds
// one (EventRepository.ListAllWithReminders' join, ADR-0064) rather than a
// caller with its own *sql.DB to construct one from scratch.
func bindEventReminderRepository(db DBTX) *EventReminderRepository {
	return &EventReminderRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018).
func (r *EventReminderRepository) WithTx(tx *sql.Tx) *EventReminderRepository {
	return &EventReminderRepository{db: tx}
}

// ReplaceByEventID discards userID's existing Reminders on eventID and
// inserts reminders in their place — a Reminder write replaces one User's
// set wholesale (ADR-0020), scoped to their own rows rather than every row
// on the Event (ADR-0064): another User's Reminders on the same Event are
// never touched.
func (r *EventReminderRepository) ReplaceByEventID(ctx context.Context, userID int64, eventID string, reminders []Reminder) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM event_reminders WHERE event_id = ? AND user_id = ?`, eventID, userID); err != nil {
		return fmt.Errorf("delete reminders: %w", err)
	}

	for _, reminder := range reminders {
		if _, err := r.db.ExecContext(ctx,
			`INSERT INTO event_reminders (event_id, user_id, offset_minutes, channel) VALUES (?, ?, ?, ?)`,
			eventID, userID, reminder.OffsetMinutes, reminder.Channel,
		); err != nil {
			return fmt.Errorf("insert reminder: %w", err)
		}
	}
	return nil
}

// CopyByEventID copies every User's Reminder rows from fromEventID onto
// toEventID — creating an Override of a recurring Occurrence copies its
// Master's whole Reminder set, not just the acting User's, and creates
// nothing for a User who had none (ADR-0064). Also used to carry every
// User's Reminders onto the new Master a "This and following" split mints,
// so a User whose Reminder applied to the old Master's future Occurrences
// doesn't silently lose it at the split point.
func (r *EventReminderRepository) CopyByEventID(ctx context.Context, fromEventID, toEventID string) error {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO event_reminders (event_id, user_id, offset_minutes, channel)
		 SELECT ?, user_id, offset_minutes, channel FROM event_reminders WHERE event_id = ?`,
		toEventID, fromEventID,
	); err != nil {
		return fmt.Errorf("copy reminders: %w", err)
	}
	return nil
}

// DeleteByUserAndCalendar clears every Reminder userID holds on any Event in
// calendarID — a User losing Access to the Calendar (a Share revoked or
// renounced) takes their Reminders on it with them (ADR-0064), since a
// Reminder with no Access behind it would otherwise linger, invisible, until
// the User was ever shared with it again.
func (r *EventReminderRepository) DeleteByUserAndCalendar(ctx context.Context, userID int64, calendarID string) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM event_reminders WHERE user_id = ? AND event_id IN (SELECT id FROM events WHERE calendar_id = ?)`,
		userID, calendarID,
	); err != nil {
		return fmt.Errorf("delete reminders for calendar: %w", err)
	}
	return nil
}

// ListAllByEventIDs returns every User's Reminders on eventIDs, keyed first
// by event id and then by the User whose rows they are, unscoped to any one
// User — the firing engine's batched read path (ADR-0021, ADR-0064), since
// every recipient's own rows must be found and fired independently.
func (r *EventReminderRepository) ListAllByEventIDs(ctx context.Context, eventIDs []string) (map[string]map[int64][]Reminder, error) {
	result := make(map[string]map[int64][]Reminder)
	if len(eventIDs) == 0 {
		return result, nil
	}

	query := `SELECT id, event_id, user_id, offset_minutes, channel FROM event_reminders WHERE event_id IN (` + placeholders(len(eventIDs)) + `)`
	args := make([]any, len(eventIDs))
	for i, id := range eventIDs {
		args[i] = id
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list all reminders: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var eventID string
		var userID int64
		var reminder Reminder
		if err := rows.Scan(&reminder.ID, &eventID, &userID, &reminder.OffsetMinutes, &reminder.Channel); err != nil {
			return nil, fmt.Errorf("scan reminder: %w", err)
		}
		if result[eventID] == nil {
			result[eventID] = make(map[int64][]Reminder)
		}
		result[eventID][userID] = append(result[eventID][userID], reminder)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reminders: %w", err)
	}

	return result, nil
}

// ListByEventIDs returns each event's Reminders belonging to userID, keyed
// by event_id (ADR-0064). Events with no Reminders for userID are simply
// absent from the map.
func (r *EventReminderRepository) ListByEventIDs(ctx context.Context, userID int64, eventIDs []string) (map[string][]Reminder, error) {
	result := make(map[string][]Reminder)
	if len(eventIDs) == 0 {
		return result, nil
	}

	query := `SELECT id, event_id, offset_minutes, channel FROM event_reminders WHERE user_id = ? AND event_id IN (` + placeholders(len(eventIDs)) + `)`
	args := make([]any, 0, len(eventIDs)+1)
	args = append(args, userID)
	for _, id := range eventIDs {
		args = append(args, id)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list reminders: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var eventID string
		var reminder Reminder
		if err := rows.Scan(&reminder.ID, &eventID, &reminder.OffsetMinutes, &reminder.Channel); err != nil {
			return nil, fmt.Errorf("scan reminder: %w", err)
		}
		result[eventID] = append(result[eventID], reminder)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reminders: %w", err)
	}

	return result, nil
}
