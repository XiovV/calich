package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

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
	// IsAdmin is authority over who exists on the instance — creating,
	// listing, and administering other Users — and never over what they can
	// see: an Admin still needs a Share to read another User's Calendars
	// (ADR-0037).
	IsAdmin bool
	// IsDisabled blocks Login, Refresh, and CalDAV Basic auth while leaving
	// everything the User owns untouched (ADR-0037). It is a property of the
	// account, never of the data.
	IsDisabled bool
	CreatedAt  time.Time
}

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

// ErrUsernameTaken is returned by Create when the username is already in
// use — usernames are unique instance-wide (ADR-0037).
var ErrUsernameTaken = errors.New("username already taken")

func (r *UserRepository) Create(ctx context.Context, username, passwordHash string, mustChangePassword bool) (User, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, must_change_password) VALUES (?, ?, ?)`,
		username, passwordHash, mustChangePassword,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return User{}, ErrUsernameTaken
		}
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
		`SELECT id, username, password_hash, must_change_password, email, synced_device_reminders_enabled, is_admin, is_disabled, created_at FROM users WHERE id = ?`, id,
	))
}

func (r *UserRepository) GetByUsername(ctx context.Context, username string) (User, error) {
	return r.scanUser(r.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, must_change_password, email, synced_device_reminders_enabled, is_admin, is_disabled, created_at FROM users WHERE username = ?`, username,
	))
}

// List returns every account on the instance, ordered by id (i.e. creation
// order) so the bootstrapped Admin always leads the list.
func (r *UserRepository) List(ctx context.Context) ([]User, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, username, password_hash, must_change_password, email, synced_device_reminders_enabled, is_admin, is_disabled, created_at FROM users ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		u, err := r.scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}

	return users, nil
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
		`SELECT id, username, password_hash, must_change_password, email, synced_device_reminders_enabled, is_admin, is_disabled, created_at FROM users ORDER BY id LIMIT 1`,
	))
}

func (r *UserRepository) Count(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return count, nil
}

// CountAdmins reports how many accounts currently hold Admin — used to
// guard the last remaining Admin against demotion (ADR-0037).
func (r *UserRepository) CountAdmins(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return count, nil
}

// SetAdmin grants or revokes Admin for userID. Callers are responsible for
// the last-Admin guard (ADR-0037) — this method applies whatever it's told.
func (r *UserRepository) SetAdmin(ctx context.Context, userID int64, isAdmin bool) (User, error) {
	if _, err := r.db.ExecContext(ctx, `UPDATE users SET is_admin = ? WHERE id = ?`, isAdmin, userID); err != nil {
		return User{}, fmt.Errorf("set admin: %w", err)
	}
	return r.GetByID(ctx, userID)
}

// CountEnabledAdmins reports how many non-Disabled accounts currently hold
// Admin — used to guard the last remaining Admin against being disabled
// (ADR-0037): a Disabled Admin can no longer administer the instance, so it
// must not count as one still can.
func (r *UserRepository) CountEnabledAdmins(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_admin = 1 AND is_disabled = 0`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count enabled admins: %w", err)
	}
	return count, nil
}

// SetDisabled disables or re-enables userID's account (ADR-0037). Callers
// are responsible for the last-Admin guard and for tearing down live
// Sessions — this method applies whatever it's told.
func (r *UserRepository) SetDisabled(ctx context.Context, userID int64, isDisabled bool) (User, error) {
	if _, err := r.db.ExecContext(ctx, `UPDATE users SET is_disabled = ? WHERE id = ?`, isDisabled, userID); err != nil {
		return User{}, fmt.Errorf("set disabled: %w", err)
	}
	return r.GetByID(ctx, userID)
}

// ResetPassword sets a new password hash and, unlike UpdatePassword, sets
// must_change_password rather than clearing it: an Admin-set password is a
// temporary one the User must replace on next login, reusing the same gate
// that closes the bootstrap window (ADR-0010, ADR-0037).
func (r *UserRepository) ResetPassword(ctx context.Context, userID int64, passwordHash string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, must_change_password = 1 WHERE id = ?`,
		passwordHash, userID,
	)
	if err != nil {
		return fmt.Errorf("reset password: %w", err)
	}
	return nil
}

func (r *UserRepository) scanUser(row *sql.Row) (User, error) {
	u, err := scanUserFields(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}

func (r *UserRepository) scanUserRow(rows *sql.Rows) (User, error) {
	u, err := scanUserFields(rows.Scan)
	if err != nil {
		return User{}, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}

// scanUserFields scans one users row via scan, which is either a *sql.Row's
// or *sql.Rows' Scan — the column list and null-handling are identical
// either way, so List doesn't have to duplicate GetByID's.
func scanUserFields(scan func(dest ...any) error) (User, error) {
	var u User
	var email sql.NullString
	// modernc.org/sqlite converts the TIMESTAMP column straight into time.Time
	// based on the declared column type — no manual parsing needed here.
	err := scan(&u.ID, &u.Username, &u.PasswordHash, &u.MustChangePassword, &email, &u.SyncedDeviceRemindersEnabled, &u.IsAdmin, &u.IsDisabled, &u.CreatedAt)
	if err != nil {
		return User{}, err
	}
	if email.Valid {
		u.Email = &email.String
	}
	return u, nil
}
