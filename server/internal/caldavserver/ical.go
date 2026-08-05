// ical.go recomposes a series (a Master plus its Overrides and Exceptions)
// into the single VCALENDAR CalDAV expects for one calendar object
// (ADR-0025), and derives that object's ETag from the reconstruction rather
// than any raw stored bytes (ADR-0026, so PUT/GET ETags never mismatch and
// cause a re-sync loop).
package caldavserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calendar/server/internal/repository"
)

const prodID = "-//calendar//caldavserver//EN"

// allDayReminderAnchorOffset is how far after midnight (an all-day Event's
// stored start) a Reminder's offset is actually measured from — 09:00, so a
// Reminder never fires in the middle of the night (ADR-0020).
const allDayReminderAnchorOffset = 9 * time.Hour

// seriesToICal recomposes master and its overrides (ordered by
// RecurrenceID) into one VCALENDAR: the Master VEVENT (carrying EXDATE for
// each cancelled Occurrence) followed by one VEVENT per Override, all
// sharing master's id as their UID (ADR-0025).
func seriesToICal(master repository.Event, overrides []repository.Event) (*ical.Calendar, error) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, prodID)
	cal.Props.SetText(ical.PropVersion, "2.0")

	masterEvent, err := buildVEvent(master, master.ID, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("build master vevent: %w", err)
	}
	for _, exdate := range master.Exdates {
		prop, err := newDateTimeProp(ical.PropExceptionDates, exdate, master.AllDay, master.Tzid)
		if err != nil {
			return nil, fmt.Errorf("build exdate: %w", err)
		}
		masterEvent.Props.Add(prop)
	}
	cal.Children = append(cal.Children, masterEvent.Component)

	sorted := append([]repository.Event(nil), overrides...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].RecurrenceID.Before(*sorted[j].RecurrenceID)
	})
	for _, override := range sorted {
		overrideEvent, err := buildVEvent(override, master.ID, override.RecurrenceID, &master)
		if err != nil {
			return nil, fmt.Errorf("build override vevent: %w", err)
		}
		cal.Children = append(cal.Children, overrideEvent.Component)
	}

	return cal, nil
}

// buildVEvent renders one Event row (a Master or an Override) as a VEVENT.
// recurrenceID is nil for a Master. recurrenceIDAnchor is the Master whose
// rrule generated recurrenceID — its AllDay/Tzid define how RECURRENCE-ID is
// formatted, since it must match the original Occurrence the rule produced,
// independent of anything the Override itself changed.
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

	startProp, err := newDateTimeProp(ical.PropDateTimeStart, e.Start, e.AllDay, e.Tzid)
	if err != nil {
		return nil, err
	}
	v.Props.Add(startProp)
	endProp, err := newDateTimeProp(ical.PropDateTimeEnd, e.End, e.AllDay, e.Tzid)
	if err != nil {
		return nil, err
	}
	v.Props.Add(endProp)

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

	for _, reminder := range e.Reminders {
		v.Children = append(v.Children, buildVAlarm(e, reminder))
	}

	return v, nil
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

	loc, err := resolveLocation(tzid)
	if err != nil {
		return nil, err
	}
	prop.SetDateTime(t.In(loc))
	return prop, nil
}

// calendarETag derives a calendar object's ETag from its normalized
// reconstruction (ADR-0026): a hash of the exact bytes GetCalendarObject
// serves, never the client's raw PUT bytes, so a PUT's response ETag always
// matches the next GET and clients never loop re-fetching a "changed"
// object.
func calendarETag(cal *ical.Calendar) (string, error) {
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return "", fmt.Errorf("encode calendar: %w", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:]), nil
}
