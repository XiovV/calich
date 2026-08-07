package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// FiredReminderRepository is the firing engine's exactly-once ledger
// (ADR-0021): a Reminder+Occurrence+User triple is marked at most once,
// across repeated ticks and process restarts, via a UNIQUE(reminder_id,
// occurrence_start, user_id) constraint (ADR-0036) — a shared Calendar's
// Reminder fires independently per recipient.
type FiredReminderRepository struct {
	db DBTX
}

func NewFiredReminderRepository(db *sql.DB) *FiredReminderRepository {
	return &FiredReminderRepository{db: db}
}

// MarkFired records reminderID as fired for userID at occurrenceStart,
// unless it already was — reporting whether this call is the one that newly
// recorded it (true) or found it already there (false). The caller
// dispatches only when this reports true, which is what makes the
// exactly-once guarantee hold per recipient, regardless of how many times a
// tick is repeated or the process is restarted (ADR-0021, ADR-0036).
func (r *FiredReminderRepository) MarkFired(ctx context.Context, reminderID, userID int64, occurrenceStart, firedAt time.Time) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO fired_reminders (reminder_id, user_id, occurrence_start, fired_at) VALUES (?, ?, ?, ?)`,
		reminderID, userID, occurrenceStart, firedAt,
	)
	if err != nil {
		return false, fmt.Errorf("mark reminder fired: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("get rows affected: %w", err)
	}
	return affected > 0, nil
}
