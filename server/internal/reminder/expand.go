package reminder

import (
	"fmt"
	"time"

	gorrule "github.com/teambition/rrule-go"

	"github.com/XiovV/calendar/server/internal/repository"
)

// anchorLocation is the zone an Event's recurrence expands in — its own
// Anchor zone (a named IANA zone or "Etc/UTC", ADR-0019) — or UTC for a
// Floating Event. Unlike the frontend, the server has no Viewer zone to fall
// back to for a Floating Event; UTC is the best available default for a
// background process (ADR-0021).
func anchorLocation(tzid *string) *time.Location {
	if tzid == nil {
		return time.UTC
	}
	loc, err := time.LoadLocation(*tzid)
	if err != nil {
		return time.UTC
	}
	return loc
}

// toFloating reinterprets instant's wall-clock in loc as a UTC instant with
// the same field values. rrule-go's date arithmetic is UTC-only, so this
// "floating frame" trick — mirroring the frontend's floatingTime.ts
// (ADR-0019) — is what keeps a zoned rule's wall-clock stable across a DST
// transition (a 09:00 series stays 09:00) instead of drifting by the offset
// change.
func toFloating(instant time.Time, loc *time.Location) time.Time {
	t := instant.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
}

// fromFloating is toFloating's inverse: floating's fields, reinterpreted as
// a wall-clock in loc and resolved to the true UTC instant it names. Go's
// time.Date normalizes a nonexistent wall-clock (the spring-forward gap)
// forward and resolves an ambiguous one (the fall-back overlap) to the
// pre-transition offset — approximately matching the frontend's date-fns-tz
// defaults (ADR-0019); exact DST-edge parity across the two is not
// guaranteed.
func fromFloating(floating time.Time, loc *time.Location) time.Time {
	return time.Date(
		floating.Year(), floating.Month(), floating.Day(),
		floating.Hour(), floating.Minute(), floating.Second(), 0,
		loc,
	)
}

// occurrenceStarts returns event's Occurrence starts that fall in the
// closed-open window [windowStart, windowEnd), skipping any exdate
// (Exception, ADR-0016). A non-recurring Event is a series of one. Callers
// needing exact boundary semantics on the *trigger* instant (rather than the
// occurrence start) pad this window and prune precisely themselves (see
// Due) — this function only needs to return a superset.
func occurrenceStarts(event repository.Event, windowStart, windowEnd time.Time) ([]time.Time, error) {
	if event.Rrule == "" {
		if !event.Start.Before(windowStart) && event.Start.Before(windowEnd) {
			return []time.Time{event.Start}, nil
		}
		return nil, nil
	}

	loc := anchorLocation(event.Tzid)
	option, err := gorrule.StrToROptionInLocation(event.Rrule, time.UTC)
	if err != nil {
		return nil, fmt.Errorf("parse rrule: %w", err)
	}
	option.Dtstart = toFloating(event.Start, loc)
	rule, err := gorrule.NewRRule(*option)
	if err != nil {
		return nil, fmt.Errorf("build rrule: %w", err)
	}

	floatingStarts := rule.Between(toFloating(windowStart, loc), toFloating(windowEnd, loc), true)

	exdateKeys := make(map[int64]struct{}, len(event.Exdates))
	for _, exdate := range event.Exdates {
		exdateKeys[exdate.UTC().Unix()] = struct{}{}
	}

	starts := make([]time.Time, 0, len(floatingStarts))
	for _, floating := range floatingStarts {
		start := fromFloating(floating, loc)
		if start.Before(windowStart) || !start.Before(windowEnd) {
			continue
		}
		if _, excluded := exdateKeys[start.UTC().Unix()]; excluded {
			continue
		}
		starts = append(starts, start)
	}
	return starts, nil
}
