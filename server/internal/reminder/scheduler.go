package reminder

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/XiovV/calendar/server/internal/repository"
)

// EventLister is the scheduler's read path: every Event across every user
// that carries at least one Reminder (ADR-0021). Satisfied by
// *service.EventService.
type EventLister interface {
	ListAllWithReminders(ctx context.Context) ([]repository.EventWithOwner, error)
}

// Ledger is the scheduler's exactly-once seam. Satisfied by
// *repository.FiredReminderRepository.
type Ledger interface {
	MarkFired(ctx context.Context, reminderID, userID int64, occurrenceStart, firedAt time.Time) (bool, error)
}

// Scheduler is the background ticker that fires due Reminders (ADR-0021). A
// tick's window is (lastTick, now] — lastTick lives only in the struct, in
// memory, so a process restart naturally starts a fresh Scheduler with
// lastTick at the new process's own start time. Nothing before that is ever
// considered due: "no catch-up" for a downtime gap is a consequence of this,
// not special-cased.
//
// Not safe for concurrent use: Tick reads and later writes lastTick with no
// locking, on the assumption that only one call is ever in flight at a time
// (Run's own single goroutine, or one call at a time in a test). Calling
// Tick concurrently with itself would race on lastTick.
type Scheduler struct {
	events     EventLister
	ledger     Ledger
	dispatcher Dispatcher
	now        func() time.Time
	lastTick   time.Time
}

// NewScheduler constructs a Scheduler whose window starts at construction
// time — the first call to Tick only considers Reminders due after this
// moment.
func NewScheduler(events EventLister, ledger Ledger, dispatcher Dispatcher, now func() time.Time) *Scheduler {
	return &Scheduler{
		events:     events,
		ledger:     ledger,
		dispatcher: dispatcher,
		now:        now,
		lastTick:   now(),
	}
}

// Tick resolves and dispatches every Reminder that came due in the half-open
// window (lastTick, now], then advances lastTick to now. lastTick only
// advances once the window has been fully resolved: a transient read/compute
// failure leaves it in place so the *next* tick retries the same window,
// rather than silently dropping it — "no catch-up" (ADR-0021) is about a
// downed process's lastTick resetting to its own restart time, not about
// tolerating data loss from an in-process error.
func (s *Scheduler) Tick(ctx context.Context) error {
	from, to := s.lastTick, s.now()

	events, err := s.events.ListAllWithReminders(ctx)
	if err != nil {
		return fmt.Errorf("list events with reminders: %w", err)
	}

	due, err := DueAll(events, from, to)
	if err != nil {
		return fmt.Errorf("compute due reminders: %w", err)
	}

	for _, d := range due {
		isNew, err := s.ledger.MarkFired(ctx, d.ReminderID, d.UserID, d.OccurrenceStart, to)
		if err != nil {
			// lastTick deliberately hasn't advanced yet: the next tick
			// retries this whole window. MarkFired is idempotent for
			// whatever this loop already got through, so a retry only
			// redoes wasted work, not double-dispatch.
			return fmt.Errorf("mark reminder fired: %w", err)
		}
		if !isNew {
			// Already fired on an earlier tick (or a prior process's tick,
			// via the persisted ledger) — the exactly-once guarantee.
			continue
		}
		// The ledger entry is already committed before dispatch, so a
		// dispatch failure is logged, not retried — this ticket's
		// Dispatcher is a no-op/log sink; real delivery's own retry
		// behavior (if any) is that seam's concern (#56, #57).
		if err := s.dispatcher.Dispatch(ctx, d); err != nil {
			log.Printf("dispatch reminder (event=%s reminder=%d): %v", d.EventID, d.ReminderID, err)
		}
	}
	s.lastTick = to
	return nil
}

// Run ticks every interval until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil {
				log.Printf("reminder scheduler tick: %v", err)
			}
		}
	}
}
