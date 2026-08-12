package reply

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calendar/server/internal/icalendar"
	"github.com/XiovV/calendar/server/internal/repository"
)

// replyMessage builds a minimal RFC 822 message carrying a METHOD:REPLY
// text/calendar body — the shape a mail client's Accept/Decline/Tentative
// button produces — for uid.
func replyMessage(uid uint32, eventUID, attendee, partstat string) RawMessage {
	ics := "BEGIN:VCALENDAR\r\n" +
		"METHOD:REPLY\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:" + eventUID + "\r\n" +
		"ATTENDEE;PARTSTAT=" + partstat + ":mailto:" + attendee + "\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	raw := "From: " + attendee + "\r\n" +
		"To: calendar@example.com\r\n" +
		"Content-Type: text/calendar; method=REPLY\r\n" +
		"\r\n" +
		ics
	return RawMessage{UID: uid, Bytes: []byte(raw)}
}

// ordinaryMessage builds an RFC 822 message with no calendar part at all —
// a human reply, not a Response.
func ordinaryMessage(uid uint32) RawMessage {
	raw := "From: someone@example.com\r\n" +
		"To: calendar@example.com\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"Sounds good, see you there.\r\n"
	return RawMessage{UID: uid, Bytes: []byte(raw)}
}

// unparseableCalendarMessage carries a text/calendar part that isn't valid
// iCalendar at all.
func unparseableCalendarMessage(uid uint32) RawMessage {
	raw := "From: someone@example.com\r\n" +
		"To: calendar@example.com\r\n" +
		"Content-Type: text/calendar; method=REPLY\r\n" +
		"\r\n" +
		"this is not iCalendar\r\n"
	return RawMessage{UID: uid, Bytes: []byte(raw)}
}

type fakeMailReader struct {
	messages      []RawMessage
	fetchErr      error
	markSeenErr   error
	markSeenCalls [][]uint32
}

func (f *fakeMailReader) FetchUnseen(_ context.Context) ([]RawMessage, error) {
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	return f.messages, nil
}

func (f *fakeMailReader) MarkSeen(_ context.Context, uids []uint32) error {
	f.markSeenCalls = append(f.markSeenCalls, uids)
	return f.markSeenErr
}

// fakeResponseApplier scripts ApplyReply's outcome per event UID.
type fakeResponseApplier struct {
	applied map[string]bool
	errs    map[string]error
	calls   []icalendar.ParsedReply
}

func (f *fakeResponseApplier) ApplyReply(_ context.Context, reply icalendar.ParsedReply) (bool, error) {
	f.calls = append(f.calls, reply)
	if err, ok := f.errs[reply.UID]; ok {
		return false, err
	}
	return f.applied[reply.UID], nil
}

func TestWorker_Tick_MatchedReply_AppliesAndMarksSeen(t *testing.T) {
	mail := &fakeMailReader{messages: []RawMessage{replyMessage(1, "evt-1", "guest@example.com", "ACCEPTED")}}
	responses := &fakeResponseApplier{applied: map[string]bool{"evt-1": true}}

	w := NewWorker(mail, responses)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(responses.calls) != 1 || responses.calls[0].UID != "evt-1" || responses.calls[0].Attendee != "guest@example.com" || responses.calls[0].Response != repository.ResponseAccepted {
		t.Fatalf("expected ApplyReply called with the decoded reply, got %+v", responses.calls)
	}
	if len(mail.markSeenCalls) != 1 || len(mail.markSeenCalls[0]) != 1 || mail.markSeenCalls[0][0] != 1 {
		t.Fatalf("expected uid 1 marked seen, got %+v", mail.markSeenCalls)
	}
}

// TestWorker_Tick_UnmatchedReply_StillMarksSeen covers "a reply naming an
// unknown UID is logged and dropped without erroring the poller" — applied
// false with no error still marks the message seen so it's not reprocessed
// forever.
func TestWorker_Tick_UnmatchedReply_StillMarksSeen(t *testing.T) {
	mail := &fakeMailReader{messages: []RawMessage{replyMessage(1, "no-such-event", "guest@example.com", "ACCEPTED")}}
	responses := &fakeResponseApplier{applied: map[string]bool{}}

	w := NewWorker(mail, responses)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(mail.markSeenCalls) != 1 || len(mail.markSeenCalls[0]) != 1 || mail.markSeenCalls[0][0] != 1 {
		t.Fatalf("expected the unmatched reply's uid marked seen anyway, got %+v", mail.markSeenCalls)
	}
}

// TestWorker_Tick_OrdinaryMail_NeverMarkedSeen covers "mail that is not a
// calendar reply is left alone": Tick never calls MarkSeen at all when
// nothing in the batch was a calendar REPLY.
func TestWorker_Tick_OrdinaryMail_NeverMarkedSeen(t *testing.T) {
	mail := &fakeMailReader{messages: []RawMessage{ordinaryMessage(1)}}
	responses := &fakeResponseApplier{}

	w := NewWorker(mail, responses)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(responses.calls) != 0 {
		t.Fatalf("expected ApplyReply never called for ordinary mail, got %+v", responses.calls)
	}
	if len(mail.markSeenCalls) != 0 {
		t.Fatalf("expected MarkSeen never called, got %+v", mail.markSeenCalls)
	}
}

// TestWorker_Tick_ApplyReplyError_LeavesMessageUnseen covers the
// infrastructure-hiccup case: an error (not an unmatched reply) leaves the
// message unseen so it's retried on the next Tick rather than dropped.
func TestWorker_Tick_ApplyReplyError_LeavesMessageUnseen(t *testing.T) {
	mail := &fakeMailReader{messages: []RawMessage{replyMessage(1, "evt-1", "guest@example.com", "ACCEPTED")}}
	responses := &fakeResponseApplier{errs: map[string]error{"evt-1": errors.New("db unavailable")}}

	w := NewWorker(mail, responses)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(mail.markSeenCalls) != 0 {
		t.Fatalf("expected the message left unseen after an apply error, got %+v", mail.markSeenCalls)
	}
}

func TestWorker_Tick_UnparseableCalendarPart_LeavesMessageUnseen(t *testing.T) {
	mail := &fakeMailReader{messages: []RawMessage{unparseableCalendarMessage(1)}}
	responses := &fakeResponseApplier{}

	w := NewWorker(mail, responses)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(mail.markSeenCalls) != 0 {
		t.Fatalf("expected no MarkSeen call for an unparseable calendar part, got %+v", mail.markSeenCalls)
	}
	if len(responses.calls) != 0 {
		t.Fatalf("expected ApplyReply never called, got %+v", responses.calls)
	}
}

// TestWorker_Tick_MixedBatch_OnlyMarksRecognizedRepliesSeen exercises a
// batch with an ordinary message, a matched reply, and an unmatched reply
// together, asserting each is handled independently — one broken/irrelevant
// message never blocks the rest of the batch.
func TestWorker_Tick_MixedBatch_OnlyMarksRecognizedRepliesSeen(t *testing.T) {
	mail := &fakeMailReader{messages: []RawMessage{
		ordinaryMessage(1),
		replyMessage(2, "evt-1", "guest@example.com", "ACCEPTED"),
		replyMessage(3, "unknown-event", "guest@example.com", "DECLINED"),
	}}
	responses := &fakeResponseApplier{applied: map[string]bool{"evt-1": true}}

	w := NewWorker(mail, responses)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(mail.markSeenCalls) != 1 {
		t.Fatalf("expected exactly one MarkSeen call, got %+v", mail.markSeenCalls)
	}
	got := mail.markSeenCalls[0]
	if len(got) != 2 {
		t.Fatalf("expected uids 2 and 3 marked seen, got %+v", got)
	}
	seenSet := map[uint32]bool{got[0]: true, got[1]: true}
	if !seenSet[2] || !seenSet[3] {
		t.Fatalf("expected uids 2 and 3 marked seen, got %+v", got)
	}
}

func TestWorker_Tick_FetchError_Propagates(t *testing.T) {
	mail := &fakeMailReader{fetchErr: errors.New("imap unavailable")}
	responses := &fakeResponseApplier{}

	w := NewWorker(mail, responses)
	if err := w.Tick(context.Background()); err == nil {
		t.Fatalf("expected the fetch error to propagate")
	}
}
