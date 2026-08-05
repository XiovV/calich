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
	// Email is the Email-Channel Reminder's recipient (ADR-0021, ADR-0010).
	// Nil until the user sets it on the Settings page.
	Email *string
	// SyncedDeviceRemindersEnabled is "let my synced devices show reminder
	// pop-ups (disable in-app reminder notifications)" (ADR-0027): when true,
	// the scheduler skips the Notification channel for this user, since a
	// synced device already fires its own alarm from the VALARM. The Email
	// channel is never gated by this. Defaults off.
	SyncedDeviceRemindersEnabled bool
	CreatedAt                    time.Time
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
		`SELECT id, username, password_hash, must_change_password, email, synced_device_reminders_enabled, created_at FROM users WHERE id = ?`, id,
	))
}

func (r *UserRepository) GetByUsername(ctx context.Context, username string) (User, error) {
	return r.scanUser(r.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, must_change_password, email, synced_device_reminders_enabled, created_at FROM users WHERE username = ?`, username,
	))
}

// UpdateEmail sets userID's account email — the Email-Channel Reminder
// recipient (ADR-0021). An empty string clears it back to unset.
func (r *UserRepository) UpdateEmail(ctx context.Context, userID int64, email string) (User, error) {
	var value any
	if email != "" {
		value = email
	}

	if _, err := r.db.ExecContext(ctx, `UPDATE users SET email = ? WHERE id = ?`, value, userID); err != nil {
		return User{}, fmt.Errorf("update email: %w", err)
	}
	return r.GetByID(ctx, userID)
}

// UpdateSyncedDeviceReminders sets userID's "let my synced devices show
// reminder pop-ups" preference (ADR-0027).
func (r *UserRepository) UpdateSyncedDeviceReminders(ctx context.Context, userID int64, enabled bool) (User, error) {
	if _, err := r.db.ExecContext(ctx, `UPDATE users SET synced_device_reminders_enabled = ? WHERE id = ?`, enabled, userID); err != nil {
		return User{}, fmt.Errorf("update synced device reminders preference: %w", err)
	}
	return r.GetByID(ctx, userID)
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
		`SELECT id, username, password_hash, must_change_password, email, synced_device_reminders_enabled, created_at FROM users ORDER BY id LIMIT 1`,
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
	var email sql.NullString
	// modernc.org/sqlite converts the TIMESTAMP column straight into time.Time
	// based on the declared column type — no manual parsing needed here.
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.MustChangePassword, &email, &u.SyncedDeviceRemindersEnabled, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("scan user: %w", err)
	}
	if email.Valid {
		u.Email = &email.String
	}
	return u, nil
}
