package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("not found")

type User struct {
	ID                 int64
	Username           string
	PasswordHash       string
	MustChangePassword bool
	CreatedAt          time.Time
}

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, username, passwordHash string, mustChangePassword bool) (User, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, must_change_password) VALUES (?, ?, ?)`,
		username, passwordHash, mustChangePassword,
	)
	if err != nil {
		return User{}, fmt.Errorf("insert user: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return User{}, fmt.Errorf("get inserted user id: %w", err)
	}

	return r.GetByID(ctx, id)
}

func (r *UserRepository) GetByID(ctx context.Context, id int64) (User, error) {
	return r.scanUser(r.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, must_change_password, created_at FROM users WHERE id = ?`, id,
	))
}

func (r *UserRepository) GetByUsername(ctx context.Context, username string) (User, error) {
	return r.scanUser(r.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, must_change_password, created_at FROM users WHERE username = ?`, username,
	))
}

// UpdatePassword sets a new password hash and clears must_change_password.
func (r *UserRepository) UpdatePassword(ctx context.Context, userID int64, passwordHash string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, must_change_password = 0 WHERE id = ?`,
		passwordHash, userID,
	)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	return nil
}

// First returns the earliest-created user — the sole user in today's
// single-user instance (ADR-0010).
func (r *UserRepository) First(ctx context.Context) (User, error) {
	return r.scanUser(r.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, must_change_password, created_at FROM users ORDER BY id LIMIT 1`,
	))
}

func (r *UserRepository) Count(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return count, nil
}

func (r *UserRepository) scanUser(row *sql.Row) (User, error) {
	var u User
	// modernc.org/sqlite converts the TIMESTAMP column straight into time.Time
	// based on the declared column type — no manual parsing needed here.
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.MustChangePassword, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}
