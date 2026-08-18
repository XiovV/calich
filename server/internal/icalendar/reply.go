// reply.go decodes a METHOD:REPLY VCALENDAR — an inbound Response arriving
// as ordinary mail (ADR-0059) — into the (UID, RECURRENCE-ID, Attendee
// address, Response) tuple the reply poller resolves against Attendee rows.
// A separate decode path from ParseCalendarObject (ical_decode.go), which is
// CalDAV's PUT path and deliberately ignores ATTENDEE inbound (ADR-0062): a
// REPLY's whole reason to exist is its ATTENDEE line.
package icalendar

import (
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calich/server/internal/repository"
)

// ParsedReply is one decoded METHOD:REPLY VCALENDAR.
type ParsedReply struct {
	UID string
	// RecurrenceID is set only when the REPLY answers one Occurrence of a
	// recurring Event rather than its Master (ADR-0059).
	RecurrenceID *time.Time
	// Attendee is the replying party's address, lowercased.
	Attendee string
	// Response is the Attendee Response the reply names —
	// repository.Response* vocabulary (ADR-0046), translated from PARTSTAT.
	Response string
}

// ParseReply decodes cal into a ParsedReply. ok is false when cal isn't a
// METHOD:REPLY at all — an ordinary calendar attachment, or the
// Invitation's own METHOD:REQUEST bounced back unmodified — which the
// caller leaves alone rather than treating as an error (#202).
func ParseReply(cal *ical.Calendar) (*ParsedReply, bool, error) {
	methodProp := cal.Props.Get(ical.PropMethod)
	if methodProp == nil || !strings.EqualFold(methodProp.Value, "REPLY") {
		return nil, false, nil
	}

	events := cal.Events()
	if len(events) == 0 {
		return nil, false, fmt.Errorf("REPLY calendar object has no VEVENT")
	}
	v := events[0]

	uid, err := v.Props.Text(ical.PropUID)
	if err != nil {
		return nil, false, fmt.Errorf("read uid: %w", err)
	}
	if uid == "" {
		return nil, false, fmt.Errorf("REPLY VEVENT missing UID")
	}

	var recurrenceID *time.Time
	if p := v.Props.Get(ical.PropRecurrenceID); p != nil {
		t, _, _, err := parseEventTime(p)
		if err != nil {
			return nil, false, fmt.Errorf("parse recurrence-id: %w", err)
		}
		recurrenceID = &t
	}

	attendeeProp := replyingAttendeeProp(v)
	if attendeeProp == nil {
		return nil, false, fmt.Errorf("REPLY VEVENT has no ATTENDEE")
	}
	address := strings.ToLower(strings.TrimPrefix(attendeeProp.Value, "mailto:"))
	if address == "" {
		return nil, false, fmt.Errorf("REPLY ATTENDEE has no address")
	}

	response, ok := partstatToResponse(attendeeProp.Params.Get(ical.ParamParticipationStatus))
	if !ok {
		return nil, false, fmt.Errorf("REPLY ATTENDEE %q has no recognized PARTSTAT", address)
	}

	return &ParsedReply{
		UID:          uid,
		RecurrenceID: recurrenceID,
		Attendee:     address,
		Response:     response,
	}, true, nil
}

// replyingAttendeeProp picks the ATTENDEE property a REPLY's answer belongs
// to. RFC 5546 says a REPLY carries exactly one, but some clients echo the
// whole guest list back with only the responder's own line carrying
// PARTSTAT — so among more than one ATTENDEE, the first carrying a PARTSTAT
// param wins; a single ATTENDEE is used regardless of whether it carries one
// (ParseReply itself rejects a missing/unrecognized PARTSTAT after).
func replyingAttendeeProp(v ical.Event) *ical.Prop {
	attendees := v.Props.Values(ical.PropAttendee)
	if len(attendees) == 0 {
		return nil
	}
	if len(attendees) == 1 {
		return &attendees[0]
	}
	for i := range attendees {
		if attendees[i].Params.Get(ical.ParamParticipationStatus) != "" {
			return &attendees[i]
		}
	}
	return &attendees[0]
}

// partstatToResponse translates PARTSTAT to attendees.response's vocabulary
// (ADR-0046) — the inverse of appendAttendeeProps's
// strings.ToUpper(a.Response). ok is false for a PARTSTAT this app doesn't
// model as a Response (empty, DELEGATED, or any other VEVENT-inapplicable
// value) — logged and dropped by the caller (#202) rather than guessed at.
func partstatToResponse(partstat string) (response string, ok bool) {
	switch strings.ToUpper(partstat) {
	case "ACCEPTED":
		return repository.ResponseAccepted, true
	case "DECLINED":
		return repository.ResponseDeclined, true
	case "TENTATIVE":
		return repository.ResponseTentative, true
	case "NEEDS-ACTION":
		return repository.ResponseNeedsAction, true
	default:
		return "", false
	}
}
