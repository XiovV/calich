// poller.go implements the background ticker that refreshes Subscribed
// Calendars on its own, with no client involvement (#86, ADR-0033) — the
// same shape as reminder.Scheduler: an injected clock, a Tick doing one
// pass, and a loop over it. Freshness cannot depend on someone watching:
// Reminders fire from a server-side scheduler and CalDAV clients never
// touch the web API, so a Subscription that nobody's browser is open for
// still needs to refresh itself.
package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// DueSubscriptionLister is Poller's read path: every Subscription, across
// every user, whose next_refresh_at has come due. Satisfied by
// *CalendarService.
type DueSubscriptionLister interface {
	ListDueForRefresh(ctx context.Context, now time.Time) ([]repository.DueRefresh, error)
}

// SubscriptionRefresher is Poller's write path: one Refresh attempt against
// a single Subscribed Calendar. Satisfied by *SubscribeService. force is
// always false from the poller — only the manual "Refresh now" action
// bypasses backoff (ADR-0033).
type SubscriptionRefresher interface {
	Refresh(ctx context.Context, userID int64, calendarID string, force bool) (RefreshResult, error)
}

// Poller is the background ticker that refreshes due Subscriptions
// (#86, ADR-0033).
//
// Not safe for concurrent use: like reminder.Scheduler, Tick assumes only
// one call is ever in flight at a time.
type Poller struct {
	calendars DueSubscriptionLister
	refresher SubscriptionRefresher
	now       func() time.Time
}

func NewPoller(calendars DueSubscriptionLister, refresher SubscriptionRefresher, now func() time.Time) *Poller {
	return &Poller{calendars: calendars, refresher: refresher, now: now}
}

// Tick refreshes every Subscription whose next_refresh_at has come due as
// of now. Each Refresh call is itself responsible for rescheduling its own
// Calendar (success or failure — see SubscribeService.Refresh), so Tick
// doesn't need to: a Refresh that errors is logged and Tick moves on to the
// next due Calendar rather than aborting the whole pass, since one broken
// feed must never block every other Subscription's refresh.
func (p *Poller) Tick(ctx context.Context) error {
	due, err := p.calendars.ListDueForRefresh(ctx, p.now())
	if err != nil {
		return fmt.Errorf("list due subscriptions: %w", err)
	}

	for _, d := range due {
		if _, err := p.refresher.Refresh(ctx, d.UserID, d.CalendarID, false); err != nil {
			log.Printf("subscription poller refresh (calendar=%s): %v", d.CalendarID, err)
		}
	}

	return nil
}

// Run ticks every interval until ctx is cancelled.
func (p *Poller) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.Tick(ctx); err != nil {
				log.Printf("subscription poller tick: %v", err)
			}
		}
	}
}
