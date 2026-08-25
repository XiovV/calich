// series.go recomposes a series (a Master plus its Overrides and Exceptions)
// into the single VCALENDAR CalDAV expects for one calendar object
// (ADR-0025).
package icalendar

import (
	"fmt"
	"sort"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calich/server/internal/repository"
)

// ProdID is the PRODID emitted on every VCALENDAR this app produces,
// including the ICS import/export files (ADR-0031).
const ProdID = "-//calendar//caldavserver//EN"

// SeriesToICal recomposes master and its overrides into one VCALENDAR: a
// VTIMEZONE for each distinct TZID the series references, then the Master
// VEVENT (carrying EXDATE for each cancelled Occurrence) followed by one
// VEVENT per Override ordered by RecurrenceID, all sharing master's id as
// their UID (ADR-0025). target picks how master.Attachments are rendered as
// ATTACH — see appendSeriesVEvents. The returned OmittedAttachments are what
// target could not carry (#217).
func SeriesToICal(master repository.Event, overrides []repository.Event, target SerializationTarget) (*ical.Calendar, []OmittedAttachment, error) {
	cal := newVCalendar()

	for _, tzid := range seriesTzids(master, overrides) {
		vtz, err := buildVTimezone(tzid)
		if err != nil {
			return nil, nil, fmt.Errorf("build vtimezone %q: %w", tzid, err)
		}
		cal.Children = append(cal.Children, vtz)
	}

	omitted, err := appendSeriesVEvents(cal, master, overrides, target)
	if err != nil {
		return nil, nil, err
	}
	return cal, omitted, nil
}

// newVCalendar starts a VCALENDAR carrying this app's PRODID and VERSION —
// the two properties every VCALENDAR this package emits shares.
func newVCalendar() *ical.Calendar {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, ProdID)
	cal.Props.SetText(ical.PropVersion, "2.0")
	return cal
}

// appendSeriesVEvents appends master's VEVENT (with its EXDATEs) followed by
// one VEVENT per override (sorted by RecurrenceID) to cal.Children — the
// per-series VEVENT-building step SeriesToICal and CalendarToICal both need,
// the latter once per series in a Calendar. target's ATTACH rendering (see
// appendAttachProps) is added to every VEVENT emitted, master and every
// override alike — an Attachment belongs to the whole series, never to one
// Occurrence (ADR-0040). The omissions returned are the Master pass's alone:
// every VEVENT renders the same master.Attachments, so one Attachment the
// target cannot carry is one thing the file lost, not one per VEVENT.
func appendSeriesVEvents(cal *ical.Calendar, master repository.Event, overrides []repository.Event, target SerializationTarget) ([]OmittedAttachment, error) {
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
	omitted, err := appendAttachProps(masterEvent, master, target)
	if err != nil {
		return nil, fmt.Errorf("append master attachments: %w", err)
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
		if _, err := appendAttachProps(overrideEvent, master, target); err != nil {
			return nil, fmt.Errorf("append override attachments: %w", err)
		}
		cal.Children = append(cal.Children, overrideEvent.Component)
	}
	return omitted, nil
}

// seriesTzids returns the distinct, sorted TZIDs master and overrides
// anchor to (a Floating Event's nil Tzid contributes none), so SeriesToICal
// emits exactly one VTIMEZONE per zone actually referenced, in a
// deterministic order.
func seriesTzids(master repository.Event, overrides []repository.Event) []string {
	seen := make(map[string]struct{})
	if master.Tzid != nil {
		seen[*master.Tzid] = struct{}{}
	}
	for _, o := range overrides {
		if o.Tzid != nil {
			seen[*o.Tzid] = struct{}{}
		}
	}

	tzids := make([]string, 0, len(seen))
	for tzid := range seen {
		tzids = append(tzids, tzid)
	}
	sort.Strings(tzids)
	return tzids
}

// CalendarToICal recomposes every series in a Calendar into one VCALENDAR:
// a VTIMEZONE for each distinct TZID referenced across every series, then
// each Master's VEVENT (with its Overrides), in the same shape SeriesToICal
// produces for one series. name and color are carried as X-WR-CALNAME/
// X-APPLE-CALENDAR-COLOR so a Calendar's identity survives the round trip
// (#76) — masters need not be pre-sorted; the output orders them by ID for
// a deterministic file. Every export call site passes a CalendarFileTarget
// (ADR-0041); CalendarToICal is never used to serve CalDAV. The returned
// OmittedAttachments span every series, in the same order the file is
// written in (#217).
func CalendarToICal(name, color string, masters []repository.Event, overridesByParent map[string][]repository.Event, target SerializationTarget) (*ical.Calendar, []OmittedAttachment, error) {
	cal := newVCalendar()
	setRawText(cal.Props, propWRCalName, name)
	if color != "" {
		setRawText(cal.Props, propAppleCalendarColor, color)
	}

	for _, tzid := range calendarTzids(masters, overridesByParent) {
		vtz, err := buildVTimezone(tzid)
		if err != nil {
			return nil, nil, fmt.Errorf("build vtimezone %q: %w", tzid, err)
		}
		cal.Children = append(cal.Children, vtz)
	}

	sorted := append([]repository.Event(nil), masters...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	var omitted []OmittedAttachment
	for _, master := range sorted {
		seriesOmitted, err := appendSeriesVEvents(cal, master, overridesByParent[master.ID], target)
		if err != nil {
			return nil, nil, err
		}
		omitted = append(omitted, seriesOmitted...)
	}
	return cal, omitted, nil
}

// calendarTzids is seriesTzids generalized across every series in a
// Calendar, so CalendarToICal emits exactly one VTIMEZONE per zone actually
// referenced anywhere in it.
func calendarTzids(masters []repository.Event, overridesByParent map[string][]repository.Event) []string {
	seen := make(map[string]struct{})
	for _, master := range masters {
		for _, tzid := range seriesTzids(master, overridesByParent[master.ID]) {
			seen[tzid] = struct{}{}
		}
	}

	tzids := make([]string, 0, len(seen))
	for tzid := range seen {
		tzids = append(tzids, tzid)
	}
	sort.Strings(tzids)
	return tzids
}

// OccurrenceToICal renders one flattened Occurrence — e's Start/End already
// concrete, its Rrule already cleared by the caller (service.GetOccurrence)
// — as a standalone VCALENDAR carrying a single VEVENT: uid (a fresh id,
// never the series' own UID) with no RRULE and no RECURRENCE-ID. Emitting
// the series' UID with a RECURRENCE-ID instead would describe an orphan
// detached instance, which is both a bad shape for a recipient client and
// the exact shape this app's own ICS importer rejects (#76).
//
// target renders e.Attachments as ATTACH exactly as it does for a whole
// series: a Calendar file describing one Occurrence is no less standalone
// than one describing a series, so it inlines the bytes too (#217,
// ADR-0041). That an Attachment belongs to the series rather than to any one
// Occurrence (ADR-0040) governs where it is stored and edited, not what a
// file exported from it carries.
func OccurrenceToICal(uid string, e repository.Event, target SerializationTarget) (*ical.Calendar, []OmittedAttachment, error) {
	cal := newVCalendar()

	if e.Tzid != nil {
		vtz, err := buildVTimezone(*e.Tzid)
		if err != nil {
			return nil, nil, fmt.Errorf("build vtimezone %q: %w", *e.Tzid, err)
		}
		cal.Children = append(cal.Children, vtz)
	}

	v, err := buildVEvent(e, uid, nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build occurrence vevent: %w", err)
	}
	omitted, err := appendAttachProps(v, e, target)
	if err != nil {
		return nil, nil, fmt.Errorf("append occurrence attachments: %w", err)
	}
	cal.Children = append(cal.Children, v.Component)
	return cal, omitted, nil
}
