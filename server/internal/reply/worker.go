package reply

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calendar/server/internal/icalendar"
)

// RawMessage is one fetched, not-yet-classified message: its IMAP UID (for
// MarkSeen) and raw RFC 822 bytes.
type RawMessage struct {
	UID   uint32
	Bytes []byte
}

// MailReader is the Worker's mail-transport seam. Satisfied by *IMAPClient.
type MailReader interface {
	// FetchUnseen returns every message not yet flagged \Seen.
	FetchUnseen(ctx context.Context) ([]RawMessage, error)
	// MarkSeen flags uids \Seen.
	MarkSeen(ctx context.Context, uids []uint32) error
}

// ResponseApplier resolves a decoded METHOD:REPLY and writes the Response
// it names. Satisfied by *service.EventService (ApplyReply).
type ResponseApplier interface {
	ApplyReply(ctx context.Context, reply icalendar.ParsedReply) (applied bool, err error)
}

// Worker is the background poller that drains inbound Attendee Responses
// (ADR-0059) — the same Tick/Run shape as the reminder Scheduler and the
// outbox Worker.
type Worker struct {
	mail      MailReader
	responses ResponseApplier
}

func NewWorker(mail MailReader, responses ResponseApplier) *Worker {
	return &Worker{mail: mail, responses: responses}
}

// Tick fetches every unseen message and processes each in turn, then flags
// \Seen every one recognized as a calendar REPLY (matched or not) in one
// batched call — ordinary mail Tick finds no calendar part in is left with
// its flags completely untouched (#202, "mail that is not a calendar reply
// is left alone"). A single message's own decode/apply failure is logged
// and does not stop the rest of the batch, the same "one broken item never
// blocks the others" posture the outbox Worker and the Refresh poller both
// already take.
func (w *Worker) Tick(ctx context.Context) error {
	messages, err := w.mail.FetchUnseen(ctx)
	if err != nil {
		return fmt.Errorf("fetch unseen mail: %w", err)
	}

	var seen []uint32
	for _, msg := range messages {
		if w.processMessage(ctx, msg) {
			seen = append(seen, msg.UID)
		}
	}
	if len(seen) == 0 {
		return nil
	}
	if err := w.mail.MarkSeen(ctx, seen); err != nil {
		return fmt.Errorf("mark seen: %w", err)
	}
	return nil
}

// processMessage reports whether msg was recognized as a calendar REPLY —
// true whether or not a matching Attendee was found for it, which is what
// makes Tick mark it \Seen either way: an unknown UID or a non-Attendee
// address has nowhere to go and is logged and dropped, not reprocessed
// forever (ADR-0059). False for ordinary mail (no text/calendar part, or a
// method other than REPLY) and for a ResponseApplier error — the latter
// reads as an infrastructure hiccup rather than an unmatched reply, so the
// message is left unseen to retry on the next Tick instead of being
// silently dropped.
func (w *Worker) processMessage(ctx context.Context, msg RawMessage) bool {
	calBytes, ok, err := ExtractCalendarPart(msg.Bytes)
	if err != nil {
		log.Printf("reply: extract calendar part (uid=%d): %v", msg.UID, err)
		return false
	}
	if !ok {
		return false
	}

	cal, err := ical.NewDecoder(bytes.NewReader(calBytes)).Decode()
	if err != nil {
		log.Printf("reply: decode calendar (uid=%d): %v", msg.UID, err)
		return false
	}

	parsed, ok, err := icalendar.ParseReply(cal)
	if err != nil {
		log.Printf("reply: parse reply (uid=%d): %v", msg.UID, err)
		return false
	}
	if !ok {
		return false
	}

	applied, err := w.responses.ApplyReply(ctx, *parsed)
	if err != nil {
		log.Printf("reply: apply (uid=%d, event=%s): %v", msg.UID, parsed.UID, err)
		return false
	}
	if !applied {
		log.Printf("reply: no matching attendee (uid=%d, event=%s, address=%s)", msg.UID, parsed.UID, parsed.Attendee)
	}
	return true
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
				log.Printf("reply worker tick: %v", err)
			}
		}
	}
}
