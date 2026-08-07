// refreshschedule.go implements the pure scheduling rules a Refresh and the
// background poller share (#86, ADR-0033): how long until the next attempt,
// and how a failure is classified for the sidebar. All three functions here
// are deliberately pure — no database, no clock beyond the times passed in —
// so they table-test directly, the same way ReconcileSeries does.
package service

import (
	"errors"
	"hash/fnv"
	"time"
)

const (
	// DefaultRefreshInterval is how often a Subscription is refreshed when
	// its feed states no cadence of its own. Configurable per deployment
	// (config.SubscriptionRefreshInterval); this is the fallback default.
	DefaultRefreshInterval = time.Hour
	// MaxRefreshBackoff is the ceiling exponential backoff climbs to after
	// repeated failures — roughly a day, so a genuinely dead feed is
	// retried a handful of times per day rather than hammered or forgotten.
	MaxRefreshBackoff = 24 * time.Hour
)

// Error classes a broken Subscription's sidebar entry carries (#86,
// ADR-0033): needs-attention problems require a human to fix something
// (bad credentials, a feed that no longer exists, one that isn't valid
// iCalendar); retrying problems are expected to clear on their own.
const (
	ErrorClassNeedsAttention = "needs_attention"
	ErrorClassRetrying       = "retrying"
)

// effectiveRefreshInterval is the interval a Refresh actually schedules
// against: the deployment default, unless the publisher's own stated
// cadence (REFRESH-INTERVAL/X-PUBLISHED-TTL) asks for something longer. A
// poller never polls faster than the publisher asked, but a publisher
// asking for something shorter than the default doesn't speed it up either
// — the default is also a floor.
func effectiveRefreshInterval(defaultInterval time.Duration, publisherIntervalSeconds *int) time.Duration {
	if publisherIntervalSeconds == nil {
		return defaultInterval
	}
	publisher := time.Duration(*publisherIntervalSeconds) * time.Second
	if publisher > defaultInterval {
		return publisher
	}
	return defaultInterval
}

// backoffInterval is how long to wait before the next attempt after
// failureCount consecutive failures, doubling base each time and capping at
// MaxRefreshBackoff. failureCount is the count *including* the failure that
// just happened (i.e. the first failure passes 1, doubling base once).
func backoffInterval(base time.Duration, failureCount int) time.Duration {
	if failureCount < 1 {
		failureCount = 1
	}
	interval := base
	for i := 1; i < failureCount; i++ {
		if interval >= MaxRefreshBackoff {
			return MaxRefreshBackoff
		}
		interval *= 2
	}
	if interval > MaxRefreshBackoff {
		return MaxRefreshBackoff
	}
	return interval
}

// nextRefreshTime is the one place Subscribe/Refresh computes a Calendar's
// next due time, so the "add an offset, then stagger against base" shape
// isn't repeated at each of their three call sites (initial schedule,
// success, failure). offset is what actually decides the wait — base (the
// effective interval, never the inflated backoff figure) is only the scale
// staggerJitter spreads across.
func nextRefreshTime(now time.Time, calendarID string, offset, base time.Duration) time.Time {
	return now.Add(offset).Add(staggerJitter(calendarID, base))
}

// staggerJitter spreads many Subscriptions that share the same interval
// across it, rather than letting them all come due on the same tick: a
// deterministic (not random) fraction of interval, derived from
// calendarID, so scheduling stays reproducible in tests and across
// restarts. Bounded to at most a quarter of interval.
func staggerJitter(calendarID string, interval time.Duration) time.Duration {
	h := fnv.New32a()
	h.Write([]byte(calendarID))
	quarter := interval / 4
	if quarter <= 0 {
		return 0
	}
	return time.Duration(h.Sum32()) % quarter
}

// durationSecondsPtr converts a feed-stated refresh interval into the
// whole-second form the repository stores it in, nil when the feed stated
// none.
func durationSecondsPtr(d *time.Duration) *int {
	if d == nil {
		return nil
	}
	seconds := int(d.Seconds())
	return &seconds
}

// classifyRefreshError sorts a failed Refresh's error into the sidebar's
// two classes (#86, ADR-0033): needs-attention (auth failure, not found, an
// unparseable or oversized feed, or a URL blocked as non-public (#97) —
// something a human must fix) or retrying (everything else
// fetchICSConditional reports: timeout, DNS, 5xx, other non-2xx). message is
// err's own text, which already carries a masked URL via MaskURL everywhere
// these sentinels are wrapped.
func classifyRefreshError(err error) (class, message string) {
	switch {
	case errors.Is(err, ErrSubscribeAuthFailed),
		errors.Is(err, ErrSubscribeNotFound),
		errors.Is(err, ErrSubscribeUnparseable),
		errors.Is(err, ErrSubscribeTooLarge),
		errors.Is(err, ErrSubscribeInvalidURL),
		errors.Is(err, ErrSubscribeURLBlocked):
		return ErrorClassNeedsAttention, err.Error()
	default:
		return ErrorClassRetrying, err.Error()
	}
}
