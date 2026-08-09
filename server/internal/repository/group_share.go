package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CalendarGroupShare is a Group-targeted Share (ADR-0045): the grant binding
// one Calendar to one Group with one Role, alongside CalendarShare's
// per-User grant.
type CalendarGroupShare struct {
	CalendarID string
	GroupID    int64
	Role       string
	CreatedAt  time.Time
}

type CalendarGroupShareRepository struct {
	db *sql.DB
}

func NewCalendarGroupShareRepository(db *sql.DB) *CalendarGroupShareRepository {
	return &CalendarGroupShareRepository{db: db}
}

// Upsert grants calendarID a Share to groupID with role, or changes an
// existing Share's role if one already exists (mirrors
// CalendarShareRepository.Upsert).
func (r *CalendarGroupShareRepository) Upsert(ctx context.Context, calendarID string, groupID int64, role string) (CalendarGroupShare, error) {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO calendar_group_shares (calendar_id, group_id, role) VALUES (?, ?, ?)
		 ON CONFLICT(calendar_id, group_id) DO UPDATE SET role = excluded.role`,
		calendarID, groupID, role,
	); err != nil {
		return CalendarGroupShare{}, fmt.Errorf("upsert calendar group share: %w", err)
	}
	return r.Get(ctx, calendarID, groupID)
}

// Get returns the Share granting groupID Access to calendarID, or
// ErrNotFound if none exists.
func (r *CalendarGroupShareRepository) Get(ctx context.Context, calendarID string, groupID int64) (CalendarGroupShare, error) {
	var s CalendarGroupShare
	err := r.db.QueryRowContext(ctx,
		`SELECT calendar_id, group_id, role, created_at FROM calendar_group_shares WHERE calendar_id = ? AND group_id = ?`,
		calendarID, groupID,
	).Scan(&s.CalendarID, &s.GroupID, &s.Role, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return CalendarGroupShare{}, ErrNotFound
	}
	if err != nil {
		return CalendarGroupShare{}, fmt.Errorf("scan calendar group share: %w", err)
	}
	return s, nil
}

// Delete revokes calendarID's Share to groupID.
func (r *CalendarGroupShareRepository) Delete(ctx context.Context, calendarID string, groupID int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM calendar_group_shares WHERE calendar_id = ? AND group_id = ?`, calendarID, groupID)
	if err != nil {
		return fmt.Errorf("delete calendar group share: %w", err)
	}
	return requireAffected(res)
}

// CountByCalendar returns how many Group Shares calendarID carries —
// CalendarShareRepository.CountByCalendar's Group-share counterpart, both
// summed into OwnershipMeta's ShareCount.
func (r *CalendarGroupShareRepository) CountByCalendar(ctx context.Context, calendarID string) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM calendar_group_shares WHERE calendar_id = ?`, calendarID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count calendar group shares: %w", err)
	}
	return count, nil
}

// CalendarGroupShareWithGroupName pairs a Group Share with the Name of the
// Group it grants Access to — ListByCalendarWithGroupName's row, mirroring
// CalendarShareWithUsername.
type CalendarGroupShareWithGroupName struct {
	CalendarGroupShare
	GroupName string
}

// ListByCalendarWithGroupName returns every Group Share on calendarID joined
// against groups, ordered by when each was granted — the Owner-facing "which
// Groups have Access to my Calendar, and with what Role" listing.
func (r *CalendarGroupShareRepository) ListByCalendarWithGroupName(ctx context.Context, calendarID string) ([]CalendarGroupShareWithGroupName, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT s.calendar_id, s.group_id, s.role, s.created_at, g.name
		 FROM calendar_group_shares s
		 JOIN groups g ON g.id = s.group_id
		 WHERE s.calendar_id = ?
		 ORDER BY s.created_at, s.group_id`,
		calendarID,
	)
	if err != nil {
		return nil, fmt.Errorf("list calendar group shares with group name: %w", err)
	}
	defer rows.Close()

	shares := []CalendarGroupShareWithGroupName{}
	for rows.Next() {
		var s CalendarGroupShareWithGroupName
		if err := rows.Scan(&s.CalendarID, &s.GroupID, &s.Role, &s.CreatedAt, &s.GroupName); err != nil {
			return nil, fmt.Errorf("scan calendar group share with group name: %w", err)
		}
		shares = append(shares, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate calendar group shares with group name: %w", err)
	}
	return shares, nil
}

// BestRoleForUser returns the most permissive Role any Group Share on
// calendarID grants userID, resolved off userID's current group_members rows
// — never a snapshot taken at share time (ADR-0045). Returns ErrNotFound if
// userID belongs to no Group holding a Share on calendarID.
func (r *CalendarGroupShareRepository) BestRoleForUser(ctx context.Context, calendarID string, userID int64) (string, error) {
	var role string
	err := r.db.QueryRowContext(ctx,
		`SELECT s.role
		 FROM calendar_group_shares s
		 JOIN group_members gm ON gm.group_id = s.group_id
		 WHERE s.calendar_id = ? AND gm.user_id = ?
		 ORDER BY CASE s.role WHEN 'editor' THEN 2 ELSE 1 END DESC
		 LIMIT 1`,
		calendarID, userID,
	).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get best calendar group share role: %w", err)
	}
	return role, nil
}
