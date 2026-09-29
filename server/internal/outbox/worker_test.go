package outbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// userID builds a *int64 test messages can assign to RecipientUserID —
// composite literals can't take the address of an untyped constant.
func userID(id int64) *int64 {
	return &id
}

// email builds a *string test messages can assign to RecipientEmail.
func email(address string) *string {
	return &address
}

// fakeStore is an in-memory stand-in for *repository.OutboxRepository,
// giving a test direct visibility into every state transition a Tick makes.
type fakeStore struct {
	messages []repository.OutboxMessage
}

func (f *fakeStore) find(id int64) *repository.OutboxMessage {
	for i := range f.messages {
		if f.messages[i].ID == id {
			return &f.messages[i]
		}
	}
	return nil
}

func (f *fakeStore) ListPending(_ context.Context, limit int) ([]repository.OutboxMessage, error) {
	pending := []repository.OutboxMessage{}
	for _, m := range f.messages {
		if m.Status == repository.OutboxStatusPending {
			pending = append(pending, m)
		}
		if len(pending) == limit {
			break
		}
	}
	return pending, nil
}

func (f *fakeStore) MarkSent(_ context.Context, id int64, sentAt time.Time) error {
	m := f.find(id)
	m.Status = repository.OutboxStatusSent
	m.SentAt = &sentAt
	return nil
}

func (f *fakeStore) MarkSkipped(_ context.Context, id int64, reason string) error {
	m := f.find(id)
	m.Status = repository.OutboxStatusSkipped
	m.LastError = reason
	return nil
}

func (f *fakeStore) MarkRetry(_ context.Context, id int64, attempts int, nextAttemptAt time.Time, lastErr string) error {
	m := f.find(id)
	m.Attempts = attempts
	m.NextAttemptAt = nextAttemptAt
	m.LastError = lastErr
	return nil
}

func (f *fakeStore) MarkFailed(_ context.Context, id int64, attempts int, lastErr string) error {
	m := f.find(id)
	m.Status = repository.OutboxStatusFailed
	m.Attempts = attempts
	m.LastError = lastErr
	return nil
}

// fakeSender lets a test script per-recipient or per-message outcomes and
// records the order Send was actually called in.
type fakeSender struct {
	// fail, keyed by message id, is the error Send returns for that id —
	// absent means success.
	fail map[int64]error
	// calls records every message id Send was invoked with, in order.
	calls []int64
}

func (f *fakeSender) Send(_ context.Context, msg repository.OutboxMessage) error {
	f.calls = append(f.calls, msg.ID)
	if err, ok := f.fail[msg.ID]; ok {
		return err
	}
	return nil
}

func TestWorker_Tick_SendsAPendingMessageAndMarksItSent(t *testing.T) {
	store := &fakeStore{messages: []repository.OutboxMessage{
		{ID: 1, EventID: "evt-1", RecipientUserID: userID(10), Status: repository.OutboxStatusPending},
	}}
	sender := &fakeSender{}
	w := NewWorker(store, sender, func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) })

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(sender.calls) != 1 || sender.calls[0] != 1 {
		t.Fatalf("expected message 1 to be sent, got calls %+v", sender.calls)
	}
	if store.messages[0].Status != repository.OutboxStatusSent {
		t.Fatalf("expected message marked sent, got %+v", store.messages[0])
	}
}

// TestWorker_Tick_OrdersDeliveryPerRecipient covers the AC bullet directly:
// messages for one recipient are sent in the order they were queued. A
// second recipient's message, interleaved in id order, must not be held up
// by the first recipient's — and must not jump ahead of it either.
func TestWorker_Tick_OrdersDeliveryPerRecipient(t *testing.T) {
	store := &fakeStore{messages: []repository.OutboxMessage{
		{ID: 1, EventID: "evt-1", RecipientUserID: userID(10), Status: repository.OutboxStatusPending},
		{ID: 2, EventID: "evt-2", RecipientUserID: userID(20), Status: repository.OutboxStatusPending},
		{ID: 3, EventID: "evt-3", RecipientUserID: userID(10), Status: repository.OutboxStatusPending},
	}}
	sender := &fakeSender{}
	w := NewWorker(store, sender, func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) })

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(sender.calls) != 3 {
		t.Fatalf("expected all 3 messages sent, got calls %+v", sender.calls)
	}
	// Recipient 10's two messages (1, then 3) must appear in that relative
	// order among the calls, wherever recipient 20's message 2 lands.
	var firstIdx, thirdIdx int
	for i, id := range sender.calls {
		if id == 1 {
			firstIdx = i
		}
		if id == 3 {
			thirdIdx = i
		}
	}
	if firstIdx >= thirdIdx {
		t.Fatalf("expected message 1 to be sent before message 3 for the same recipient, got calls %+v", sender.calls)
	}
}

// TestWorker_Tick_ARecipientsBackoffDoesNotBlockAnotherRecipient covers
// ADR-0060's ordering scope: a message still backing off (NextAttemptAt in
// the future) stalls only *its own* recipient's later messages, never a
// different recipient's message that happens to sort after it by id.
func TestWorker_Tick_ARecipientsBackoffDoesNotBlockAnotherRecipient(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeStore{messages: []repository.OutboxMessage{
		{ID: 1, EventID: "evt-1", RecipientUserID: userID(10), Status: repository.OutboxStatusPending, NextAttemptAt: now.Add(time.Hour)},
		{ID: 2, EventID: "evt-2", RecipientUserID: userID(20), Status: repository.OutboxStatusPending, NextAttemptAt: now},
		{ID: 3, EventID: "evt-3", RecipientUserID: userID(10), Status: repository.OutboxStatusPending, NextAttemptAt: now},
	}}
	sender := &fakeSender{}
	w := NewWorker(store, sender, func() time.Time { return now })

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(sender.calls) != 1 || sender.calls[0] != 2 {
		t.Fatalf("expected only message 2 (a different recipient) to send this tick, got calls %+v", sender.calls)
	}
	if store.messages[0].Status != repository.OutboxStatusPending {
		t.Fatalf("expected message 1 to remain pending (still backing off)")
	}
	if store.messages[2].Status != repository.OutboxStatusPending {
		t.Fatalf("expected message 3 to remain pending too — blocked behind message 1, its own recipient's earlier message")
	}
}

func TestWorker_Tick_FailureBelowMaxAttemptsSchedulesRetryWithBackoff(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeStore{messages: []repository.OutboxMessage{
		{ID: 1, EventID: "evt-1", RecipientUserID: userID(10), Status: repository.OutboxStatusPending, Attempts: 0},
	}}
	sender := &fakeSender{fail: map[int64]error{1: errors.New("smtp: connection refused")}}
	w := NewWorker(store, sender, func() time.Time { return now })

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	got := store.messages[0]
	if got.Status != repository.OutboxStatusPending {
		t.Fatalf("expected the message to stay pending after a retryable failure, got %+v", got)
	}
	if got.Attempts != 1 {
		t.Fatalf("expected 1 attempt recorded, got %d", got.Attempts)
	}
	if !got.NextAttemptAt.After(now) {
		t.Fatalf("expected a future NextAttemptAt (backoff), got %v", got.NextAttemptAt)
	}
	if got.LastError == "" {
		t.Fatalf("expected the failure reason recorded")
	}
}

// TestWorker_Tick_ExhaustingBackoffReachesTerminalFailedState covers the AC
// bullet: a send failure retries with backoff and reaches a terminal failed
// state rather than retrying forever.
func TestWorker_Tick_ExhaustingBackoffReachesTerminalFailedState(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeStore{messages: []repository.OutboxMessage{
		{ID: 1, EventID: "evt-1", RecipientUserID: userID(10), Status: repository.OutboxStatusPending, Attempts: maxAttemptsFor("") - 1},
	}}
	sender := &fakeSender{fail: map[int64]error{1: errors.New("smtp: giving up")}}
	w := NewWorker(store, sender, func() time.Time { return now })

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	got := store.messages[0]
	if got.Status != repository.OutboxStatusFailed {
		t.Fatalf("expected a terminal failed state after exhausting every retry, got %+v", got)
	}
	if got.Attempts != maxAttemptsFor("") {
		t.Fatalf("expected %d attempts recorded, got %d", maxAttemptsFor(""), got.Attempts)
	}
}

// TestWorker_Tick_EmailRecipientIsBlockedSeparatelyFromAUserRecipient covers
// an email-shaped OutboxMessage (#200, ADR-0058): its own backoff blocks
// only later messages to that same address, never a User-backed recipient's
// message, and the reverse — a User recipient backing off never blocks an
// unrelated email recipient either.
func TestWorker_Tick_EmailRecipientIsBlockedSeparatelyFromAUserRecipient(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeStore{messages: []repository.OutboxMessage{
		{ID: 1, EventID: "evt-1", RecipientEmail: email("guest@example.com"), Status: repository.OutboxStatusPending, NextAttemptAt: now.Add(time.Hour)},
		{ID: 2, EventID: "evt-2", RecipientUserID: userID(20), Status: repository.OutboxStatusPending, NextAttemptAt: now},
		{ID: 3, EventID: "evt-3", RecipientEmail: email("guest@example.com"), Status: repository.OutboxStatusPending, NextAttemptAt: now},
	}}
	sender := &fakeSender{}
	w := NewWorker(store, sender, func() time.Time { return now })

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(sender.calls) != 1 || sender.calls[0] != 2 {
		t.Fatalf("expected only message 2 (the unrelated User recipient) to send this tick, got calls %+v", sender.calls)
	}
	if store.messages[2].Status != repository.OutboxStatusPending {
		t.Fatalf("expected message 3 to remain pending — blocked behind message 1, its own recipient's still-backing-off earlier message")
	}
}

// TestWorker_Tick_EmailRecipientKeyIsCaseInsensitive covers matching
// differently-cased addresses to the same recipient (ADR-0058's
// case-insensitive email everywhere) so ordering isn't accidentally lost to
// a stray uppercase letter.
func TestWorker_Tick_EmailRecipientKeyIsCaseInsensitive(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeStore{messages: []repository.OutboxMessage{
		{ID: 1, EventID: "evt-1", RecipientEmail: email("Guest@Example.com"), Status: repository.OutboxStatusPending, NextAttemptAt: now.Add(time.Hour)},
		{ID: 2, EventID: "evt-2", RecipientEmail: email("guest@example.com"), Status: repository.OutboxStatusPending, NextAttemptAt: now},
	}}
	sender := &fakeSender{}
	w := NewWorker(store, sender, func() time.Time { return now })

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(sender.calls) != 0 {
		t.Fatalf("expected message 2 blocked behind message 1's same (case-insensitively) recipient, got calls %+v", sender.calls)
	}
}

// TestWorker_Tick_ACancelNeverOvertakesTheRequestItWithdraws covers the AC
// bullet directly (#201): a REQUEST and a later CANCEL queued for the same
// recipient — the shape EventService produces when an Attendee is removed
// or an Event deleted shortly after being invited — are sent in that same
// order, regardless of Method. Worker.Tick draws no distinction between the
// two; per-recipient ordering already falls out of ListPending's id order
// plus per-recipient blocking, which this exercises directly.
func TestWorker_Tick_ACancelNeverOvertakesTheRequestItWithdraws(t *testing.T) {
	store := &fakeStore{messages: []repository.OutboxMessage{
		{ID: 1, EventID: "evt-1", RecipientUserID: userID(10), Method: repository.OutboxMethodRequest, Status: repository.OutboxStatusPending},
		{ID: 2, EventID: "evt-2", RecipientUserID: userID(20), Method: repository.OutboxMethodRequest, Status: repository.OutboxStatusPending},
		{ID: 3, EventID: "evt-1", RecipientUserID: userID(10), Method: repository.OutboxMethodCancel, Status: repository.OutboxStatusPending},
	}}
	sender := &fakeSender{}
	w := NewWorker(store, sender, func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) })

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(sender.calls) != 3 {
		t.Fatalf("expected all 3 messages sent, got calls %+v", sender.calls)
	}
	var requestIdx, cancelIdx int
	for i, id := range sender.calls {
		if id == 1 {
			requestIdx = i
		}
		if id == 3 {
			cancelIdx = i
		}
	}
	if requestIdx >= cancelIdx {
		t.Fatalf("expected the REQUEST (message 1) sent before the CANCEL that withdraws it (message 3), got calls %+v", sender.calls)
	}
}

// TestWorker_Tick_ARequestStillBackingOffBlocksItsOwnCancel covers the same
// AC bullet from the failure side: if the original REQUEST hasn't gone out
// yet (still backing off after a transient failure), the CANCEL behind it
// must not jump ahead and reach the recipient first.
func TestWorker_Tick_ARequestStillBackingOffBlocksItsOwnCancel(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeStore{messages: []repository.OutboxMessage{
		{ID: 1, EventID: "evt-1", RecipientUserID: userID(10), Method: repository.OutboxMethodRequest, Status: repository.OutboxStatusPending, NextAttemptAt: now.Add(time.Hour)},
		{ID: 2, EventID: "evt-1", RecipientUserID: userID(10), Method: repository.OutboxMethodCancel, Status: repository.OutboxStatusPending, NextAttemptAt: now},
	}}
	sender := &fakeSender{}
	w := NewWorker(store, sender, func() time.Time { return now })

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(sender.calls) != 0 {
		t.Fatalf("expected the CANCEL blocked behind its own still-backing-off REQUEST, got calls %+v", sender.calls)
	}
}

// fakeSenderWithTerminalHandler wraps fakeSender to also implement
// TerminalFailureHandler (#291, ADR-0075) — recording every call so a test
// can assert Tick invokes it exactly when, and only when, a message reaches
// the terminal failed state.
type fakeSenderWithTerminalHandler struct {
	fakeSender
	terminalCalls []int64
}

func (f *fakeSenderWithTerminalHandler) HandleTerminalFailure(_ context.Context, msg repository.OutboxMessage, _ error) error {
	f.terminalCalls = append(f.terminalCalls, msg.ID)
	return nil
}

// TestWorker_Tick_CallsTerminalFailureHandlerOnlyOnceExhausted covers #291's
// own hook: a Sender implementing TerminalFailureHandler is called the
// moment Tick marks a message permanently failed, and not before — a
// still-retrying failure must never trigger it.
func TestWorker_Tick_CallsTerminalFailureHandlerOnlyOnceExhausted(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeStore{messages: []repository.OutboxMessage{
		{ID: 1, EventID: "evt-1", RecipientUserID: userID(10), Status: repository.OutboxStatusPending, Attempts: 0},
		{ID: 2, EventID: "evt-2", RecipientUserID: userID(20), Status: repository.OutboxStatusPending, Attempts: maxAttemptsFor("") - 1},
	}}
	sender := &fakeSenderWithTerminalHandler{fakeSender: fakeSender{fail: map[int64]error{
		1: errors.New("smtp: connection refused"),
		2: errors.New("smtp: giving up"),
	}}}
	w := NewWorker(store, sender, func() time.Time { return now })

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if store.messages[0].Status != repository.OutboxStatusPending {
		t.Fatalf("expected message 1 to still be retrying, got %+v", store.messages[0])
	}
	if store.messages[1].Status != repository.OutboxStatusFailed {
		t.Fatalf("expected message 2 permanently failed, got %+v", store.messages[1])
	}
	if len(sender.terminalCalls) != 1 || sender.terminalCalls[0] != 2 {
		t.Fatalf("expected the terminal handler called exactly once, for message 2, got %+v", sender.terminalCalls)
	}
}

func TestWorker_Tick_NothingPendingIsANoOp(t *testing.T) {
	store := &fakeStore{}
	sender := &fakeSender{}
	w := NewWorker(store, sender, time.Now)

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(sender.calls) != 0 {
		t.Fatalf("expected nothing sent, got calls %+v", sender.calls)
	}
}

// TestWorker_Tick_SkippedIsTerminalAndClaimsNoDelivery covers ADR-0079's
// central distinction. A Sender that dispatched nothing returns ErrSkipped, and
// the row must record that rather than the successful delivery a bare nil once
// implied: Status skipped, the reason kept, sent_at untouched, and no attempt
// consumed — there is nothing to retry, because the state that produced the
// skip does not resolve on its own.
func TestWorker_Tick_SkippedIsTerminalAndClaimsNoDelivery(t *testing.T) {
	store := &fakeStore{messages: []repository.OutboxMessage{
		{ID: 1, EventID: "evt-1", Kind: repository.OutboxKindWriteBack, Method: repository.OutboxMethodPatch, Status: repository.OutboxStatusPending},
	}}
	sender := &fakeSender{fail: map[int64]error{
		1: fmt.Errorf("%w: this linked calendar is no longer writable at google", ErrSkipped),
	}}
	w := NewWorker(store, sender, func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) })

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	msg := store.messages[0]
	if msg.Status != repository.OutboxStatusSkipped {
		t.Fatalf("expected the message marked skipped, got %q", msg.Status)
	}
	if msg.SentAt != nil {
		t.Fatalf("expected no sent_at on a message that was never dispatched, got %v", msg.SentAt)
	}
	if msg.Attempts != 0 {
		t.Fatalf("expected a skip to consume no attempt, got %d", msg.Attempts)
	}
	if !strings.Contains(msg.LastError, "no longer writable") {
		t.Fatalf("expected the skip reason recorded, got %q", msg.LastError)
	}

	// Terminal: a second Tick must not pick it up again.
	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if len(sender.calls) != 1 {
		t.Fatalf("expected a skipped message never retried, got calls %+v", sender.calls)
	}
}

// TestWorker_Tick_PermanentFailureSkipsTheBackoffSchedule covers the other half
// of ADR-0079: a failure the Sender has classified as structural (a Google 4xx —
// a malformed body, a dead grant, a calendar the account may not write) fails on
// the first response instead of walking all four backoff entries. The delay
// mattered because a Write-back has no user-visible marker until its message is
// terminal, so retrying a hopeless push silently withholds the badge that says
// an edit never landed.
func TestWorker_Tick_PermanentFailureSkipsTheBackoffSchedule(t *testing.T) {
	store := &fakeStore{messages: []repository.OutboxMessage{
		{ID: 1, EventID: "evt-1", Kind: repository.OutboxKindWriteBack, Method: repository.OutboxMethodPatch, Status: repository.OutboxStatusPending},
	}}
	sender := &fakeSender{fail: map[int64]error{
		1: fmt.Errorf("%w: could not push this event's changes to google: status 400", ErrPermanent),
	}}
	terminal := &recordingTerminalSender{fakeSender: sender}
	w := NewWorker(store, terminal, func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) })

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	msg := store.messages[0]
	if msg.Status != repository.OutboxStatusFailed {
		t.Fatalf("expected the message failed on its first attempt, got %q after %d attempts", msg.Status, msg.Attempts)
	}
	if msg.Attempts != 1 {
		t.Fatalf("expected exactly one attempt spent, got %d", msg.Attempts)
	}
	// The marker hook fires here, not thirteen minutes later.
	if terminal.handled != 1 {
		t.Fatalf("expected the terminal-failure handler called once, got %d", terminal.handled)
	}
}

// recordingTerminalSender is a fakeSender that also implements
// TerminalFailureHandler, so a test can assert the marker hook fired.
type recordingTerminalSender struct {
	*fakeSender
	handled int
}

func (r *recordingTerminalSender) HandleTerminalFailure(_ context.Context, _ repository.OutboxMessage, _ error) error {
	r.handled++
	return nil
}
