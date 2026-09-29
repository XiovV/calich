package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CalendarSet is a Calendar Set (ADR-0082): a named, private selection of
// one Workspace's Calendars belonging to one User. Membership lives in
// calendar_set_members, added separately from creation.
type CalendarSet struct {
	ID          int64
	UserID      int64
	WorkspaceID int64
	Name        string
	CreatedAt   time.Time
}

type CalendarSetRepository struct {
	db DBTX
}

func NewCalendarSetRepository(db *sql.DB) *CalendarSetRepository {
	return &CalendarSetRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx.
func (r *CalendarSetRepository) WithTx(tx *sql.Tx) *CalendarSetRepository {
	return &CalendarSetRepository{db: tx}
}

// Create inserts a new Calendar Set named name, owned by userID inside
// workspaceID.
func (r *CalendarSetRepository) Create(ctx context.Context, userID, workspaceID int64, name string) (CalendarSet, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO calendar_sets (user_id, workspace_id, name) VALUES (?, ?, ?)`,
		userID, workspaceID, name,
	)
	if err != nil {
		return CalendarSet{}, fmt.Errorf("insert calendar set: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return CalendarSet{}, fmt.Errorf("get inserted calendar set id: %w", err)
	}

	return r.GetByID(ctx, id, userID, workspaceID)
}

// GetByID returns the Calendar Set identified by id, scoped to userID and
// workspaceID together — a Set belonging to another User, or to a different
// Workspace, is ErrNotFound rather than any error naming the mismatch, so
// the caller can never distinguish "not mine" from "doesn't exist".
func (r *CalendarSetRepository) GetByID(ctx context.Context, id, userID, workspaceID int64) (CalendarSet, error) {
	var s CalendarSet
	err := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, workspace_id, name, created_at FROM calendar_sets WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		id, userID, workspaceID,
	).Scan(&s.ID, &s.UserID, &s.WorkspaceID, &s.Name, &s.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CalendarSet{}, ErrNotFound
		}
		return CalendarSet{}, fmt.Errorf("scan calendar set: %w", err)
	}
	return s, nil
}

// ListForUser returns every Calendar Set userID owns inside workspaceID,
// ordered by when it was created.
func (r *CalendarSetRepository) ListForUser(ctx context.Context, userID, workspaceID int64) ([]CalendarSet, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, workspace_id, name, created_at FROM calendar_sets WHERE user_id = ? AND workspace_id = ? ORDER BY created_at, id`,
		userID, workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list calendar sets: %w", err)
	}
	return collectRows(rows, scanCalendarSetRow)
}

func scanCalendarSetRow(row rowScanner) (CalendarSet, error) {
	var s CalendarSet
	err := row.Scan(&s.ID, &s.UserID, &s.WorkspaceID, &s.Name, &s.CreatedAt)
	return s, err
}

// Rename updates id's name, scoped to userID and workspaceID like GetByID.
func (r *CalendarSetRepository) Rename(ctx context.Context, id, userID, workspaceID int64, name string) (CalendarSet, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE calendar_sets SET name = ? WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		name, id, userID, workspaceID,
	)
	if err != nil {
		return CalendarSet{}, fmt.Errorf("rename calendar set: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return CalendarSet{}, err
	}
	return r.GetByID(ctx, id, userID, workspaceID)
}

// Delete removes id, scoped to userID and workspaceID like GetByID, and via
// ON DELETE CASCADE every calendar_set_members row that referenced it. It
// destroys no Calendar and no Event.
func (r *CalendarSetRepository) Delete(ctx context.Context, id, userID, workspaceID int64) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM calendar_sets WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		id, userID, workspaceID,
	)
	if err != nil {
		return fmt.Errorf("delete calendar set: %w", err)
	}
	return requireAffected(res)
}

// AddCalendar puts calendarID into setID's membership (#302, ADR-0082).
// INSERT OR IGNORE rather than erroring on a Calendar already in the Set —
// the endpoint above this is a PUT, so re-adding an existing member is a
// no-op, not a conflict.
func (r *CalendarSetRepository) AddCalendar(ctx context.Context, setID int64, calendarID string) error {
	if _, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO calendar_set_members (set_id, calendar_id) VALUES (?, ?)`,
		setID, calendarID,
	); err != nil {
		return fmt.Errorf("insert calendar set member: %w", err)
	}
	return nil
}

// RemoveCalendar takes calendarID out of setID's membership.
func (r *CalendarSetRepository) RemoveCalendar(ctx context.Context, setID int64, calendarID string) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM calendar_set_members WHERE set_id = ? AND calendar_id = ?`,
		setID, calendarID,
	)
	if err != nil {
		return fmt.Errorf("delete calendar set member: %w", err)
	}
	return requireAffected(res)
}

// ListCalendarIDs returns every Calendar id belonging to setID — the
// membership dialog's "which Calendars are already in" answer, and the
// switcher's own eventual read.
func (r *CalendarSetRepository) ListCalendarIDs(ctx context.Context, setID int64) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT calendar_id FROM calendar_set_members WHERE set_id = ?`,
		setID,
	)
	if err != nil {
		return nil, fmt.Errorf("list calendar set members: %w", err)
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan calendar set member: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
