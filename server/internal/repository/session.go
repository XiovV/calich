package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Session struct {
	ID                    int64
	UserID                int64
	RefreshTokenHash      string
	RefreshTokenExpiresAt time.Time
	CreatedAt             time.Time
}

type SessionRepository struct {
	db *sql.DB
}

func NewSessionRepository(db *sql.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

func (r *SessionRepository) Create(ctx context.Context, userID int64, refreshTokenHash string, refreshTokenExpiresAt time.Time) (Session, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO sessions (user_id, refresh_token_hash, refresh_token_expires_at) VALUES (?, ?, ?)`,
		userID, refreshTokenHash, refreshTokenExpiresAt,
	)
	if err != nil {
		return Session{}, fmt.Errorf("insert session: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return Session{}, fmt.Errorf("get inserted session id: %w", err)
	}

	return r.getByID(ctx, id)
}

func (r *SessionRepository) GetByRefreshTokenHash(ctx context.Context, refreshTokenHash string) (Session, error) {
	return r.scanSession(r.db.QueryRowContext(ctx,
		`SELECT id, user_id, refresh_token_hash, refresh_token_expires_at, created_at FROM sessions WHERE refresh_token_hash = ?`,
		refreshTokenHash,
	))
}

func (r *SessionRepository) Delete(ctx context.Context, id int64) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteAllForUser invalidates every session belonging to a user — used when
// their password changes, so a stolen refresh token stops working.
func (r *SessionRepository) DeleteAllForUser(ctx context.Context, userID int64) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete sessions for user: %w", err)
	}
	return nil
}

func (r *SessionRepository) getByID(ctx context.Context, id int64) (Session, error) {
	return r.scanSession(r.db.QueryRowContext(ctx,
		`SELECT id, user_id, refresh_token_hash, refresh_token_expires_at, created_at FROM sessions WHERE id = ?`, id,
	))
}

func (r *SessionRepository) scanSession(row *sql.Row) (Session, error) {
	var s Session
	err := row.Scan(&s.ID, &s.UserID, &s.RefreshTokenHash, &s.RefreshTokenExpiresAt, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("scan session: %w", err)
	}
	return s, nil
}
