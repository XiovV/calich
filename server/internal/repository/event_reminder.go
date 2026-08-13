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
}

// EventReminderRepository stores Reminders (ADR-0020) — many per (User,
// Event), unconstrained, replaced wholesale on Event update.
type EventReminderRepository struct {
	db DBTX
}

func NewEventReminderRepository(db *sql.DB) *EventReminderRepository {
	return &EventReminderRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018).
func (r *EventReminderRepository) WithTx(tx *sql.Tx) *EventReminderRepository {
	return &EventReminderRepository{db: tx}
}

// ReplaceByEventID discards userID's existing Reminders on eventID and
// inserts reminders in their place — an Event update replaces its Reminders
// set wholesale (ADR-0020), scoped to one User's rows rather than every row
// on the Event (ADR-0064). Every current caller passes the Calendar's Owner
// (#208); a personal Reminder for a non-Owner User is a later ticket's
// addition.
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

// ListByEventIDs returns each event's Reminders belonging to userID, keyed
// by event_id (ADR-0064). Events with no Reminders for userID are simply
// absent from the map. Every current caller passes the Calendar's Owner
// (#208).
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
