package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ReminderOverride is one User's personal replacement for an Event's
// Reminders (ADR-0036, CONTEXT.md's Reminder override): a different offset,
// a different Channel, or muted entirely, applying to that User alone.
// OffsetMinutes and Channel are independently nil when the User hasn't
// overridden that dimension, leaving the Event's own value in effect for it.
type ReminderOverride struct {
	OffsetMinutes *int
	Channel       *string
	Muted         bool
}

// ReminderOverrideRepository stores per-User Reminder overrides, keyed on
// (user_id, event_id) rather than the Reminder so a row survives the
// wholesale Reminder replace an Event update performs (ADR-0020, ADR-0036).
type ReminderOverrideRepository struct {
	db DBTX
}

func NewReminderOverrideRepository(db *sql.DB) *ReminderOverrideRepository {
	return &ReminderOverrideRepository{db: db}
}

// bindReminderOverrideRepository shares an already-open DBTX with a new
// ReminderOverrideRepository — for a same-package caller that already holds
// one (EventRepository.ListAllWithReminders' fan-out join, ADR-0036) rather
// than a caller with its own *sql.DB to construct one from scratch.
func bindReminderOverrideRepository(db DBTX) *ReminderOverrideRepository {
	return &ReminderOverrideRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018).
func (r *ReminderOverrideRepository) WithTx(tx *sql.Tx) *ReminderOverrideRepository {
	return &ReminderOverrideRepository{db: tx}
}

// Upsert sets userID's Reminder override on eventID, replacing any override
// already there — a User retuning their own override again is a replace,
// not a merge.
func (r *ReminderOverrideRepository) Upsert(ctx context.Context, userID int64, eventID string, override ReminderOverride) (ReminderOverride, error) {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO user_event_reminders (user_id, event_id, offset_minutes, channel, muted) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, event_id) DO UPDATE SET offset_minutes = excluded.offset_minutes, channel = excluded.channel, muted = excluded.muted`,
		userID, eventID, override.OffsetMinutes, override.Channel, override.Muted,
	); err != nil {
		return ReminderOverride{}, fmt.Errorf("upsert reminder override: %w", err)
	}
	return override, nil
}

// Get returns userID's Reminder override on eventID, or ErrNotFound if userID
// has never set one (the fallback case: the Event's own Reminders apply
// unchanged).
func (r *ReminderOverrideRepository) Get(ctx context.Context, userID int64, eventID string) (ReminderOverride, error) {
	var o ReminderOverride
	err := r.db.QueryRowContext(ctx,
		`SELECT offset_minutes, channel, muted FROM user_event_reminders WHERE user_id = ? AND event_id = ?`,
		userID, eventID,
	).Scan(&o.OffsetMinutes, &o.Channel, &o.Muted)
	if errors.Is(err, sql.ErrNoRows) {
		return ReminderOverride{}, ErrNotFound
	}
	if err != nil {
		return ReminderOverride{}, fmt.Errorf("scan reminder override: %w", err)
	}
	return o, nil
}

// Delete clears userID's Reminder override on eventID — the User falling
// back to the Event's own Reminders. Unlike most deletes in this package,
// a no-op (nothing to clear) is not an error: clearing an override that
// isn't set is idempotent, not a failure.
func (r *ReminderOverrideRepository) Delete(ctx context.Context, userID int64, eventID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM user_event_reminders WHERE user_id = ? AND event_id = ?`, userID, eventID)
	if err != nil {
		return fmt.Errorf("delete reminder override: %w", err)
	}
	return nil
}

// DeleteByUserAndCalendar clears every Reminder override userID holds on any
// Event in calendarID — a User losing Access to the Calendar (a Share
// revoked or renounced) takes their overrides on it with them (ADR-0036's
// acceptance criteria), since an override with no Access behind it would
// otherwise linger, invisible, until the User was ever shared with it again.
func (r *ReminderOverrideRepository) DeleteByUserAndCalendar(ctx context.Context, userID int64, calendarID string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM user_event_reminders WHERE user_id = ? AND event_id IN (SELECT id FROM events WHERE calendar_id = ?)`,
		userID, calendarID,
	)
	if err != nil {
		return fmt.Errorf("delete reminder overrides for calendar: %w", err)
	}
	return nil
}

// ListByEventIDs returns every Reminder override on any of eventIDs, keyed
// first by event id and then by the User who set it — the firing engine's
// batched read path (ADR-0021, ADR-0036), avoiding one query per Event.
func (r *ReminderOverrideRepository) ListByEventIDs(ctx context.Context, eventIDs []string) (map[string]map[int64]ReminderOverride, error) {
	result := make(map[string]map[int64]ReminderOverride)
	if len(eventIDs) == 0 {
		return result, nil
	}

	args := make([]any, len(eventIDs))
	for i, id := range eventIDs {
		args[i] = id
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT event_id, user_id, offset_minutes, channel, muted FROM user_event_reminders WHERE event_id IN (`+placeholders(len(eventIDs))+`)`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list reminder overrides: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var eventID string
		var userID int64
		var o ReminderOverride
		if err := rows.Scan(&eventID, &userID, &o.OffsetMinutes, &o.Channel, &o.Muted); err != nil {
			return nil, fmt.Errorf("scan reminder override: %w", err)
		}
		if result[eventID] == nil {
			result[eventID] = make(map[int64]ReminderOverride)
		}
		result[eventID][userID] = o
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reminder overrides: %w", err)
	}

	return result, nil
}
