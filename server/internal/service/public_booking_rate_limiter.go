package service

import (
	"context"
	"fmt"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// publicBookingRateLimitWindow is PublicBookingRateLimiter's rolling window
// (#324, ADR-0087), fixed like AuthRateLimiter's own windows — only the
// per-IP ceiling is env-configurable (config.Config.PublicBookingRateLimitPerIP).
const publicBookingRateLimitWindow = time.Minute

// ErrPublicBookingRateLimitExceeded mirrors repository.ErrRateLimited so
// handlers only need to import this package's sentinels.
var ErrPublicBookingRateLimitExceeded = repository.ErrRateLimited

// PublicBookingRateLimiter throttles the public Booking Link page (#324,
// ADR-0087) — the first unauthenticated surface that reads calendar data.
// IP-only, unlike AuthRateLimiter's email+IP pair: there is no credential or
// account being guessed here, only a stranger's own browsing, so the only
// key available is the caller's address. Deliberately enforced from the HTTP
// layer (PublicBookingHandler), mirroring AuthRateLimiter's own posture.
type PublicBookingRateLimiter struct {
	repo     *repository.RateLimitAttemptRepository
	maxPerIP int
}

func NewPublicBookingRateLimiter(repo *repository.RateLimitAttemptRepository, maxPerIP int) *PublicBookingRateLimiter {
	return &PublicBookingRateLimiter{repo: repo, maxPerIP: maxPerIP}
}

// Check refuses with ErrPublicBookingRateLimitExceeded once ip's own
// rolling-window count has reached its ceiling.
func (l *PublicBookingRateLimiter) Check(ctx context.Context, ip string) error {
	since := time.Now().Add(-publicBookingRateLimitWindow)

	count, err := l.repo.CountSince(ctx, repository.RateLimitScopePublicBooking, repository.RateLimitKeyIP, ip, since)
	if err != nil {
		return fmt.Errorf("count public booking attempts: %w", err)
	}
	if count >= l.maxPerIP {
		return ErrPublicBookingRateLimitExceeded
	}
	return nil
}

// Record charges one call to ip's bucket — every call, not just a failed
// one, since what's throttled is how often the page is loaded at all
// (mirroring Register's own AuthRateLimiter.RecordRegisterAttempt).
func (l *PublicBookingRateLimiter) Record(ctx context.Context, ip string) error {
	olderThan := time.Now().Add(-publicBookingRateLimitWindow)
	if err := l.repo.Record(ctx, repository.RateLimitScopePublicBooking, repository.RateLimitKeyIP, ip, olderThan); err != nil {
		return fmt.Errorf("record public booking attempt: %w", err)
	}
	return nil
}
