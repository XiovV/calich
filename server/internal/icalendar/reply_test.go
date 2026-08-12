package icalendar

import (
	"testing"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calendar/server/internal/repository"
)

// buildReplyCalendar builds a minimal METHOD:REPLY VCALENDAR carrying one
// VEVENT with the given UID, optional RECURRENCE-ID, and one ATTENDEE line —
// the shape a mainstream mail client's Accept/Decline/Tentative button
// produces (RFC 5546), without needing a REPLY-side encoder this app has no
// other reason to have.
func buildReplyCalendar(t *testing.T, uid string, recurrenceID *time.Time, attendee, partstat string) *ical.Calendar {
	t.Helper()

	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropMethod, "REPLY")

	v := ical.NewEvent()
	v.Props.SetText(ical.PropUID, uid)
	v.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())

	if recurrenceID != nil {
		prop, err := newDateTimeProp(ical.PropRecurrenceID, *recurrenceID, false, nil)
		if err != nil {
			t.Fatalf("build recurrence-id: %v", err)
		}
		v.Props.Add(prop)
	}

	attendeeProp := ical.NewProp(ical.PropAttendee)
	if partstat != "" {
		attendeeProp.Params.Set(ical.ParamParticipationStatus, partstat)
	}
	attendeeProp.Value = "mailto:" + attendee
	v.Props.Add(attendeeProp)

	cal.Children = append(cal.Children, v.Component)
	return cal
}

func TestParseReply_DecodesUIDAttendeeAndResponse(t *testing.T) {
	cal := buildReplyCalendar(t, "evt-1", nil, "Guest@Example.com", "ACCEPTED")

	parsed, ok, err := ParseReply(cal)
	if err != nil {
		t.Fatalf("parse reply: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true for a METHOD:REPLY")
	}
	if parsed.UID != "evt-1" {
		t.Fatalf("expected UID evt-1, got %q", parsed.UID)
	}
	if parsed.RecurrenceID != nil {
		t.Fatalf("expected no RecurrenceID, got %v", *parsed.RecurrenceID)
	}
	// Lowercased so a poller comparison needs no further normalization —
	// mail addresses arrive in whatever case the sending client used.
	if parsed.Attendee != "guest@example.com" {
		t.Fatalf("expected lowercased attendee address, got %q", parsed.Attendee)
	}
	if parsed.Response != repository.ResponseAccepted {
		t.Fatalf("expected response %q, got %q", repository.ResponseAccepted, parsed.Response)
	}
}

func TestParseReply_DecodesRecurrenceID(t *testing.T) {
	occurrence := time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC)
	cal := buildReplyCalendar(t, "evt-1", &occurrence, "guest@example.com", "DECLINED")

	parsed, ok, err := ParseReply(cal)
	if err != nil {
		t.Fatalf("parse reply: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if parsed.RecurrenceID == nil || !parsed.RecurrenceID.Equal(occurrence) {
		t.Fatalf("expected RecurrenceID %v, got %v", occurrence, parsed.RecurrenceID)
	}
	if parsed.Response != repository.ResponseDeclined {
		t.Fatalf("expected response %q, got %q", repository.ResponseDeclined, parsed.Response)
	}
}

func TestParseReply_EveryPartstatTranslates(t *testing.T) {
	cases := map[string]string{
		"ACCEPTED":     repository.ResponseAccepted,
		"DECLINED":     repository.ResponseDeclined,
		"TENTATIVE":    repository.ResponseTentative,
		"NEEDS-ACTION": repository.ResponseNeedsAction,
		"accepted":     repository.ResponseAccepted, // case-insensitive
	}
	for partstat, want := range cases {
		t.Run(partstat, func(t *testing.T) {
			cal := buildReplyCalendar(t, "evt-1", nil, "guest@example.com", partstat)
			parsed, ok, err := ParseReply(cal)
			if err != nil {
				t.Fatalf("parse reply: %v", err)
			}
			if !ok || parsed.Response != want {
				t.Fatalf("expected response %q, got %q (ok=%v)", want, parsed.Response, ok)
			}
		})
	}
}

// TestParseReply_NotAReply covers the "mail that is not a calendar reply is
// left alone" AC: a METHOD:REQUEST (or any non-REPLY method) is not an
// error, just ok=false, so the poller leaves the mail untouched instead of
// logging a spurious parse failure.
func TestParseReply_NotAReply(t *testing.T) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropMethod, "REQUEST")
	v := ical.NewEvent()
	v.Props.SetText(ical.PropUID, "evt-1")
	cal.Children = append(cal.Children, v.Component)

	parsed, ok, err := ParseReply(cal)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for a non-REPLY method, got %+v", parsed)
	}
}

func TestParseReply_NoMethodAtAll(t *testing.T) {
	cal := ical.NewCalendar()
	v := ical.NewEvent()
	v.Props.SetText(ical.PropUID, "evt-1")
	cal.Children = append(cal.Children, v.Component)

	_, ok, err := ParseReply(cal)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false with no METHOD property")
	}
}

// TestParseReply_UnrecognizedPartstat covers a REPLY that names a PARTSTAT
// this app doesn't model as a Response (e.g. DELEGATED) — dropped as an
// error the poller logs, not guessed at (#202).
func TestParseReply_UnrecognizedPartstat(t *testing.T) {
	cal := buildReplyCalendar(t, "evt-1", nil, "guest@example.com", "DELEGATED")

	_, ok, err := ParseReply(cal)
	if ok {
		t.Fatalf("expected ok=false for an unrecognized PARTSTAT")
	}
	if err == nil {
		t.Fatalf("expected an error for an unrecognized PARTSTAT")
	}
}

func TestParseReply_MultipleAttendees_PicksTheOneCarryingPartstat(t *testing.T) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropMethod, "REPLY")

	v := ical.NewEvent()
	v.Props.SetText(ical.PropUID, "evt-1")

	organizerEcho := ical.NewProp(ical.PropAttendee)
	organizerEcho.Value = "mailto:organizer@example.com"
	v.Props.Add(organizerEcho)

	responder := ical.NewProp(ical.PropAttendee)
	responder.Params.Set(ical.ParamParticipationStatus, "TENTATIVE")
	responder.Value = "mailto:responder@example.com"
	v.Props.Add(responder)

	cal.Children = append(cal.Children, v.Component)

	parsed, ok, err := ParseReply(cal)
	if err != nil {
		t.Fatalf("parse reply: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if parsed.Attendee != "responder@example.com" {
		t.Fatalf("expected the PARTSTAT-carrying attendee, got %q", parsed.Attendee)
	}
}

func TestParseReply_MissingUID(t *testing.T) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropMethod, "REPLY")

	v := ical.NewEvent()
	attendeeProp := ical.NewProp(ical.PropAttendee)
	attendeeProp.Params.Set(ical.ParamParticipationStatus, "ACCEPTED")
	attendeeProp.Value = "mailto:guest@example.com"
	v.Props.Add(attendeeProp)
	cal.Children = append(cal.Children, v.Component)

	_, ok, err := ParseReply(cal)
	if ok || err == nil {
		t.Fatalf("expected an error and ok=false for a missing UID, got ok=%v err=%v", ok, err)
	}
}
