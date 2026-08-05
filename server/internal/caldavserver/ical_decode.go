// ical_decode.go is seriesToICal's inverse: it decomposes one incoming
// VCALENDAR into the Master/Overrides/Exdates PutCalendarObject writes
// (ADR-0025). Anything it does not recognize — VALARM actions other than
// DISPLAY/EMAIL, end-relative or absolute-datetime TRIGGERs (ADR-0020
// deferred) — is silently dropped rather than rejected, matching ADR-0026's
// "unmodeled data is normalized away, not preserved" decision.
package caldavserver

import (
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calendar/server/internal/repository"
)

// parsedEvent is one decoded VEVENT — a Master or an Override — before it is
// turned into a service.SeriesWrite/OverrideWrite.
type parsedEvent struct {
	Title, Description, Location string
	Start, End                   time.Time
	AllDay                       bool
	Tzid                         *string
	Reminders                    []repository.Reminder
}

// parsedSeries is a whole decomposed calendar object: the Master VEVENT
// (identified by having no RECURRENCE-ID), every Override VEVENT (has one),
// and the Master's EXDATEs.
type parsedSeries struct {
	UID       string
	Rrule     string
	Exdates   []time.Time
	Master    parsedEvent
	Overrides []overrideEvent
}

type overrideEvent struct {
	parsedEvent
	RecurrenceID time.Time
}

// parseCalendarObject decomposes cal into a parsedSeries: exactly one VEVENT
// without RECURRENCE-ID becomes the Master, every VEVENT with one becomes an
// Override, and all must share one UID (ADR-0025).
func parseCalendarObject(cal *ical.Calendar) (*parsedSeries, error) {
	events := cal.Events()
	if len(events) == 0 {
		return nil, fmt.Errorf("calendar object has no VEVENT")
	}

	var uid string
	var master *parsedEvent
	var masterRrule string
	var exdates []time.Time
	var overrides []overrideEvent

	for _, v := range events {
		vUID, err := v.Props.Text(ical.PropUID)
		if err != nil {
			return nil, fmt.Errorf("read uid: %w", err)
		}
		if vUID == "" {
			return nil, fmt.Errorf("VEVENT missing UID")
		}
		if uid == "" {
			uid = vUID
		} else if uid != vUID {
			return nil, fmt.Errorf("VEVENTs in calendar object have mismatched UIDs %q and %q", uid, vUID)
		}

		recurrenceIDProp := v.Props.Get(ical.PropRecurrenceID)
		isOverride := recurrenceIDProp != nil

		pe, rrule, err := parseVEvent(v, isOverride)
		if err != nil {
			return nil, err
		}

		if !isOverride {
			if master != nil {
				return nil, fmt.Errorf("calendar object has more than one master VEVENT")
			}
			m := pe
			master = &m
			masterRrule = rrule

			exdates, err = parseExdates(v)
			if err != nil {
				return nil, err
			}
			continue
		}

		recurrenceID, _, _, err := parseEventTime(recurrenceIDProp)
		if err != nil {
			return nil, fmt.Errorf("parse recurrence-id: %w", err)
		}
		overrides = append(overrides, overrideEvent{parsedEvent: pe, RecurrenceID: recurrenceID})
	}

	if master == nil {
		return nil, fmt.Errorf("calendar object has no master VEVENT (every VEVENT carries RECURRENCE-ID)")
	}

	return &parsedSeries{
		UID:       uid,
		Rrule:     masterRrule,
		Exdates:   exdates,
		Master:    *master,
		Overrides: overrides,
	}, nil
}

// parseVEvent decodes v's shared fields. rrule is only populated for a
// Master (isOverride false) — an Override is never itself a recurring
// series (ADR-0016), so an RRULE on one is unmodeled and dropped.
func parseVEvent(v ical.Event, isOverride bool) (parsedEvent, string, error) {
	startProp := v.Props.Get(ical.PropDateTimeStart)
	if startProp == nil {
		return parsedEvent{}, "", fmt.Errorf("VEVENT missing DTSTART")
	}
	start, allDay, tzid, err := parseEventTime(startProp)
	if err != nil {
		return parsedEvent{}, "", fmt.Errorf("parse dtstart: %w", err)
	}

	end := start
	if endProp := v.Props.Get(ical.PropDateTimeEnd); endProp != nil {
		end, _, _, err = parseEventTime(endProp)
		if err != nil {
			return parsedEvent{}, "", fmt.Errorf("parse dtend: %w", err)
		}
	}

	title, err := v.Props.Text(ical.PropSummary)
	if err != nil {
		return parsedEvent{}, "", fmt.Errorf("parse summary: %w", err)
	}
	description, err := v.Props.Text(ical.PropDescription)
	if err != nil {
		return parsedEvent{}, "", fmt.Errorf("parse description: %w", err)
	}
	location, err := v.Props.Text(ical.PropLocation)
	if err != nil {
		return parsedEvent{}, "", fmt.Errorf("parse location: %w", err)
	}

	var rrule string
	if !isOverride {
		if p := v.Props.Get(ical.PropRecurrenceRule); p != nil {
			rrule = p.Value
		}
	}

	reminders, err := parseVAlarms(v, allDay)
	if err != nil {
		return parsedEvent{}, "", err
	}

	return parsedEvent{
		Title:       title,
		Description: description,
		Location:    location,
		Start:       start,
		End:         end,
		AllDay:      allDay,
		Tzid:        tzid,
		Reminders:   reminders,
	}, rrule, nil
}

// parseEventTime decodes a DTSTART/DTEND/RECURRENCE-ID/EXDATE property into
// the (instant, allDay, tzid) triple newDateTimeProp encodes (ADR-0019): a
// VALUE=DATE property is an all-day date; a TZID param names a zoned
// Event's anchor (including "Etc/UTC" for an absolute instant, since
// newDateTimeProp always writes TZID for a named zone); a bare "Z"-suffixed
// value with no TZID param is an absolute instant our own encoder never
// produces but other clients may send, normalized here to tzid "Etc/UTC";
// anything else is a Floating Event (nil tzid).
func parseEventTime(prop *ical.Prop) (t time.Time, allDay bool, tzid *string, err error) {
	if prop.ValueType() == ical.ValueDate {
		t, err = prop.DateTime(nil)
		return t.UTC(), true, nil, err
	}

	if tz := prop.Params.Get(ical.PropTimezoneID); tz != "" {
		t, err = prop.DateTime(nil)
		if err != nil {
			return time.Time{}, false, nil, err
		}
		return t.UTC(), false, &tz, nil
	}

	t, err = prop.DateTime(nil)
	if err != nil {
		return time.Time{}, false, nil, err
	}
	if strings.HasSuffix(prop.Value, "Z") {
		utcZone := "Etc/UTC"
		return t.UTC(), false, &utcZone, nil
	}
	return t.UTC(), false, nil, nil
}

// parseExdates decodes every EXDATE on a Master VEVENT.
func parseExdates(v ical.Event) ([]time.Time, error) {
	var out []time.Time
	for _, prop := range v.Props.Values(ical.PropExceptionDates) {
		p := prop
		t, _, _, err := parseEventTime(&p)
		if err != nil {
			return nil, fmt.Errorf("parse exdate: %w", err)
		}
		out = append(out, t)
	}
	return out, nil
}

// parseVAlarms decodes v's VALARM children into Reminders (the inverse of
// buildVAlarm). A VALARM this app cannot express as a Reminder — any ACTION
// but DISPLAY/EMAIL, a RELATED=END or absolute-datetime TRIGGER (ADR-0020
// only supports a DTSTART-relative offset) — is skipped rather than
// rejected (ADR-0026).
func parseVAlarms(v ical.Event, allDay bool) ([]repository.Reminder, error) {
	var reminders []repository.Reminder
	for _, child := range v.Children {
		if child.Name != ical.CompAlarm {
			continue
		}

		action, err := child.Props.Text(ical.PropAction)
		if err != nil {
			return nil, fmt.Errorf("parse valarm action: %w", err)
		}
		var channel string
		switch action {
		case "DISPLAY":
			channel = "notification"
		case "EMAIL":
			channel = "email"
		default:
			continue
		}

		triggerProp := child.Props.Get(ical.PropTrigger)
		if triggerProp == nil {
			continue
		}
		if related := triggerProp.Params.Get("RELATED"); related == "END" {
			continue
		}
		trigger, err := triggerProp.Duration()
		if err != nil {
			// An absolute-datetime TRIGGER (VALUE=DATE-TIME) isn't a
			// Duration; unsupported, so drop it rather than fail the PUT.
			continue
		}
		if allDay {
			trigger -= allDayReminderAnchorOffset
		}
		offset := -trigger

		reminders = append(reminders, repository.Reminder{
			OffsetMinutes: int(offset.Minutes()),
			Channel:       channel,
		})
	}
	return reminders, nil
}
