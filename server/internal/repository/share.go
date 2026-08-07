package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Role values a Share may hold (ADR-0034). Stored verbatim in
// calendar_shares.role, and constrained there by a CHECK to just these two.
const (
	RoleViewer = "viewer"
	RoleEditor = "editor"
)

// CalendarShare is a Share (ADR-0034): the grant binding one Calendar to one
// User with one Role. The Owner never has one — ownership stays implicit in
// calendars.user_id.
type CalendarShare struct {
	CalendarID string
	UserID     int64
	Role       string
	CreatedAt  time.Time
}

type CalendarShareRepository struct {
	db *sql.DB
}

func NewCalendarShareRepository(db *sql.DB) *CalendarShareRepository {
	return &CalendarShareRepository{db: db}
}

// Upsert grants calendarID a Share to userID with role, or changes an
// existing Share's role if one already exists (ADR-0034: a Share's
// (calendar_id, user_id) pair is unique, so granting again is how a Role
// changes).
func (r *CalendarShareRepository) Upsert(ctx context.Context, calendarID string, userID int64, role string) (CalendarShare, error) {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO calendar_shares (calendar_id, user_id, role) VALUES (?, ?, ?)
		 ON CONFLICT(calendar_id, user_id) DO UPDATE SET role = excluded.role`,
		calendarID, userID, role,
	); err != nil {
		return CalendarShare{}, fmt.Errorf("upsert calendar share: %w", err)
	}
	return r.Get(ctx, calendarID, userID)
}

// Get returns the Share granting userID Access to calendarID, or
// ErrNotFound if none exists — the Access resolver's lookup for a non-Owner
// (ADR-0034).
func (r *CalendarShareRepository) Get(ctx context.Context, calendarID string, userID int64) (CalendarShare, error) {
	var s CalendarShare
	err := r.db.QueryRowContext(ctx,
		`SELECT calendar_id, user_id, role, created_at FROM calendar_shares WHERE calendar_id = ? AND user_id = ?`,
		calendarID, userID,
	).Scan(&s.CalendarID, &s.UserID, &s.Role, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return CalendarShare{}, ErrNotFound
	}
	if err != nil {
		return CalendarShare{}, fmt.Errorf("scan calendar share: %w", err)
	}
	return s, nil
}

// Delete revokes calendarID's Share to userID — an Owner revoking it, or the
// shared User leaving it themselves (ADR-0034); both are the same row
// removal, distinguished only by who's authorized to call it, which is the
// service layer's job.
func (r *CalendarShareRepository) Delete(ctx context.Context, calendarID string, userID int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM calendar_shares WHERE calendar_id = ? AND user_id = ?`, calendarID, userID)
	if err != nil {
		return fmt.Errorf("delete calendar share: %w", err)
	}
	return requireAffected(res)
}

// CalendarShareWithUsername pairs a Share with the Username of the User it
// grants Access to — ListByCalendarWithUsername's row, since an Owner
// reviewing who has Access to their Calendar wants to see a Username, not
// a bare user id.
type CalendarShareWithUsername struct {
	CalendarShare
	Username string
}

// ListByCalendarWithUsername returns every Share on calendarID joined
// against users, ordered by when each was granted — the Owner-facing "who
// has Access to my Calendar, and with what Role" listing (ADR-0034).
func (r *CalendarShareRepository) ListByCalendarWithUsername(ctx context.Context, calendarID string) ([]CalendarShareWithUsername, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT s.calendar_id, s.user_id, s.role, s.created_at, u.username
		 FROM calendar_shares s
		 JOIN users u ON u.id = s.user_id
		 WHERE s.calendar_id = ?
		 ORDER BY s.created_at, s.user_id`,
		calendarID,
	)
	if err != nil {
		return nil, fmt.Errorf("list calendar shares with username: %w", err)
	}
	defer rows.Close()

	shares := []CalendarShareWithUsername{}
	for rows.Next() {
		var s CalendarShareWithUsername
		if err := rows.Scan(&s.CalendarID, &s.UserID, &s.Role, &s.CreatedAt, &s.Username); err != nil {
			return nil, fmt.Errorf("scan calendar share with username: %w", err)
		}
		shares = append(shares, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate calendar shares with username: %w", err)
	}
	return shares, nil
}
