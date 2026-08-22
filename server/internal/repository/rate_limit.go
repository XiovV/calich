package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RateLimitScopeAuth is Login's and CalDAV Basic auth's shared scope (#240,
// ADR-0070) — the same "guess a password against one account" problem
// behind two transports. RateLimitScopeRegister is Register's own scope,
// counted independently.
const (
	RateLimitScopeAuth     = "auth"
	RateLimitScopeRegister = "register"
)

// RateLimitKeyEmail and RateLimitKeyIP are the two key_type values a
// RateLimitAttemptRepository row may carry (#240, ADR-0070).
const (
	RateLimitKeyEmail = "email"
	RateLimitKeyIP    = "ip"
)

// ErrRateLimited is AuthRateLimiter's sentinel (#240, ADR-0070), mirrored as
// service.ErrAuthRateLimitExceeded so handlers and httpauth can recognize it
// without importing this package's whole error surface.
var ErrRateLimited = errors.New("rate limited")

// RateLimitAttemptRepository stores AuthRateLimiter's rolling-window
// attempts (#240, ADR-0070): one row per throttled call against Login,
// CalDAV Basic auth, or Register, counted by (scope, key_type, key_value)
// rather than by an authenticated actor — unlike OutboxRepository's
// actor_user_id ceiling (ADR-0058), none of the three surfaces this guards
// has an authenticated actor to key on.
type RateLimitAttemptRepository struct {
	db DBTX
}

func NewRateLimitAttemptRepository(db *sql.DB) *RateLimitAttemptRepository {
	return &RateLimitAttemptRepository{db: db}
}

// CountSince counts scope/keyType/keyValue's attempts recorded at or after
// since, the rolling-window read AuthRateLimiter checks before letting a
// request through. since is normalized to UTC first, mirroring
// OutboxRepository.CountByActorSince — created_at's CURRENT_TIMESTAMP
// default is always UTC with no offset suffix, and a local-zone time.Time
// bound as-is would break the lexical comparison SQLite does against this
// TEXT column.
func (r *RateLimitAttemptRepository) CountSince(ctx context.Context, scope, keyType, keyValue string, since time.Time) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM auth_rate_limit_attempts WHERE scope = ? AND key_type = ? AND key_value = ? AND created_at >= ?`,
		scope, keyType, keyValue, since.UTC(),
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count rate limit attempts: %w", err)
	}
	return count, nil
}

// Record writes one attempt row for scope/keyType/keyValue, first deleting
// every row in that same scope (any key) older than olderThan —
// self-pruning, so the table never grows past what one rolling window can
// hold without a separate sweeper to run, stop, and reason about at
// shutdown. Scoped to scope rather than to keyType/keyValue alone: a
// key-specific prune would never fire for a key that's only ever written
// once — an attacker rotating the submitted Email (or, behind a shared
// proxy IP, unable to rotate IP but still able to rotate Email) on every
// request would otherwise leave one permanent row behind per distinct
// value, since nothing else ever revisits that exact key to prune it.
// Scoped to scope rather than left unconditional: Login/CalDAV's 15-minute
// window and Register's 1-hour window are different cutoffs, and pruning
// across scopes with one call's own olderThan would delete another scope's
// still-live rows.
func (r *RateLimitAttemptRepository) Record(ctx context.Context, scope, keyType, keyValue string, olderThan time.Time) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM auth_rate_limit_attempts WHERE scope = ? AND created_at < ?`,
		scope, olderThan.UTC(),
	); err != nil {
		return fmt.Errorf("prune rate limit attempts: %w", err)
	}

	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO auth_rate_limit_attempts (scope, key_type, key_value) VALUES (?, ?, ?)`,
		scope, keyType, keyValue,
	); err != nil {
		return fmt.Errorf("insert rate limit attempt: %w", err)
	}
	return nil
}

// Clear deletes every attempt recorded for scope/keyType/keyValue —
// AuthRateLimiter's success path, resetting a credential's count rather
// than leaving it to decay out of the rolling window on its own (#240,
// ADR-0070).
func (r *RateLimitAttemptRepository) Clear(ctx context.Context, scope, keyType, keyValue string) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM auth_rate_limit_attempts WHERE scope = ? AND key_type = ? AND key_value = ?`,
		scope, keyType, keyValue,
	); err != nil {
		return fmt.Errorf("clear rate limit attempts: %w", err)
	}
	return nil
}
