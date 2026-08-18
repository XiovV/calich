// invitation.go renders one Attendee's Invitation or Cancellation
// (ADR-0059): a standalone VCALENDAR carrying a single VEVENT.
// InvitationToICal's METHOD:REQUEST is built from the same buildVEvent the
// CalDAV/Calendar-file codec uses so the two never drift apart in shape —
// only ORGANIZER and VALARM differ. CancellationToICal's METHOD:CANCEL
// (#201) is built from a repository.OutboxCancelSnapshot instead, since by
// the time it sends, the row (or Attendee row) it withdraws may already be
// gone.
package icalendar

import (
	"fmt"
	"strconv"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calich/server/internal/repository"
)

// MethodRequest is a fresh (or re-issued) invite. MethodCancel is a
// withdrawal, queued on Attendee removal or Event deletion (#201).
const (
	MethodRequest = "REQUEST"
	MethodCancel  = "CANCEL"
)

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

// CancellationToICal renders snap as a Cancellation: a METHOD:CANCEL
// VCALENDAR withdrawing the Invitation previously sent to one recipient
// (ADR-0059, #201). Built entirely from snap rather than a live Event/
// Attendee lookup — unlike InvitationToICal, which always reflects state as
// it stands right now, a Cancellation's own purpose is to still render
// correctly after the row (or Attendee row) it withdraws is gone. ORGANIZER
// names fromAddress (this instance's own mailbox), the same ADR-0059 split
// InvitationToICal applies via overrideInvitationOrganizer; ATTENDEE names
// only the one recipient this message addresses, not the row's whole
// Attendee list — RFC 5546's CANCEL carries just the Attendee(s) actually
// being cancelled.
func CancellationToICal(snap repository.OutboxCancelSnapshot, fromAddress string) (*ical.Calendar, error) {
	cal := newVCalendar()
	cal.Props.SetText(ical.PropMethod, MethodCancel)

	if snap.Tzid != nil {
		vtz, err := buildVTimezone(*snap.Tzid)
		if err != nil {
			return nil, fmt.Errorf("build vtimezone %q: %w", *snap.Tzid, err)
		}
		cal.Children = append(cal.Children, vtz)
	}

	v := ical.NewEvent()
	v.Props.SetText(ical.PropUID, snap.UID)
	v.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	v.Props.SetText(ical.PropSummary, snap.Title)
	v.Props.SetText(ical.PropStatus, "CANCELLED")

	sequenceProp := ical.NewProp(ical.PropSequence)
	sequenceProp.Value = strconv.FormatInt(snap.Sequence, 10)
	v.Props.Set(sequenceProp)

	startProp, err := newDateTimeProp(ical.PropDateTimeStart, snap.Start, snap.AllDay, snap.Tzid)
	if err != nil {
		return nil, err
	}
	v.Props.Add(startProp)
	endProp, err := newDateTimeProp(ical.PropDateTimeEnd, snap.End, snap.AllDay, snap.Tzid)
	if err != nil {
		return nil, err
	}
	v.Props.Add(endProp)

	if snap.RecurrenceID != nil {
		prop, err := newDateTimeProp(ical.PropRecurrenceID, *snap.RecurrenceID, snap.AllDay, snap.Tzid)
		if err != nil {
			return nil, fmt.Errorf("build recurrence-id: %w", err)
		}
		v.Props.Add(prop)
	}

	v.Props.Add(organizerProp(snap.OrganizerName, fromAddress))

	attendeeProp := ical.NewProp(ical.PropAttendee)
	if snap.RecipientName != "" {
		attendeeProp.Params.Set(ical.ParamCommonName, snap.RecipientName)
	}
	attendeeProp.Value = "mailto:" + snap.RecipientEmail
	v.Props.Add(attendeeProp)

	cal.Children = append(cal.Children, v.Component)
	return cal, nil
}
