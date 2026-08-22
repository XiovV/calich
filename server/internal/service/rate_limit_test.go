package service

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calich/server/internal/db"
	"github.com/XiovV/calich/server/internal/repository"
)

func newTestAuthRateLimiter(t *testing.T, maxAuthPerEmail, maxAuthPerIP, maxRegisterPerIP int) *AuthRateLimiter {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	repo := repository.NewRateLimitAttemptRepository(sqlDB)
	return NewAuthRateLimiter(repo, maxAuthPerEmail, maxAuthPerIP, maxRegisterPerIP)
}

func TestAuthRateLimiter_CheckAuth_AllowsAttemptsBelowCeiling(t *testing.T) {
	limiter := newTestAuthRateLimiter(t, 3, 3, 3)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := limiter.CheckAuth(ctx, "alice@example.com", "1.2.3.4"); err != nil {
			t.Fatalf("check auth (attempt %d): %v", i, err)
		}
		if err := limiter.RecordAuthFailure(ctx, "alice@example.com", "1.2.3.4"); err != nil {
			t.Fatalf("record auth failure (attempt %d): %v", i, err)
		}
	}

	// A third attempt is still below the ceiling of 3.
	if err := limiter.CheckAuth(ctx, "alice@example.com", "1.2.3.4"); err != nil {
		t.Fatalf("expected the 3rd attempt to be allowed, got %v", err)
	}
}

func TestAuthRateLimiter_CheckAuth_RefusesOnceEmailCeilingReached(t *testing.T) {
	limiter := newTestAuthRateLimiter(t, 3, 100, 100)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := limiter.CheckAuth(ctx, "alice@example.com", "1.2.3.4"); err != nil {
			t.Fatalf("check auth (attempt %d): %v", i, err)
		}
		if err := limiter.RecordAuthFailure(ctx, "alice@example.com", "1.2.3.4"); err != nil {
			t.Fatalf("record auth failure (attempt %d): %v", i, err)
		}
	}

	err := limiter.CheckAuth(ctx, "alice@example.com", "1.2.3.4")
	if !errors.Is(err, ErrAuthRateLimitExceeded) {
		t.Fatalf("expected ErrAuthRateLimitExceeded once the email ceiling is reached, got %v", err)
	}
}

func TestAuthRateLimiter_CheckAuth_RefusesOnceIPCeilingReachedAcrossDifferentEmails(t *testing.T) {
	limiter := newTestAuthRateLimiter(t, 100, 3, 100)
	ctx := context.Background()

	emails := []string{"a@example.com", "b@example.com", "c@example.com"}
	for _, email := range emails {
		if err := limiter.CheckAuth(ctx, email, "9.9.9.9"); err != nil {
			t.Fatalf("check auth for %s: %v", email, err)
		}
		if err := limiter.RecordAuthFailure(ctx, email, "9.9.9.9"); err != nil {
			t.Fatalf("record auth failure for %s: %v", email, err)
		}
	}

	// A 4th distinct email from the same IP trips the IP ceiling even
	// though none of these emails individually reached the email ceiling —
	// this is what rotating the target defeats a per-Email-only limiter.
	err := limiter.CheckAuth(ctx, "d@example.com", "9.9.9.9")
	if !errors.Is(err, ErrAuthRateLimitExceeded) {
		t.Fatalf("expected ErrAuthRateLimitExceeded once the ip ceiling is reached, got %v", err)
	}
}

func TestAuthRateLimiter_CheckAuth_DoesNotDistinguishRealFromUnknownEmail(t *testing.T) {
	// A 429 must look identical whether or not the submitted Email names a
	// real account (ADR-0047's enumeration posture) — CheckAuth/
	// RecordAuthFailure never look at the users table at all, so there is
	// nothing here that could tell the two apart in the first place. This
	// test pins that by exercising the ceiling against an email nobody would
	// ever register.
	limiter := newTestAuthRateLimiter(t, 1, 100, 100)
	ctx := context.Background()

	if err := limiter.CheckAuth(ctx, "nonexistent@example.com", "1.1.1.1"); err != nil {
		t.Fatalf("check auth: %v", err)
	}
	if err := limiter.RecordAuthFailure(ctx, "nonexistent@example.com", "1.1.1.1"); err != nil {
		t.Fatalf("record auth failure: %v", err)
	}

	err := limiter.CheckAuth(ctx, "nonexistent@example.com", "1.1.1.1")
	if !errors.Is(err, ErrAuthRateLimitExceeded) {
		t.Fatalf("expected ErrAuthRateLimitExceeded for an unknown email too, got %v", err)
	}
}

func TestAuthRateLimiter_ClearAuthFailures_ResetsEmailBucketOnly(t *testing.T) {
	limiter := newTestAuthRateLimiter(t, 1, 100, 100)
	ctx := context.Background()

	if err := limiter.RecordAuthFailure(ctx, "Alice@Example.com", "1.2.3.4"); err != nil {
		t.Fatalf("record auth failure: %v", err)
	}

	// The email bucket alone is now at its ceiling of 1.
	if err := limiter.CheckAuth(ctx, "alice@example.com", "5.6.7.8"); !errors.Is(err, ErrAuthRateLimitExceeded) {
		t.Fatalf("expected the email ceiling to already be tripped, got %v", err)
	}

	// Clearing normalizes case the same way CheckAuth/RecordAuthFailure do.
	if err := limiter.ClearAuthFailures(ctx, "alice@example.com"); err != nil {
		t.Fatalf("clear auth failures: %v", err)
	}

	if err := limiter.CheckAuth(ctx, "alice@example.com", "5.6.7.8"); err != nil {
		t.Fatalf("expected a successful login to reset the email bucket, got %v", err)
	}
}

func TestAuthRateLimiter_ClearAuthFailures_LeavesIPBucketIntact(t *testing.T) {
	limiter := newTestAuthRateLimiter(t, 100, 1, 100)
	ctx := context.Background()

	// One failed attempt against a different email from the same IP trips
	// the (very low) IP ceiling.
	if err := limiter.RecordAuthFailure(ctx, "attacker-target@example.com", "1.2.3.4"); err != nil {
		t.Fatalf("record auth failure: %v", err)
	}

	// alice successfully authenticates from the same IP.
	if err := limiter.ClearAuthFailures(ctx, "alice@example.com"); err != nil {
		t.Fatalf("clear auth failures: %v", err)
	}

	// The IP bucket must still be tripped for a fresh target from that same
	// address — clearing alice's own success must not have reset it.
	err := limiter.CheckAuth(ctx, "another-target@example.com", "1.2.3.4")
	if !errors.Is(err, ErrAuthRateLimitExceeded) {
		t.Fatalf("expected the ip bucket to remain tripped after an unrelated email's success, got %v", err)
	}
}

func TestAuthRateLimiter_CheckRegister_AllowsBelowCeilingRefusesAtCeiling(t *testing.T) {
	limiter := newTestAuthRateLimiter(t, 100, 100, 2)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := limiter.CheckRegister(ctx, "1.2.3.4"); err != nil {
			t.Fatalf("check register (attempt %d): %v", i, err)
		}
		if err := limiter.RecordRegisterAttempt(ctx, "1.2.3.4"); err != nil {
			t.Fatalf("record register attempt (attempt %d): %v", i, err)
		}
	}

	err := limiter.CheckRegister(ctx, "1.2.3.4")
	if !errors.Is(err, ErrAuthRateLimitExceeded) {
		t.Fatalf("expected ErrAuthRateLimitExceeded once the register ceiling is reached, got %v", err)
	}
}

func TestAuthRateLimiter_CheckRegister_IsIndependentPerIP(t *testing.T) {
	limiter := newTestAuthRateLimiter(t, 100, 100, 1)
	ctx := context.Background()

	if err := limiter.RecordRegisterAttempt(ctx, "1.2.3.4"); err != nil {
		t.Fatalf("record register attempt: %v", err)
	}

	// A different IP's own bucket is untouched by the first IP's ceiling.
	if err := limiter.CheckRegister(ctx, "5.6.7.8"); err != nil {
		t.Fatalf("expected a different ip's register ceiling to be unaffected, got %v", err)
	}
}
