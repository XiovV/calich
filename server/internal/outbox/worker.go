// Package outbox drains the outbox: a background ticker, the same shape as
// the reminder Scheduler, that sends, retries with backoff, and records the
// outcome for every queued message regardless of Kind — queued Invitation
// emails (ADR-0059, ADR-0060) and, since #290 (ADR-0075), queued Write-back
// pushes to a Provider. The Worker's own shape doesn't know Kind exists at
// all: it lists, blocks per key, sends through whatever Sender it was given,
// and backs off by whatever schedule that message's Kind names — dispatching
// by Kind, and to which sender, is service.OutboxDispatcher's job, one layer
// up.
package outbox

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// Store is the Worker's persistence seam. Satisfied by
// *repository.OutboxRepository.
type Store interface {
	ListPending(ctx context.Context, limit int) ([]repository.OutboxMessage, error)
	MarkSent(ctx context.Context, id int64, sentAt time.Time) error
	MarkRetry(ctx context.Context, id int64, attempts int, nextAttemptAt time.Time, lastErr string) error
	MarkFailed(ctx context.Context, id int64, attempts int, lastErr string) error
}

// Sender delivers one OutboxMessage — building and sending its Invitation.
// Satisfied by *service.InvitationSender.
type Sender interface {
	Send(ctx context.Context, msg repository.OutboxMessage) error
}

// batchSize is how many Pending messages one Tick considers — comfortably
// above what a self-hosted instance queues between ticks, so a healthy
// queue fully drains within a single Tick.
const batchSize = 200

// mailBackoffSchedule is how long Tick waits before retrying a failed
// OutboxKindMail send, indexed by attempt number (the 1st failure retries
// after mailBackoffSchedule[0], etc.) — widening geometrically, generous
// enough that a transient SMTP blip clears well inside it.
var mailBackoffSchedule = []time.Duration{
	time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	30 * time.Minute,
}

// writeBackBackoffSchedule is mailBackoffSchedule's OutboxKindWriteBack
// counterpart (#290, ADR-0075): "a pending push needs no UI; it lands in
// seconds" is only true if a transient failure retries quickly rather than
// waiting a full minute before the first attempt — tighter throughout, but
// still bounded, since backoff cannot retry forever any more than mail's
// does (ADR-0060).
var writeBackBackoffSchedule = []time.Duration{
	10 * time.Second,
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
}

// backoffScheduleFor is the per-Kind backoff Tick consults (#290, ADR-0075's
// "the sender dispatches by kind, with per-kind backoff") — an unrecognized
// Kind falls back to mail's schedule, mirroring OutboxDispatcher's own
// default.
func backoffScheduleFor(kind string) []time.Duration {
	if kind == repository.OutboxKindWriteBack {
		return writeBackBackoffSchedule
	}
	return mailBackoffSchedule
}

// maxAttemptsFor is one more than len(backoffScheduleFor(kind)): a message
// that fails after exhausting every backoff entry is marked failed rather
// than scheduled for yet another retry — backoff cannot retry forever
// (ADR-0060).
func maxAttemptsFor(kind string) int {
	return len(backoffScheduleFor(kind)) + 1
}

// recipientKey identifies what Tick's per-key blocking should serialize msg
// behind (#200, ADR-0058; #290). A mail message keys on its recipient: a
// User-backed one on its RecipientUserID, an email-shaped one — no
// RecipientUserID to key on — on RecipientEmail folded to lowercase, mirroring
// the case-insensitive matching every other email comparison in this app
// already does. A write-back message carries no recipient at all — it keys
// on its own EventID instead, so two pushes queued for the same Event never
// race each other, while unrelated Events' pushes, and every mail message,
// proceed independently.
func recipientKey(msg repository.OutboxMessage) string {
	if msg.Kind == repository.OutboxKindWriteBack {
		return "w:" + msg.EventID
	}
	if msg.RecipientUserID != nil {
		return fmt.Sprintf("u:%d", *msg.RecipientUserID)
	}
	return "e:" + strings.ToLower(*msg.RecipientEmail)
}

// Worker is the background ticker that drains the outbox (ADR-0060).
type Worker struct {
	store  Store
	sender Sender
	now    func() time.Time
}

func NewWorker(store Store, sender Sender, now func() time.Time) *Worker {
	return &Worker{store: store, sender: sender, now: now}
}

// Tick attempts every due Pending message, oldest first (Store.ListPending's
// own order). A message whose NextAttemptAt hasn't arrived yet, or whose
// recipient already has an earlier message left unresolved this Tick,
// blocks every later message to that *same* recipient — never a later one
// to someone else. That is what makes delivery per-recipient ordered
// (ADR-0060) with no locking: the only messages that could ever need
// ordering against each other — a REQUEST, a re-issued REQUEST, a CANCEL
// (#201) — are exactly the ones this stalls behind one another.
func (w *Worker) Tick(ctx context.Context) error {
	messages, err := w.store.ListPending(ctx, batchSize)
	if err != nil {
		return fmt.Errorf("list pending outbox messages: %w", err)
	}

	now := w.now()
	blocked := make(map[string]bool)
	for _, msg := range messages {
		key := recipientKey(msg)
		if blocked[key] {
			continue
		}
		if msg.NextAttemptAt.After(now) {
			blocked[key] = true
			continue
		}

		if err := w.sender.Send(ctx, msg); err != nil {
			attempts := msg.Attempts + 1
			if attempts >= maxAttemptsFor(msg.Kind) {
				if merr := w.store.MarkFailed(ctx, msg.ID, attempts, err.Error()); merr != nil {
					log.Printf("outbox: mark failed (id=%d): %v", msg.ID, merr)
				}
				continue
			}
			next := now.Add(backoffScheduleFor(msg.Kind)[attempts-1])
			if merr := w.store.MarkRetry(ctx, msg.ID, attempts, next, err.Error()); merr != nil {
				log.Printf("outbox: mark retry (id=%d): %v", msg.ID, merr)
			}
			blocked[key] = true
			continue
		}

		if merr := w.store.MarkSent(ctx, msg.ID, now); merr != nil {
			log.Printf("outbox: mark sent (id=%d): %v", msg.ID, merr)
		}
	}
	return nil
}

// Run ticks every interval until ctx is cancelled.
func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.Tick(ctx); err != nil {
				log.Printf("outbox worker tick: %v", err)
			}
		}
	}
}
