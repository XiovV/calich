// Package outbox drains queued Invitation emails (ADR-0059, ADR-0060): a
// background ticker, the same shape as the reminder Scheduler, that sends,
// retries with backoff, and records the outcome.
package outbox

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/XiovV/calendar/server/internal/repository"
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

// backoffSchedule is how long Tick waits before retrying a failed send,
// indexed by attempt number (the 1st failure retries after
// backoffSchedule[0], etc.) — widening geometrically, generous enough that
// a transient SMTP blip clears well inside it.
var backoffSchedule = []time.Duration{
	time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	30 * time.Minute,
}

// maxAttempts is one more than len(backoffSchedule): a message that fails
// after exhausting every backoff entry is marked failed rather than
// scheduled for yet another retry — backoff cannot retry forever (ADR-0060).
var maxAttempts = len(backoffSchedule) + 1

// recipientKey identifies msg's recipient for Tick's per-recipient blocking
// (#200, ADR-0058): a User-backed message keys on its RecipientUserID, an
// email-shaped one — no RecipientUserID to key on — folds RecipientEmail to
// lowercase first, mirroring the case-insensitive matching every other
// email comparison in this app already does.
func recipientKey(msg repository.OutboxMessage) string {
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
// ordering against each other — a REQUEST, a re-issued REQUEST, a CANCEL,
// once #201 lands — are exactly the ones this stalls behind one another.
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
			if attempts >= maxAttempts {
				if merr := w.store.MarkFailed(ctx, msg.ID, attempts, err.Error()); merr != nil {
					log.Printf("outbox: mark failed (id=%d): %v", msg.ID, merr)
				}
				continue
			}
			next := now.Add(backoffSchedule[attempts-1])
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
