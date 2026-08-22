package repository

import (
	"context"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/db"
)

func newTestRateLimitAttemptRepository(t *testing.T) *RateLimitAttemptRepository {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	return NewRateLimitAttemptRepository(sqlDB)
}

func TestRateLimitAttemptRepository_CountSince_CountsOnlyMatchingKeyWithinWindow(t *testing.T) {
	repo := newTestRateLimitAttemptRepository(t)
	ctx := context.Background()

	farPast := time.Now().Add(-24 * time.Hour)
	if err := repo.Record(ctx, RateLimitScopeAuth, RateLimitKeyEmail, "alice@example.com", farPast); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := repo.Record(ctx, RateLimitScopeAuth, RateLimitKeyEmail, "alice@example.com", farPast); err != nil {
		t.Fatalf("record: %v", err)
	}
	// A different key_type for the same scope must not be counted alongside
	// the email bucket.
	if err := repo.Record(ctx, RateLimitScopeAuth, RateLimitKeyIP, "1.2.3.4", farPast); err != nil {
		t.Fatalf("record: %v", err)
	}
	// A different scope for the same key_type/key_value must not be counted
	// alongside the auth bucket.
	if err := repo.Record(ctx, RateLimitScopeRegister, RateLimitKeyEmail, "alice@example.com", farPast); err != nil {
		t.Fatalf("record: %v", err)
	}

	count, err := repo.CountSince(ctx, RateLimitScopeAuth, RateLimitKeyEmail, "alice@example.com", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("count since: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2, got %d", count)
	}
}

func TestRateLimitAttemptRepository_CountSince_ExcludesAttemptsOutsideWindow(t *testing.T) {
	repo := newTestRateLimitAttemptRepository(t)
	ctx := context.Background()

	farPast := time.Now().Add(-24 * time.Hour)
	if err := repo.Record(ctx, RateLimitScopeAuth, RateLimitKeyIP, "1.2.3.4", farPast); err != nil {
		t.Fatalf("record: %v", err)
	}

	// Recording with olderThan in the far past means the just-recorded row
	// itself (created "now") is never pruned by this call — only checking
	// with a recent `since` should exclude it.
	count, err := repo.CountSince(ctx, RateLimitScopeAuth, RateLimitKeyIP, "1.2.3.4", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("count since: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 attempts within a window that starts in the future, got %d", count)
	}
}

func TestRateLimitAttemptRepository_Record_PrunesOlderRowsForSameKey(t *testing.T) {
	repo := newTestRateLimitAttemptRepository(t)
	ctx := context.Background()

	// Seed a row whose created_at is genuinely in the past — Record itself
	// always inserts with CURRENT_TIMESTAMP, so a row old enough to prune
	// has to be written directly rather than through Record.
	if _, err := repo.db.ExecContext(ctx,
		`INSERT INTO auth_rate_limit_attempts (scope, key_type, key_value, created_at) VALUES (?, ?, ?, ?)`,
		RateLimitScopeAuth, RateLimitKeyEmail, "bob@example.com", time.Now().Add(-24*time.Hour).UTC(),
	); err != nil {
		t.Fatalf("seed old row: %v", err)
	}

	// This Record call prunes anything older than 15 minutes ago before
	// inserting its own row — the seeded row above is older than that, so
	// it should be gone afterward, leaving only the fresh one.
	if err := repo.Record(ctx, RateLimitScopeAuth, RateLimitKeyEmail, "bob@example.com", time.Now().Add(-15*time.Minute)); err != nil {
		t.Fatalf("record: %v", err)
	}

	count, err := repo.CountSince(ctx, RateLimitScopeAuth, RateLimitKeyEmail, "bob@example.com", time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("count since: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected the pruned bucket to hold exactly 1 row, got %d", count)
	}
}

// TestRateLimitAttemptRepository_Record_PrunesOtherKeysInTheSameScope pins
// the fix for a key that's written exactly once — an attacker rotating the
// submitted Email (or IP) on every request. Record's own prune must not be
// scoped to the key it's about to insert, or such a row would never be
// pruned by anything, growing the table without bound.
func TestRateLimitAttemptRepository_Record_PrunesOtherKeysInTheSameScope(t *testing.T) {
	repo := newTestRateLimitAttemptRepository(t)
	ctx := context.Background()

	// A stale row under a key ("one-shot@example.com") that Record is never
	// called for again — nothing same-key would ever prune it.
	if _, err := repo.db.ExecContext(ctx,
		`INSERT INTO auth_rate_limit_attempts (scope, key_type, key_value, created_at) VALUES (?, ?, ?, ?)`,
		RateLimitScopeAuth, RateLimitKeyEmail, "one-shot@example.com", time.Now().Add(-24*time.Hour).UTC(),
	); err != nil {
		t.Fatalf("seed old row: %v", err)
	}

	// A Record call for a *different* key in the same scope.
	if err := repo.Record(ctx, RateLimitScopeAuth, RateLimitKeyEmail, "someone-else@example.com", time.Now().Add(-15*time.Minute)); err != nil {
		t.Fatalf("record: %v", err)
	}

	count, err := repo.CountSince(ctx, RateLimitScopeAuth, RateLimitKeyEmail, "one-shot@example.com", time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("count since: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected the one-shot key's stale row to be pruned by another key's Record call in the same scope, got %d rows left", count)
	}
}

// TestRateLimitAttemptRepository_Record_DoesNotPruneOtherScopes pins the
// other half of the same fix: the scope-wide prune must stay scoped to the
// caller's own scope, or Login/CalDAV's 15-minute cutoff would delete
// Register's still-live rows under its own, longer 1-hour window.
func TestRateLimitAttemptRepository_Record_DoesNotPruneOtherScopes(t *testing.T) {
	repo := newTestRateLimitAttemptRepository(t)
	ctx := context.Background()

	// A row in the register scope that's 20 minutes old — stale for the
	// auth scope's 15-minute window, but still live for register's 1-hour
	// one.
	if _, err := repo.db.ExecContext(ctx,
		`INSERT INTO auth_rate_limit_attempts (scope, key_type, key_value, created_at) VALUES (?, ?, ?, ?)`,
		RateLimitScopeRegister, RateLimitKeyIP, "1.2.3.4", time.Now().Add(-20*time.Minute).UTC(),
	); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	// An auth-scope Record call, pruning with the auth scope's own
	// (shorter) cutoff — this must never touch the register-scope row.
	if err := repo.Record(ctx, RateLimitScopeAuth, RateLimitKeyEmail, "someone@example.com", time.Now().Add(-15*time.Minute)); err != nil {
		t.Fatalf("record: %v", err)
	}

	count, err := repo.CountSince(ctx, RateLimitScopeRegister, RateLimitKeyIP, "1.2.3.4", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("count since: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected the register-scope row to survive an auth-scope prune, got %d rows left", count)
	}
}

func TestRateLimitAttemptRepository_Clear_DeletesOnlyMatchingKey(t *testing.T) {
	repo := newTestRateLimitAttemptRepository(t)
	ctx := context.Background()

	farPast := time.Now().Add(-24 * time.Hour)
	if err := repo.Record(ctx, RateLimitScopeAuth, RateLimitKeyEmail, "carol@example.com", farPast); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := repo.Record(ctx, RateLimitScopeAuth, RateLimitKeyIP, "5.6.7.8", farPast); err != nil {
		t.Fatalf("record: %v", err)
	}

	if err := repo.Clear(ctx, RateLimitScopeAuth, RateLimitKeyEmail, "carol@example.com"); err != nil {
		t.Fatalf("clear: %v", err)
	}

	emailCount, err := repo.CountSince(ctx, RateLimitScopeAuth, RateLimitKeyEmail, "carol@example.com", farPast)
	if err != nil {
		t.Fatalf("count since (email): %v", err)
	}
	if emailCount != 0 {
		t.Fatalf("expected the cleared email bucket to be empty, got %d", emailCount)
	}

	ipCount, err := repo.CountSince(ctx, RateLimitScopeAuth, RateLimitKeyIP, "5.6.7.8", farPast)
	if err != nil {
		t.Fatalf("count since (ip): %v", err)
	}
	if ipCount != 1 {
		t.Fatalf("expected clearing the email bucket to leave the ip bucket untouched, got %d", ipCount)
	}
}

func TestRateLimitAttemptRepository_CountSince_EmailKeyIsCaseInsensitive(t *testing.T) {
	repo := newTestRateLimitAttemptRepository(t)
	ctx := context.Background()

	farPast := time.Now().Add(-24 * time.Hour)
	if err := repo.Record(ctx, RateLimitScopeAuth, RateLimitKeyEmail, "Dave@Example.com", farPast); err != nil {
		t.Fatalf("record: %v", err)
	}

	count, err := repo.CountSince(ctx, RateLimitScopeAuth, RateLimitKeyEmail, "dave@example.com", farPast)
	if err != nil {
		t.Fatalf("count since: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected key_value's COLLATE NOCASE to match regardless of case, got %d", count)
	}
}
