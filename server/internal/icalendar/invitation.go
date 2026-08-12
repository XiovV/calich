// invitation.go renders one Attendee's Invitation (ADR-0059): a standalone
// METHOD:REQUEST VCALENDAR carrying a single VEVENT, built from the same
// buildVEvent the CalDAV/Calendar-file codec uses so the two never drift
// apart in shape — only ORGANIZER and VALARM differ.
package icalendar

import (
	"fmt"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calendar/server/internal/repository"
)

// MethodRequest is the only Method an Invitation carries today — a fresh
// invite. MethodCancel (withdrawal on removal/delete, #201) isn't wired up
// yet.
const MethodRequest = "REQUEST"

// InvitationToICal renders event as an Invitation: buildVEvent's usual
// VEVENT shape (ATTENDEE lines included, ADR-0062), minus VALARM — the
// recipient's own client applies its own default alarm rather than the
// Organizer's habits — and with ORGANIZER naming fromAddress (this
// instance's own mailbox) instead of the Organizer's own address, since an
// iMIP REPLY is addressed to ORGANIZER and this instance cannot read a
// mailbox at gmail.com (ADR-0059). event.Reminders is expected to be unset
// by the caller (EventService.LoadInvitation never hydrates it); Children
// is cleared regardless, so a caller that hydrates it anyway still produces
// no VALARM.
//
// masterAnchor is the Master's own AllDay/Tzid, needed only when event is
// itself an Override, to format its RECURRENCE-ID the same way
// appendSeriesVEvents does (buildVEvent's recurrenceIDAnchor contract) — nil
// when event is a Master.
func InvitationToICal(event repository.Event, masterAnchor *repository.Event, fromAddress string) (*ical.Calendar, error) {
	cal := newVCalendar()
	cal.Props.SetText(ical.PropMethod, MethodRequest)

	if event.Tzid != nil {
		vtz, err := buildVTimezone(*event.Tzid)
		if err != nil {
			return nil, fmt.Errorf("build vtimezone %q: %w", *event.Tzid, err)
		}
		cal.Children = append(cal.Children, vtz)
	}

	uid := event.ID
	var recurrenceID *time.Time
	anchor := &event
	if event.ParentID != nil {
		uid = *event.ParentID
		recurrenceID = event.RecurrenceID
		if masterAnchor != nil {
			anchor = masterAnchor
		}
	}

	v, err := buildVEvent(event, uid, recurrenceID, anchor)
	if err != nil {
		return nil, fmt.Errorf("build invitation vevent: %w", err)
	}
	v.Children = nil // no VALARM in an Invitation, whatever Reminders event carries (ADR-0059)

	overrideInvitationOrganizer(v, event, fromAddress)

	cal.Children = append(cal.Children, v.Component)
	return cal, nil
}

// overrideInvitationOrganizer replaces the ORGANIZER buildVEvent already set
// via appendOrganizerProp (the CalDAV/Calendar-file form, naming the
// Organizer's own address) with the Invitation's own form, built by the same
// organizerProp: same CN, but mailto:fromAddress instead (ADR-0059). A no-op
// when e.CreatedBy is nil — appendOrganizerProp added no ORGANIZER to
// override in the first place.
func overrideInvitationOrganizer(v *ical.Event, e repository.Event, fromAddress string) {
	if e.CreatedBy == nil {
		return
	}
	v.Props.Set(organizerProp(e.CreatedByName, fromAddress))
}
