package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// CalendarExposureRepository stores each User's own choice about whether a
// Calendar appears in their own CalDAV home-set (ADR-0080, CONTEXT.md's
// Exposure entry). Keyed directly on (calendar_id, user_id), the same shape
// calendar_user_colors already uses (ADR-0038) — there is no wholesale-
// replace-on-update to survive here either.
type CalendarExposureRepository struct {
	db DBTX
}

func NewCalendarExposureRepository(db *sql.DB) *CalendarExposureRepository {
	return &CalendarExposureRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018).
func (r *CalendarExposureRepository) WithTx(tx *sql.Tx) *CalendarExposureRepository {
	return &CalendarExposureRepository{db: tx}
}

// Upsert sets userID's own Exposure choice on calendarID, replacing whatever
// was there — a User changing their mind again is a replace, not a merge.
func (r *CalendarExposureRepository) Upsert(ctx context.Context, userID int64, calendarID string, exposed bool) error {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO calendar_exposures (calendar_id, user_id, exposed) VALUES (?, ?, ?)
		 ON CONFLICT(calendar_id, user_id) DO UPDATE SET exposed = excluded.exposed`,
		calendarID, userID, exposed,
	); err != nil {
		return fmt.Errorf("upsert calendar exposure: %w", err)
	}
	return nil
}

// Get returns userID's own Exposure override on calendarID, or ErrNotFound
// if they've never set one — the fallback case, where the default applies.
func (r *CalendarExposureRepository) Get(ctx context.Context, userID int64, calendarID string) (bool, error) {
	var exposed bool
	err := r.db.QueryRowContext(ctx,
		`SELECT exposed FROM calendar_exposures WHERE calendar_id = ? AND user_id = ?`,
		calendarID, userID,
	).Scan(&exposed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("scan calendar exposure: %w", err)
	}
	return exposed, nil
}

// Delete clears userID's own Exposure override on calendarID — RevokeShare's
// and LeaveShare's cleanup, mirroring calendar_user_colors' own Delete
// (ADR-0038): an Exposure choice with no Access behind it would otherwise
// linger, invisible, until userID was ever given Access again.
func (r *CalendarExposureRepository) Delete(ctx context.Context, userID int64, calendarID string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM calendar_exposures WHERE calendar_id = ? AND user_id = ?`, calendarID, userID); err != nil {
		return fmt.Errorf("delete calendar exposure: %w", err)
	}
	return nil
}
