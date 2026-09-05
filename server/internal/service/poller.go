// poller.go implements the background ticker that refreshes Sources on its
// own, with no client involvement (#86, #288, ADR-0033) — the same shape as
// reminder.Scheduler: an injected clock, a Tick doing one pass, and a loop
// over it. Freshness cannot depend on someone watching: Reminders fire from
// a server-side scheduler and CalDAV clients never touch the web API, so a
// Subscription or Linked Calendar that nobody's browser is open for still
// needs to refresh itself.
package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// DueSourceLister is Poller's read path: every Source, across every user,
// whose next_refresh_at has come due — Subscription and Connection alike,
// each row carrying the Kind that routes it and (for a Connection) whether
// it already holds a Delta Refresh cursor. Satisfied by *CalendarService.
type DueSourceLister interface {
	ListDueForRefresh(ctx context.Context, now time.Time) ([]repository.DueRefresh, error)
}

// SubscriptionRefresher is Poller's write path for a Subscription: one
// Refresh attempt against a single Subscribed Calendar. Satisfied by
// *SubscribeService. force is always false from the poller — only the manual
// "Refresh now" action bypasses backoff (ADR-0033).
type SubscriptionRefresher interface {
	Refresh(ctx context.Context, userID int64, calendarID string, force bool) (RefreshResult, error)
}

// LinkedCalendarRefresher is Poller's write path for a Linked Calendar: one
// Refresh attempt in an explicitly chosen mode (#288, ADR-0053). Satisfied
// by *ConnectionService.
type LinkedCalendarRefresher interface {
	RefreshLinked(ctx context.Context, userID int64, calendarID string, mode RefreshMode) (RefreshResult, error)
}

// Poller is the background ticker that refreshes due Sources (#86, #288,
// ADR-0033).
//
// Not safe for concurrent use: like reminder.Scheduler, Tick assumes only
// one call is ever in flight at a time.
type Poller struct {
	sources      DueSourceLister
	subscription SubscriptionRefresher
	linked       LinkedCalendarRefresher
	now          func() time.Time
}

func NewPoller(sources DueSourceLister, subscription SubscriptionRefresher, linked LinkedCalendarRefresher, now func() time.Time) *Poller {
	return &Poller{sources: sources, subscription: subscription, linked: linked, now: now}
}

// Tick refreshes every Source whose next_refresh_at has come due as of now.
// Each Refresh call reschedules its own Calendar (success or failure), so
// Tick doesn't need to: a Refresh that errors is logged and Tick moves on to
// the next due Calendar rather than aborting the pass, since one broken
// Source must never block every other one's refresh.
func (p *Poller) Tick(ctx context.Context) error {
	due, err := p.sources.ListDueForRefresh(ctx, p.now())
	if err != nil {
		return fmt.Errorf("list due sources: %w", err)
	}

	for _, d := range due {
		if err := p.refresh(ctx, d); err != nil {
			log.Printf("source poller refresh (calendar=%s, kind=%s): %v", d.CalendarID, d.Kind, err)
		}
	}

	return nil
}

// refresh routes one due Source to the right refresher. For a Connection the
// mode is chosen here from whether a cursor exists (RefreshModeForCursor,
// the same rule the manual-refresh handler uses) and threaded down as an
// explicit argument — never re-derived inside the reconciler.
func (p *Poller) refresh(ctx context.Context, d repository.DueRefresh) error {
	switch d.Kind {
	case repository.SourceKindConnection:
		_, err := p.linked.RefreshLinked(ctx, d.UserID, d.CalendarID, RefreshModeForCursor(d.HasCursor))
		return err
	case repository.SourceKindSubscription:
		_, err := p.subscription.Refresh(ctx, d.UserID, d.CalendarID, false)
		return err
	default:
		return fmt.Errorf("due source %s has unknown kind %q", d.CalendarID, d.Kind)
	}
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
				log.Printf("source poller tick: %v", err)
			}
		}
	}
}
