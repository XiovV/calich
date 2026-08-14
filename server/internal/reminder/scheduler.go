package reminder

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/XiovV/calendar/server/internal/repository"
)

// EventLister is the scheduler's read path: every Event across every user that
// could fire a Reminder in the tick's own window, and the resolution naming who
// fires each of them (ADR-0021, ADR-0064). Satisfied by *service.EventService,
// whose resolution is the same one every other read path goes through (#216)
// and which widens the window by the offsets in play before reading, since only
// it can see them (#219).
type EventLister interface {
	ListAllWithReminders(ctx context.Context, from, to time.Time) ([]repository.Event, repository.ResolvedReminders, error)
}

// RecipientLookup resolves the Users a tick's due Reminders belong to, in one
// read for the whole tick rather than one per Reminder (#219). A recipient is
// what ADR-0037's Disabled suppression is read off, what the Notification
// channel checks ADR-0027's synced-device opt-in on, and what the Email channel
// addresses and renders its times for. Satisfied by *repository.UserRepository.
type RecipientLookup interface {
	GetByIDs(ctx context.Context, ids []int64) (map[int64]repository.User, error)
}

// Ledger is the scheduler's exactly-once seam. Satisfied by
// *repository.FiredReminderRepository.
type Ledger interface {
	MarkFired(ctx context.Context, reminderID, userID int64, occurrenceStart, firedAt time.Time) (bool, error)
	// MarkDefaultFired is MarkFired's counterpart for a Reminder that fires
	// by Calendar-default resolution (ADR-0064) — its exactly-once key is
	// (defaultReminderID, eventID, occurrenceStart) rather than
	// (reminderID, occurrenceStart), since one default list fires
	// independently across every Event it resolves onto.
	MarkDefaultFired(ctx context.Context, defaultReminderID int64, eventID string, userID int64, occurrenceStart, firedAt time.Time) (bool, error)
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
	recipients RecipientLookup
	dispatcher Dispatcher
	now        func() time.Time
	lastTick   time.Time
}

// NewScheduler constructs a Scheduler whose window starts at construction
// time — the first call to Tick only considers Reminders due after this
// moment.
func NewScheduler(events EventLister, ledger Ledger, recipients RecipientLookup, dispatcher Dispatcher, now func() time.Time) *Scheduler {
	return &Scheduler{
		events:     events,
		ledger:     ledger,
		recipients: recipients,
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

	events, resolved, err := s.events.ListAllWithReminders(ctx, from, to)
	if err != nil {
		return fmt.Errorf("list events with reminders: %w", err)
	}

	due, err := DueAll(events, resolved, from, to)
	if err != nil {
		return fmt.Errorf("compute due reminders: %w", err)
	}

	recipients, err := s.recipients.GetByIDs(ctx, recipientIDs(due))
	if err != nil {
		return fmt.Errorf("look up reminder recipients: %w", err)
	}

	for _, d := range due {
		var isNew bool
		var err error
		if d.DefaultReminderID != 0 {
			isNew, err = s.ledger.MarkDefaultFired(ctx, d.DefaultReminderID, d.EventID, d.UserID, d.OccurrenceStart, to)
		} else {
			isNew, err = s.ledger.MarkFired(ctx, d.ReminderID, d.UserID, d.OccurrenceStart, to)
		}
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
		recipient, ok := recipients[d.UserID]
		if !ok {
			// The User went away between the resolution read and this
			// lookup: there is nobody left to deliver to.
			log.Printf("dispatch reminder (event=%s user=%d): no such user", d.EventID, d.UserID)
			continue
		}
		// A Disabled User receives no Reminders on any Channel (ADR-0037) —
		// not even the LogDispatcher fallback, since that's still a delivery
		// in spirit for a device that would otherwise show it. Applied here,
		// once, ahead of the whole chain, rather than by each Dispatcher
		// answering it for itself.
		if recipient.IsDisabled {
			continue
		}
		// The ledger entry is already committed before dispatch, so a
		// dispatch failure is logged, not retried — this ticket's
		// Dispatcher is a no-op/log sink; real delivery's own retry
		// behavior (if any) is that seam's concern (#56, #57).
		if err := s.dispatcher.Dispatch(ctx, d, recipient); err != nil {
			log.Printf("dispatch reminder (event=%s reminder=%d): %v", d.EventID, d.ReminderID, err)
		}
	}
	s.lastTick = to
	return nil
}

// recipientIDs is the distinct set of Users a tick's due Reminders belong to,
// so a tick costs one User read per recipient rather than one per Reminder
// (#219) — forty Reminders across four Users is four Users' worth of lookup.
func recipientIDs(due []DueReminder) []int64 {
	seen := make(map[int64]bool, len(due))
	ids := make([]int64, 0, len(due))
	for _, d := range due {
		if seen[d.UserID] {
			continue
		}
		seen[d.UserID] = true
		ids = append(ids, d.UserID)
	}
	return ids
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
