package icalendar

import (
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calich/server/internal/recurrence"
	"github.com/XiovV/calich/server/internal/repository"
)

// allDayReminderAnchorOffset is how far after midnight (an all-day Event's
// stored start) a Reminder's offset is actually measured from — 09:00, so a
// Reminder never fires in the middle of the night (ADR-0020).
const allDayReminderAnchorOffset = 9 * time.Hour

// buildVEvent renders one Event row (a Master or an Override) as a VEVENT.
// recurrenceID is nil for a Master. recurrenceIDAnchor is the Master whose
// rrule generated recurrenceID — its AllDay/Tzid define how RECURRENCE-ID is
// formatted, since it must match the original Occurrence the rule produced,
// independent of anything the Override itself changed. e.Color, if set, is
// snapped to the nearest CSS3 keyword and written as COLOR (RFC 7986,
// ADR-0043); an inherited (nil) color emits no COLOR property at all.
func buildVEvent(e repository.Event, uid string, recurrenceID *time.Time, recurrenceIDAnchor *repository.Event) (*ical.Event, error) {
	v := ical.NewEvent()
	v.Props.SetText(ical.PropUID, uid)
	v.Props.SetDateTime(ical.PropDateTimeStamp, e.CreatedAt.UTC())
	v.Props.SetText(ical.PropSummary, e.Title)
	if e.Description != "" {
		v.Props.SetText(ical.PropDescription, e.Description)
	}
	if e.Location != "" {
		v.Props.SetText(ical.PropLocation, e.Location)
	}
	if e.URL != "" {
		// Not SetText: it marks the prop VALUE=TEXT and backslash-escapes
		// characters a URL may legally contain, un-verbatim-ing it
		// (ADR-0063 requires byte-for-byte storage).
		urlProp := ical.NewProp(ical.PropURL)
		urlProp.Value = e.URL
		v.Props.Set(urlProp)
	}
	if e.Color != nil {
		r, g, b, err := parseHexRGB(*e.Color)
		if err != nil {
			return nil, fmt.Errorf("parse event color: %w", err)
		}
		v.Props.SetText(ical.PropColor, nearestCSS3Keyword(r, g, b))
	}

	// recurrenceID nil: RECURRENCE-ID (below) is built separately, against
	// recurrenceIDAnchor rather than e's own AllDay/Tzid.
	if err := appendTimeProps(v, e.Start, e.End, e.AllDay, e.Tzid, nil); err != nil {
		return nil, err
	}

	if e.Rrule != "" {
		// Not SetText: it marks the prop VALUE=TEXT and backslash-escapes
		// ";", which would corrupt RRULE's own ";"-delimited syntax.
		rruleProp := ical.NewProp(ical.PropRecurrenceRule)
		rruleProp.Value = e.Rrule
		v.Props.Set(rruleProp)
	}
	if recurrenceID != nil {
		prop, err := newDateTimeProp(ical.PropRecurrenceID, *recurrenceID, recurrenceIDAnchor.AllDay, recurrenceIDAnchor.Tzid)
		if err != nil {
			return nil, fmt.Errorf("build recurrence-id: %w", err)
		}
		v.Props.Add(prop)
	}

	appendOrganizerProp(v, e)
	appendAttendeeProps(v, e.Attendees)

	for _, reminder := range e.Reminders {
		v.Children = append(v.Children, buildVAlarm(e, reminder))
	}

	return v, nil
}

// appendOrganizerProp adds ORGANIZER naming e's Organizer — the User named
// by CreatedBy — with CN=CreatedByName and that User's own Email as the
// mailto address, the CalDAV/Calendar-file form ADR-0059 defines (the
// Invitation's own ORGANIZER, naming this instance's address instead, is
// built separately by overrideInvitationOrganizer, sharing organizerProp's
// same CN/mailto shape). A no-op when CreatedBy is nil: an Event whose
// Organizer's account was since deleted simply has none (CONTEXT.md).
func appendOrganizerProp(v *ical.Event, e repository.Event) {
	if e.CreatedBy == nil {
		return
	}
	v.Props.Add(organizerProp(e.CreatedByName, e.CreatedByEmail))
}

// organizerProp builds one ORGANIZER property naming name as CN and address
// as its mailto value — the shape appendOrganizerProp (the CalDAV/Calendar
// file form) and overrideInvitationOrganizer (the Invitation's own form,
// ADR-0059) share, differing only in which address they carry.
func organizerProp(name, address string) *ical.Prop {
	prop := ical.NewProp(ical.PropOrganizer)
	if name != "" {
		prop.Params.Set(ical.ParamCommonName, name)
	}
	prop.Value = "mailto:" + address
	return prop
}

// appendAttendeeProps adds one ATTENDEE per attendees, naming their Response
// as PARTSTAT — attendees.response already stores iCalendar's PARTSTAT
// vocabulary verbatim (ADR-0046), upper-cased here since the column holds it
// lowercase (ADR-0062). No properties at all for an empty attendees, so an
// Event with no Attendees round-trips with no ATTENDEE lines.
func appendAttendeeProps(v *ical.Event, attendees []repository.AttendeeWithName) {
	for _, a := range attendees {
		prop := ical.NewProp(ical.PropAttendee)
		if a.Name != "" {
			prop.Params.Set(ical.ParamCommonName, a.Name)
		}
		prop.Params.Set(ical.ParamParticipationStatus, strings.ToUpper(a.Response))
		prop.Value = "mailto:" + a.Email
		v.Props.Add(prop)
	}
}

// buildVAlarm renders one Reminder as a VALARM: ACTION:DISPLAY for the
// notification Channel, ACTION:EMAIL for the email Channel, and a TRIGGER
// offset from DTSTART (ADR-0020). For an all-day Event the Reminder's offset
// is measured from 09:00 on the start date rather than midnight (DTSTART),
// so the trigger is shifted back by that same 9 hours to compensate.
func buildVAlarm(e repository.Event, reminder repository.Reminder) *ical.Component {
	alarm := ical.NewComponent(ical.CompAlarm)

	action := "DISPLAY"
	if reminder.Channel == "email" {
		action = "EMAIL"
	}
	alarm.Props.SetText(ical.PropAction, action)
	alarm.Props.SetText(ical.PropDescription, e.Title)
	if action == "EMAIL" {
		alarm.Props.SetText(ical.PropSummary, e.Title)
	}

	trigger := -time.Duration(reminder.OffsetMinutes) * time.Minute
	if e.AllDay {
		trigger += allDayReminderAnchorOffset
	}
	triggerProp := ical.NewProp(ical.PropTrigger)
	triggerProp.SetDuration(trigger)
	alarm.Props.Add(triggerProp)

	return alarm
}

// appendTimeProps adds DTSTART, DTEND, and — when recurrenceID is set —
// RECURRENCE-ID to v, all formatted against the same allDay/tzid: the
// DTSTART/DTEND/RECURRENCE-ID trio buildVEvent and CancellationToICal each
// build (#275). This shared allDay/tzid is right for CancellationToICal,
// whose OutboxCancelSnapshot carries a single anchor for all three, but not
// for buildVEvent's Override case, where RECURRENCE-ID must instead match
// its Master's own AllDay/Tzid regardless of what the Override itself
// changed (see buildVEvent's recurrenceIDAnchor) — so buildVEvent calls this
// with recurrenceID nil and builds that property separately.
func appendTimeProps(v *ical.Event, start, end time.Time, allDay bool, tzid *string, recurrenceID *time.Time) error {
	startProp, err := newDateTimeProp(ical.PropDateTimeStart, start, allDay, tzid)
	if err != nil {
		return err
	}
	v.Props.Add(startProp)

	endProp, err := newDateTimeProp(ical.PropDateTimeEnd, end, allDay, tzid)
	if err != nil {
		return err
	}
	v.Props.Add(endProp)

	if recurrenceID != nil {
		prop, err := newDateTimeProp(ical.PropRecurrenceID, *recurrenceID, allDay, tzid)
		if err != nil {
			return fmt.Errorf("build recurrence-id: %w", err)
		}
		v.Props.Add(prop)
	}
	return nil
}

// newDateTimeProp renders t as an all-day DATE, a zoned/absolute DATE-TIME
// (named tzid, or "Etc/UTC"), or a floating DATE-TIME (nil tzid) — the three
// DTSTART forms ADR-0019 defines. A Floating Event's instant is stored as a
// literal wall-clock (no real zone), so it is formatted as-is with no TZID
// param and no "Z" suffix, rather than converted through t's Go Location.
func newDateTimeProp(name string, t time.Time, allDay bool, tzid *string) (*ical.Prop, error) {
	prop := ical.NewProp(name)

	if allDay {
		prop.SetDate(t.UTC())
		return prop, nil
	}

	if tzid == nil {
		prop.SetValueType(ical.ValueDateTime)
		prop.Value = t.UTC().Format("20060102T150405")
		return prop, nil
	}

	loc, err := recurrence.ResolveLocation(tzid)
	if err != nil {
		return nil, err
	}
	prop.SetDateTime(t.In(loc))
	return prop, nil
}
