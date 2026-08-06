// vtimezone.go generates a VTIMEZONE component for a TZID so an exported
// calendar object is self-contained (ADR-0031): CalDAV clients tolerate a
// bare TZID with no VTIMEZONE definition because they carry their own zone
// database, but an arbitrary ICS importer (Outlook desktop especially) does
// not, and RFC 4791 expects the definition to travel with the object.
//
// Go exposes no zone-transition data (Location.tx and the TZ extend string
// are unexported, and no Go library generates VTIMEZONE from IANA data), so
// this probes Location.Zone() across the current year to find offset
// changes and infers a FREQ=YEARLY rule from each one.
package icalendar

import (
	"fmt"
	"time"

	"github.com/emersion/go-ical"
)

// epochDTStart anchors a permanent (non-recurring) observance's DTSTART —
// its exact date is arbitrary since there is no RRULE to align it with.
const epochDTStart = "19700101T000000"

// buildVTimezone generates tzid's VTIMEZONE: one STANDARD plus one DAYLIGHT
// observance for a zone with DST, or a single STANDARD observance for a
// zone without it (including a zone that has since abolished DST — only
// the current year's transitions are probed).
func buildVTimezone(tzid string) (*ical.Component, error) {
	loc, err := time.LoadLocation(tzid)
	if err != nil {
		return nil, fmt.Errorf("load location %q: %w", tzid, err)
	}

	transitions := findYearTransitions(loc, time.Now().UTC().Year())

	vtz := ical.NewComponent(ical.CompTimezone)
	vtz.Props.SetText(ical.PropTimezoneID, tzid)

	if len(transitions) == 0 {
		vtz.Children = append(vtz.Children, staticObservance(loc))
		return vtz, nil
	}

	standard, daylight := observancesFromTransitions(transitions)
	if standard != nil {
		vtz.Children = append(vtz.Children, standard)
	}
	if daylight != nil {
		vtz.Children = append(vtz.Children, daylight)
	}
	return vtz, nil
}

// zoneTransition is one instant at which loc's UTC offset changes.
// dtstartWall is that instant's wall-clock reading in fromOffsetSec — the
// reading immediately before the jump (e.g. "02:00" for a spring-forward
// that jumps from 2am to 3am) — which is the convention RFC 5545's own
// VTIMEZONE examples use for an observance's DTSTART.
type zoneTransition struct {
	dtstartWall   time.Time
	fromOffsetSec int
	toOffsetSec   int
	toName        string
}

// findYearTransitions probes loc's offset once per day across year and
// binary-searches each day boundary where the offset changed down to the
// second, so DST transitions (always on the hour) are located precisely.
func findYearTransitions(loc *time.Location, year int) []zoneTransition {
	end := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)

	var transitions []zoneTransition
	lo := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	_, loOffset := lo.In(loc).Zone()

	for lo.Before(end) {
		hi := lo.Add(24 * time.Hour)
		_, hiOffset := hi.In(loc).Zone()

		if hiOffset != loOffset {
			transitions = append(transitions, bisectTransition(loc, lo, hi, loOffset))
			loOffset = hiOffset
		}
		lo = hi
	}
	return transitions
}

// bisectTransition narrows [lo, hi) — a day known to contain exactly one
// offset change from fromOffsetSec — down to the second, then reports the
// transition at its right edge (the first instant carrying the new offset).
func bisectTransition(loc *time.Location, lo, hi time.Time, fromOffsetSec int) zoneTransition {
	for hi.Sub(lo) > time.Second {
		mid := lo.Add(hi.Sub(lo) / 2)
		if _, midOffset := mid.In(loc).Zone(); midOffset == fromOffsetSec {
			lo = mid
		} else {
			hi = mid
		}
	}
	toName, toOffsetSec := hi.In(loc).Zone()
	// wallClockAt renders hi as a bare "YYYYMMDDTHHMMSS" reading in a fixed
	// offset — used here for fromOffsetSec's reading, the pre-jump wall
	// clock DTSTART is conventionally expressed in.
	dtstartWall := hi.UTC().Add(time.Duration(fromOffsetSec) * time.Second)
	return zoneTransition{dtstartWall: dtstartWall, fromOffsetSec: fromOffsetSec, toOffsetSec: toOffsetSec, toName: toName}
}

// observancesFromTransitions splits year's transitions into a STANDARD and
// a DAYLIGHT observance: whichever of the (at most two) distinct offsets
// observed is larger is DAYLIGHT, matching every real-world zone's
// convention of a positive DST shift. Each observance is built from the
// transition that leads into its offset, since that transition's wall-clock
// time is what recurs every year.
func observancesFromTransitions(transitions []zoneTransition) (standard, daylight *ical.Component) {
	var standardTransition, daylightTransition *zoneTransition
	for i := range transitions {
		t := &transitions[i]
		if standardTransition == nil || t.toOffsetSec < standardTransition.toOffsetSec {
			standardTransition = t
		}
		if daylightTransition == nil || t.toOffsetSec > daylightTransition.toOffsetSec {
			daylightTransition = t
		}
	}
	if standardTransition.toOffsetSec == daylightTransition.toOffsetSec {
		// Only one distinct offset among the transitions found within the
		// year (e.g. a zone whose only change was into permanent standard
		// time) — still just one observance.
		return buildObservance(ical.CompTimezoneStandard, *standardTransition), nil
	}
	return buildObservance(ical.CompTimezoneStandard, *standardTransition),
		buildObservance(ical.CompTimezoneDaylight, *daylightTransition)
}

// buildObservance renders one recurring STANDARD/DAYLIGHT block: its
// TZOFFSETFROM/TZOFFSETTO/TZNAME from t, and a FREQ=YEARLY;BYMONTH=..;
// BYDAY=.. rule inferred from t's DTSTART wall-clock date.
func buildObservance(name string, t zoneTransition) *ical.Component {
	comp := ical.NewComponent(name)
	addDTStart(comp, t.dtstartWall.Format("20060102T150405"))
	addRRule(comp, fmt.Sprintf("FREQ=YEARLY;BYMONTH=%d;BYDAY=%s", int(t.dtstartWall.Month()), byDayRule(t.dtstartWall)))
	addOffsets(comp, formatUTCOffset(t.fromOffsetSec), formatUTCOffset(t.toOffsetSec), t.toName)
	return comp
}

// staticObservance renders the single permanent STANDARD block for a zone
// with no transition in the probed year: TZOFFSETFROM equals TZOFFSETTO,
// and there is no RRULE since the offset never changes.
func staticObservance(loc *time.Location) *ical.Component {
	name, offsetSec := time.Now().In(loc).Zone()
	offset := formatUTCOffset(offsetSec)

	comp := ical.NewComponent(ical.CompTimezoneStandard)
	addDTStart(comp, epochDTStart)
	addOffsets(comp, offset, offset, name)
	return comp
}

// addDTStart sets an observance's DTSTART to the literal "YYYYMMDDTHHMMSS"
// wall-clock reading value — no TZID param, no "Z" suffix, since it is
// itself defining the zone rather than anchored to one.
func addDTStart(comp *ical.Component, value string) {
	dtstart := ical.NewProp(ical.PropDateTimeStart)
	dtstart.SetValueType(ical.ValueDateTime)
	dtstart.Value = value
	comp.Props.Add(dtstart)
}

// addRRule sets an observance's RRULE via a plain Value assignment (not
// SetText, which would backslash-escape RRULE's own ";"-delimited syntax).
func addRRule(comp *ical.Component, value string) {
	rrule := ical.NewProp(ical.PropRecurrenceRule)
	rrule.Value = value
	comp.Props.Set(rrule)
}

// addOffsets sets an observance's TZOFFSETFROM/TZOFFSETTO/TZNAME triple.
func addOffsets(comp *ical.Component, from, to, name string) {
	offsetFrom := ical.NewProp(ical.PropTimezoneOffsetFrom)
	offsetFrom.Value = from
	comp.Props.Add(offsetFrom)

	offsetTo := ical.NewProp(ical.PropTimezoneOffsetTo)
	offsetTo.Value = to
	comp.Props.Add(offsetTo)

	comp.Props.SetText(ical.PropTimezoneName, name)
}

// byDayRule renders t's day-of-month as a BYDAY ordinal-weekday pair
// ("-1SU", "2TU", ...): the ordinal is t's occurrence-of-that-weekday
// within its month, or -1 if it is the last such occurrence — matching how
// DST rules are conventionally expressed (e.g. "last Sunday of March").
func byDayRule(t time.Time) string {
	ordinal := (t.Day()-1)/7 + 1
	daysInMonth := time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if t.Day()+7 > daysInMonth {
		ordinal = -1
	}
	return fmt.Sprintf("%d%s", ordinal, weekdayAbbrev[t.Weekday()])
}

var weekdayAbbrev = [...]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}

// formatUTCOffset renders offsetSec (seconds east of UTC) as RFC 5545's
// "+HHMM"/"-HHMM" (with a trailing "SS" only for the rare non-zero-second
// offset).
func formatUTCOffset(offsetSec int) string {
	sign := "+"
	if offsetSec < 0 {
		sign = "-"
		offsetSec = -offsetSec
	}
	hours := offsetSec / 3600
	minutes := (offsetSec % 3600) / 60
	seconds := offsetSec % 60
	if seconds != 0 {
		return fmt.Sprintf("%s%02d%02d%02d", sign, hours, minutes, seconds)
	}
	return fmt.Sprintf("%s%02d%02d", sign, hours, minutes)
}
