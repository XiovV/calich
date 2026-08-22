package service

import (
	"context"
	"fmt"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// authRateLimitWindow and registerRateLimitWindow are AuthRateLimiter's
// rolling windows (#240, ADR-0070) — fixed, unlike the per-scope ceilings,
// which are the env-configurable half (config.Config.AuthRateLimitPerEmail/
// PerIP/RegisterRateLimitPerIP), mirroring ADR-0058's invite limiter having
// a fixed hour and only a configurable count.
const (
	authRateLimitWindow     = 15 * time.Minute
	registerRateLimitWindow = time.Hour
)

// ErrAuthRateLimitExceeded mirrors repository.ErrRateLimited so callers only
// need to import this package's sentinels, matching ErrEmailTaken's own
// relationship to repository.ErrEmailTaken.
var ErrAuthRateLimitExceeded = repository.ErrRateLimited

// AuthRateLimiter throttles the three unauthenticated surfaces that accept a
// credential guess (#240, ADR-0070). Login and CalDAV Basic auth
// (AppPasswordService.Authenticate) are the same problem behind two
// transports — both compare a submitted password against one account's
// stored hash — and share the "auth" scope below, keyed on both the
// submitted Email and the caller's IP: either alone is a known-insufficient
// defense (Email alone is defeated by rotating source IPs, IP alone by
// rotating target Emails, or lets one attacker exhaust everyone behind a
// shared address). Register has no existing credential to key on, so it's
// IP-only and charges every call, not just failures — see CheckRegister.
//
// Deliberately enforced from the HTTP layer (AuthHandler, httpauth.
// RequireCalDAVAuth) rather than threaded through AuthService.Login/Register
// or AppPasswordService.Authenticate as an extra parameter — see ADR-0070's
// "Enforced at the HTTP layer" section for why.
type AuthRateLimiter struct {
	repo             *repository.RateLimitAttemptRepository
	maxAuthPerEmail  int
	maxAuthPerIP     int
	maxRegisterPerIP int
}

func NewAuthRateLimiter(repo *repository.RateLimitAttemptRepository, maxAuthPerEmail, maxAuthPerIP, maxRegisterPerIP int) *AuthRateLimiter {
	return &AuthRateLimiter{
		repo:             repo,
		maxAuthPerEmail:  maxAuthPerEmail,
		maxAuthPerIP:     maxAuthPerIP,
		maxRegisterPerIP: maxRegisterPerIP,
	}
}

// CheckAuth refuses with ErrAuthRateLimitExceeded once either email's or
// ip's own rolling-window count of failures has reached its ceiling. Run
// before any password comparison, so a flood past the ceiling costs one
// cheap SQL count rather than a bcrypt round (#240) — CheckAuth itself never
// writes. Returns the identical error regardless of which bucket tripped and
// regardless of whether email names a real account, so a 429 here can never
// become a User-enumeration oracle (ADR-0047).
func (l *AuthRateLimiter) CheckAuth(ctx context.Context, email, ip string) error {
	email = normalizeEmail(email)
	since := time.Now().Add(-authRateLimitWindow)

	emailCount, err := l.repo.CountSince(ctx, repository.RateLimitScopeAuth, repository.RateLimitKeyEmail, email, since)
	if err != nil {
		return fmt.Errorf("count auth attempts by email: %w", err)
	}
	if emailCount >= l.maxAuthPerEmail {
		return ErrAuthRateLimitExceeded
	}

	ipCount, err := l.repo.CountSince(ctx, repository.RateLimitScopeAuth, repository.RateLimitKeyIP, ip, since)
	if err != nil {
		return fmt.Errorf("count auth attempts by ip: %w", err)
	}
	if ipCount >= l.maxAuthPerIP {
		return ErrAuthRateLimitExceeded
	}

	return nil
}

// RecordAuthFailure charges one failed attempt against both email's and
// ip's buckets (#240) — every caller that ends in "invalid credentials"
// calls this, including a submitted email with no account at all, so the
// two cases stay indistinguishable to whoever is guessing.
func (l *AuthRateLimiter) RecordAuthFailure(ctx context.Context, email, ip string) error {
	email = normalizeEmail(email)
	olderThan := time.Now().Add(-authRateLimitWindow)

	if err := l.repo.Record(ctx, repository.RateLimitScopeAuth, repository.RateLimitKeyEmail, email, olderThan); err != nil {
		return fmt.Errorf("record auth failure by email: %w", err)
	}
	if err := l.repo.Record(ctx, repository.RateLimitScopeAuth, repository.RateLimitKeyIP, ip, olderThan); err != nil {
		return fmt.Errorf("record auth failure by ip: %w", err)
	}
	return nil
}

// ClearAuthFailures resets email's own accumulated failures once it
// successfully authenticates — a User who eventually gets their password
// right is not left refused by their own earlier mistakes (#240). The IP
// bucket is deliberately left alone: clearing it here would let an attacker
// mix in one correct login (their own account, from an IP otherwise
// guessing at others) to reset the cover a distributed attempt built up —
// see ADR-0070.
func (l *AuthRateLimiter) ClearAuthFailures(ctx context.Context, email string) error {
	email = normalizeEmail(email)
	if err := l.repo.Clear(ctx, repository.RateLimitScopeAuth, repository.RateLimitKeyEmail, email); err != nil {
		return fmt.Errorf("clear auth failures: %w", err)
	}
	return nil
}

// CheckRegister mirrors CheckAuth for Register's own IP-only ceiling (#240)
// — Register has no existing credential to key on, since the submitted
// email may not belong to anyone yet.
func (l *AuthRateLimiter) CheckRegister(ctx context.Context, ip string) error {
	since := time.Now().Add(-registerRateLimitWindow)

	count, err := l.repo.CountSince(ctx, repository.RateLimitScopeRegister, repository.RateLimitKeyIP, ip, since)
	if err != nil {
		return fmt.Errorf("count register attempts: %w", err)
	}
	if count >= l.maxRegisterPerIP {
		return ErrAuthRateLimitExceeded
	}
	return nil
}

// RecordRegisterAttempt charges ip's bucket for one call to Register (#240)
// — every call, not just a failed one, since what's throttled is how often
// the endpoint is invoked at all, not a count of wrong guesses against a
// credential (ADR-0070).
func (l *AuthRateLimiter) RecordRegisterAttempt(ctx context.Context, ip string) error {
	olderThan := time.Now().Add(-registerRateLimitWindow)
	if err := l.repo.Record(ctx, repository.RateLimitScopeRegister, repository.RateLimitKeyIP, ip, olderThan); err != nil {
		return fmt.Errorf("record register attempt: %w", err)
	}
	return nil
}
