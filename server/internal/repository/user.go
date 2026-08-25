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
	ID   int64
	Name string
	// Email is the account's unique login identifier (ADR-0047) — required,
	// unique instance-wide, and also the Email-Channel Reminder's recipient
	// (ADR-0021).
	Email              string
	PasswordHash       string
	MustChangePassword bool
	// SyncedDeviceRemindersEnabled is "let my synced devices show reminder
	// pop-ups (disable in-app reminder notifications)" (ADR-0027): when true,
	// the scheduler skips the Notification channel for this user, since a
	// synced device already fires its own alarm from the VALARM. The Email
	// channel is never gated by this. Defaults off.
	SyncedDeviceRemindersEnabled bool
	// IsDisabled blocks CalDAV Basic auth while leaving everything the User
	// owns untouched (ADR-0037, ADR-0044). Self-chosen and self-reversible
	// only — there is no instance-wide Admin left to disable someone else's
	// account. Login and Refresh no longer refuse a Disabled User outright
	// (AuthService.Login, AuthService.Refresh); they still issue a Session so
	// the User can reach the one action available to them — re-activating —
	// and every other route is closed off by httpauth.RequireEnabledUser
	// instead.
	IsDisabled bool
	CreatedAt  time.Time
	// TokenVersion is embedded in every access token's "tv" claim at mint
	// time (AuthService.newAccessToken) and compared against this column's
	// current value on every AuthService.Authenticate call (#242, ADR-0071).
	// UpdatePassword increments it, so a token minted before a password
	// change stops authenticating immediately rather than riding out its
	// full accessTokenTTL.
	TokenVersion int64

	// WeekStart is a date-fns weekStartsOn index (0 = Sunday .. 6 = Saturday):
	// the first column of the Month grid, the first day of the Week view, and
	// the mini calendars. Never fed into a Recurrence rule's WKST (ADR-0039).
	WeekStart int
	// DefaultView seeds Active view when a Session is established; it is
	// never written back (ADR-0039).
	DefaultView string
	// TimeFormat is "12h" or "24h", applied wherever the app formats a time
	// itself (ADR-0039).
	TimeFormat string
	// WorkingHoursStart and WorkingHoursEnd are minutes-since-midnight
	// (0..1439) shading the time outside that range in Day and Week view.
	// Nil means no shading (ADR-0039).
	WorkingHoursStart *int
	WorkingHoursEnd   *int
}

// userColumns is the column list every SELECT against users shares, in the
// exact order scanUserFields expects — kept as one constant so the two never
// drift apart across GetByID, GetByIDs, GetByEmail, ListEnabledExcluding,
// and First.
const userColumns = `id, name, password_hash, must_change_password, email, synced_device_reminders_enabled, is_disabled, created_at, week_start, default_view, time_format, working_hours_start, working_hours_end, token_version`

type UserRepository struct {
	db DBTX
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018) — Delete
// needs this to pair with CalendarRepository.TransferOwnershipOne under the
// "transfer" disposition (ADR-0044).
func (r *UserRepository) WithTx(tx *sql.Tx) *UserRepository {
	return &UserRepository{db: tx}
}

// ErrEmailTaken is returned by Create and UpdateEmail when the email is
// already in use — email is unique instance-wide (ADR-0047).
var ErrEmailTaken = errors.New("email already taken")

func (r *UserRepository) Create(ctx context.Context, name, email, passwordHash string, mustChangePassword bool) (User, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO users (name, email, password_hash, must_change_password) VALUES (?, ?, ?, ?)`,
		name, email, passwordHash, mustChangePassword,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return User{}, ErrEmailTaken
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
		`SELECT `+userColumns+` FROM users WHERE id = ?`, id,
	))
}

// GetByIDs returns the Users named by ids, keyed by id — a batched
// counterpart to GetByID for callers resolving many ids at once (e.g. Event
// attribution, #118), costing one query rather than one per id. An id with
// no matching row (a deleted User) is simply absent from the map.
func (r *UserRepository) GetByIDs(ctx context.Context, ids []int64) (map[int64]User, error) {
	result := make(map[int64]User)
	if len(ids) == 0 {
		return result, nil
	}

	query := `SELECT ` + userColumns + ` FROM users WHERE id IN (` + placeholders(len(ids)) + `)`
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	users, err := collectRows(rows, r.scanUserRow)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		result[u.ID] = u
	}

	return result, nil
}

// GetByEmail resolves the User identified by email (ADR-0047) — the
// identifier resolution used by web sign-in, CalDAV Basic auth, Share-target
// resolution, and Workspace Invite accept. email is unique instance-wide, so
// this is an exact-match lookup.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (User, error) {
	return r.scanUser(r.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE email = ?`, email,
	))
}

// ListEnabledExcluding returns every enabled User except excludeID, ordered
// by name — the User directory (#113): any authenticated caller may see
// who else has an account, so they can pick a Share recipient, but a
// Disabled User is hidden the same way Share already hides one (ADR-0037),
// and the caller never sees themselves in their own picker.
func (r *UserRepository) ListEnabledExcluding(ctx context.Context, excludeID int64) ([]User, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+userColumns+`
		 FROM users WHERE is_disabled = 0 AND id != ? ORDER BY name`, excludeID,
	)
	if err != nil {
		return nil, fmt.Errorf("query enabled users: %w", err)
	}
	return collectRows(rows, r.scanUserRow)
}

// UpdateEmail changes userID's login identifier (ADR-0047) — also the
// Email-Channel Reminder recipient (ADR-0021). A unique-constraint violation
// is surfaced as ErrEmailTaken exactly like Create, since both hit the same
// users.email UNIQUE column. Email is mandatory, so unlike the old username
// rename this never accepts an empty string to clear it.
func (r *UserRepository) UpdateEmail(ctx context.Context, userID int64, email string) (User, error) {
	if _, err := r.db.ExecContext(ctx, `UPDATE users SET email = ? WHERE id = ?`, email, userID); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return User{}, ErrEmailTaken
		}
		return User{}, fmt.Errorf("update email: %w", err)
	}
	return r.GetByID(ctx, userID)
}

// UpdateName renames userID's display name (#125). Name is not unique
// (ADR-0047), so unlike UpdateEmail this never fails on a conflict.
func (r *UserRepository) UpdateName(ctx context.Context, userID int64, name string) (User, error) {
	if _, err := r.db.ExecContext(ctx, `UPDATE users SET name = ? WHERE id = ?`, name, userID); err != nil {
		return User{}, fmt.Errorf("update name: %w", err)
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

// UpdateWeekStart sets userID's Week start preference (ADR-0039) — a
// date-fns weekStartsOn index, 0..6. Range validation happens in
// AuthService.UpdatePreferences.
func (r *UserRepository) UpdateWeekStart(ctx context.Context, userID int64, weekStart int) (User, error) {
	if _, err := r.db.ExecContext(ctx, `UPDATE users SET week_start = ? WHERE id = ?`, weekStart, userID); err != nil {
		return User{}, fmt.Errorf("update week start preference: %w", err)
	}
	return r.GetByID(ctx, userID)
}

// UpdateDefaultView sets userID's Default view preference (ADR-0039) — one
// of day/week/month/year. Validation happens in AuthService.UpdatePreferences.
func (r *UserRepository) UpdateDefaultView(ctx context.Context, userID int64, defaultView string) (User, error) {
	if _, err := r.db.ExecContext(ctx, `UPDATE users SET default_view = ? WHERE id = ?`, defaultView, userID); err != nil {
		return User{}, fmt.Errorf("update default view preference: %w", err)
	}
	return r.GetByID(ctx, userID)
}

// UpdateTimeFormat sets userID's Time format preference (ADR-0039) — "12h"
// or "24h". Validation happens in AuthService.UpdatePreferences.
func (r *UserRepository) UpdateTimeFormat(ctx context.Context, userID int64, timeFormat string) (User, error) {
	if _, err := r.db.ExecContext(ctx, `UPDATE users SET time_format = ? WHERE id = ?`, timeFormat, userID); err != nil {
		return User{}, fmt.Errorf("update time format preference: %w", err)
	}
	return r.GetByID(ctx, userID)
}

// UpdateWorkingHours sets userID's Working hours preference (ADR-0039) — a
// minutes-since-midnight range 0..1439, or both nil to clear back to no
// shading. Pair validity (both set or both nil, start < end) is checked in
// AuthService.UpdatePreferences.
func (r *UserRepository) UpdateWorkingHours(ctx context.Context, userID int64, start, end *int) (User, error) {
	var startVal, endVal any
	if start != nil {
		startVal = *start
	}
	if end != nil {
		endVal = *end
	}

	if _, err := r.db.ExecContext(ctx, `UPDATE users SET working_hours_start = ?, working_hours_end = ? WHERE id = ?`, startVal, endVal, userID); err != nil {
		return User{}, fmt.Errorf("update working hours preference: %w", err)
	}
	return r.GetByID(ctx, userID)
}

// UpdatePassword sets a new password hash, clears must_change_password, and
// bumps token_version — what AuthService.Authenticate compares a bearer
// access token's "tv" claim against (#242, ADR-0071), so every access token
// minted before this call stops authenticating on its very next use rather
// than riding out its full TTL. Returns the updated User so callers that
// immediately mint a fresh token (AuthService.ChangePassword) have the new
// token_version to hand without a second round trip.
func (r *UserRepository) UpdatePassword(ctx context.Context, userID int64, passwordHash string) (User, error) {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, must_change_password = 0, token_version = token_version + 1 WHERE id = ?`,
		passwordHash, userID,
	)
	if err != nil {
		return User{}, fmt.Errorf("update password: %w", err)
	}
	return r.GetByID(ctx, userID)
}

// GetTokenVersion returns userID's token_version alone — the single column
// AuthService.Authenticate needs on every authenticated request (#242,
// ADR-0071), cheaper than GetByID's full row.
func (r *UserRepository) GetTokenVersion(ctx context.Context, userID int64) (int64, error) {
	var tokenVersion int64
	err := r.db.QueryRowContext(ctx, `SELECT token_version FROM users WHERE id = ?`, userID).Scan(&tokenVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("get token version: %w", err)
	}
	return tokenVersion, nil
}

// First returns the earliest-created user — the sole user in today's
// single-user instance (ADR-0010).
func (r *UserRepository) First(ctx context.Context) (User, error) {
	return r.scanUser(r.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users ORDER BY id LIMIT 1`,
	))
}

func (r *UserRepository) Count(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return count, nil
}

// SetDisabled disables or re-enables userID's account (ADR-0044). Self-chosen
// and self-reversible only — callers are responsible for the sole-Workspace-
// Owner guard and for tearing down live Sessions; this method applies
// whatever it's told.
func (r *UserRepository) SetDisabled(ctx context.Context, userID int64, isDisabled bool) (User, error) {
	if _, err := r.db.ExecContext(ctx, `UPDATE users SET is_disabled = ? WHERE id = ?`, isDisabled, userID); err != nil {
		return User{}, fmt.Errorf("set disabled: %w", err)
	}
	return r.GetByID(ctx, userID)
}

// Delete removes userID's account outright (ADR-0044). Everything that
// references it either cascades — Sessions, App passwords, Shares granted
// to userID, Workspace Memberships, and, unless the caller has already
// reassigned them away via CalendarRepository.TransferOwnershipOne, the
// Calendars userID owned (whose own cascade takes their Events and Shares
// with them) — or clears via ON DELETE SET NULL (an Event's created_by
// attribution). Callers are responsible for the sole-Workspace-Owner guard,
// for retiring every Workspace userID solely owns first (workspaces.
// owner_user_id has no ON DELETE behaviour), and for the disposition of
// owned Calendars; this method applies whatever it's told.
func (r *UserRepository) Delete(ctx context.Context, userID int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return requireAffected(res)
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

func (r *UserRepository) scanUserRow(row rowScanner) (User, error) {
	u, err := scanUserFields(row.Scan)
	if err != nil {
		return User{}, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}

// scanUserFields scans one users row via scan, which is either a *sql.Row's
// or *sql.Rows' Scan — the column list and null-handling are identical
// either way, so GetByIDs doesn't have to duplicate GetByID's.
func scanUserFields(scan func(dest ...any) error) (User, error) {
	var u User
	var workingHoursStart, workingHoursEnd sql.NullInt64
	// modernc.org/sqlite converts the TIMESTAMP column straight into time.Time
	// based on the declared column type — no manual parsing needed here.
	err := scan(
		&u.ID, &u.Name, &u.PasswordHash, &u.MustChangePassword, &u.Email, &u.SyncedDeviceRemindersEnabled, &u.IsDisabled, &u.CreatedAt,
		&u.WeekStart, &u.DefaultView, &u.TimeFormat, &workingHoursStart, &workingHoursEnd, &u.TokenVersion,
	)
	if err != nil {
		return User{}, err
	}
	if workingHoursStart.Valid {
		v := int(workingHoursStart.Int64)
		u.WorkingHoursStart = &v
	}
	if workingHoursEnd.Valid {
		v := int(workingHoursEnd.Int64)
		u.WorkingHoursEnd = &v
	}
	return u, nil
}
