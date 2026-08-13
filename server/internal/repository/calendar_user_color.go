package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// CalendarUserColorRepository stores per-User colour overrides on a Calendar
// (ADR-0038, CONTEXT.md's Calendar color entry): a User with Access may
// shadow the Owner's colour with one that applies to them alone. Keyed
// directly on (calendar_id, user_id) — unlike event_reminders' (event_id,
// user_id), a colour override has no wholesale-replace-on-update to
// survive, so there's no indirection to get wrong here.
type CalendarUserColorRepository struct {
	db DBTX
}

func NewCalendarUserColorRepository(db *sql.DB) *CalendarUserColorRepository {
	return &CalendarUserColorRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018).
func (r *CalendarUserColorRepository) WithTx(tx *sql.Tx) *CalendarUserColorRepository {
	return &CalendarUserColorRepository{db: tx}
}

// Upsert sets userID's colour override on calendarID, replacing any override
// already there — a User retuning their own override again is a replace,
// not a merge.
func (r *CalendarUserColorRepository) Upsert(ctx context.Context, userID int64, calendarID, color string) error {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO calendar_user_colors (calendar_id, user_id, color) VALUES (?, ?, ?)
		 ON CONFLICT(calendar_id, user_id) DO UPDATE SET color = excluded.color`,
		calendarID, userID, color,
	); err != nil {
		return fmt.Errorf("upsert calendar user color: %w", err)
	}
	return nil
}

// Get returns userID's colour override on calendarID, or ErrNotFound if
// userID has never set one — the fallback case, where the Calendar's own
// colour applies.
func (r *CalendarUserColorRepository) Get(ctx context.Context, userID int64, calendarID string) (string, error) {
	var color string
	err := r.db.QueryRowContext(ctx,
		`SELECT color FROM calendar_user_colors WHERE calendar_id = ? AND user_id = ?`,
		calendarID, userID,
	).Scan(&color)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("scan calendar user color: %w", err)
	}
	return color, nil
}

// Delete clears userID's colour override on calendarID — the User falling
// back to the Calendar's own colour. A no-op (nothing to clear) is not an
// error: clearing an override that isn't set is idempotent, not a failure.
func (r *CalendarUserColorRepository) Delete(ctx context.Context, userID int64, calendarID string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM calendar_user_colors WHERE calendar_id = ? AND user_id = ?`, calendarID, userID); err != nil {
		return fmt.Errorf("delete calendar user color: %w", err)
	}
	return nil
}
