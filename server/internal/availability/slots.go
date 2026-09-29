// Package availability derives a Booking Link's bookable slots (#323,
// ADR-0087). DeriveSlots is a pure function — no I/O, no database — that
// mirrors internal/recurrence's own leaf-package shape: the public page
// (#324) will call whatever assembles this package's inputs with no
// Session at all, so the derivation itself must not reach for one either.
package availability

import (
	"fmt"
	"sort"
	"time"
)

// Range is one weekly time range on an Availability Schedule (ADR-0085): a
// weekday (0-6, Sunday-Saturday, matching time.Weekday's own convention)
// plus a start/end minute-of-day pair. Several per weekday are legal, so a
// split day is representable.
type Range struct {
	Weekday     int
	StartMinute int
	EndMinute   int
}

// BusyInterval is one closed window of unavailability, as absolute
// instants — already expanded and assembled by the caller from Busy
// Occurrences and incomplete Tasks' Time blocks (ADR-0087). This package
// performs no expansion of its own; it only tests candidate slots against
// the intervals it's handed.
type BusyInterval struct {
	Start time.Time
	End   time.Time
}

// Params is DeriveSlots' entire input — everything ADR-0087's derivation
// needs, and nothing it fetches itself (#323's "performs no I/O and reads
// no database").
type Params struct {
	// Ranges is the Availability Schedule's weekly pattern.
	Ranges []Range
	// Tzid is the Schedule's own IANA timezone (its own Anchor zone,
	// ADR-0085) — every Range is interpreted in this zone, never the
	// caller's, so a range spanning a DST transition yields the wall-clock
	// hours stated regardless of where Now happens to be.
	Tzid string
	// Busy closes slots it overlaps. Free Events, a Deadline, a completed
	// Task, and an all-day Event that isn't explicitly Busy must never
	// appear here — that filtering is the caller's job (ADR-0087).
	Busy []BusyInterval
	// DurationMinutes is the Booking Link's own Duration — also its slot
	// increment; there is no second increment to configure.
	DurationMinutes int
	// MinimumNoticeMinutes truncates the near edge: no slot may start
	// before Now plus this many minutes.
	MinimumNoticeMinutes int
	// BookingHorizonDays bounds the far edge: no slot may start on or after
	// Now plus this many days.
	BookingHorizonDays int
	// Now is the current instant derivation is measured from.
	Now time.Time
}

// DeriveSlots returns every bookable slot start for a Booking Link, tiled
// at Params.DurationMinutes from the start of each matching weekly Range,
// ascending. Pure: no I/O, no database (#323).
func DeriveSlots(p Params) ([]time.Time, error) {
	if p.DurationMinutes <= 0 {
		return nil, fmt.Errorf("duration must be positive, got %d minutes", p.DurationMinutes)
	}

	loc, err := time.LoadLocation(p.Tzid)
	if err != nil {
		return nil, fmt.Errorf("load location %q: %w", p.Tzid, err)
	}

	duration := time.Duration(p.DurationMinutes) * time.Minute
	earliestAllowed := p.Now.Add(time.Duration(p.MinimumNoticeMinutes) * time.Minute)
	// AddDate, not Add(N*24*time.Hour): a calendar-day horizon must land on
	// the same wall-clock instant it would without any DST transition
	// between Now and it, which only a calendar-aware step guarantees.
	horizonCutoff := p.Now.AddDate(0, 0, p.BookingHorizonDays)

	rangesByWeekday := make(map[int][]Range, len(p.Ranges))
	for _, r := range p.Ranges {
		rangesByWeekday[r.Weekday] = append(rangesByWeekday[r.Weekday], r)
	}

	var slots []time.Time

	nowLocal := p.Now.In(loc)
	// Starts at Now's own calendar day, not the next one: a day already
	// partway gone can still have later Ranges worth checking — minimum
	// notice, not the day boundary, is what actually truncates the near
	// edge.
	day := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, loc)
	for !day.After(horizonCutoff) {
		year, month, dom := day.Date()
		weekday := int(day.Weekday())

		for _, r := range rangesByWeekday[weekday] {
			// Constructed via time.Date directly from the day's own
			// calendar components, never by adding a Duration to another
			// instant — Add is a fixed real-world offset, and a range's
			// start/end are wall-clock quantities that must resolve
			// through the Location's own DST rules (ADR-0085).
			rangeStart := time.Date(year, month, dom, r.StartMinute/60, r.StartMinute%60, 0, 0, loc)
			rangeEnd := time.Date(year, month, dom, r.EndMinute/60, r.EndMinute%60, 0, 0, loc)

			for t := rangeStart; !t.Add(duration).After(rangeEnd); t = t.Add(duration) {
				if t.Before(earliestAllowed) {
					continue
				}
				if !t.Before(horizonCutoff) {
					continue
				}
				if overlapsAny(t, t.Add(duration), p.Busy) {
					continue
				}
				slots = append(slots, t)
			}
		}

		// AddDate, not Add(24*time.Hour): the same calendar-aware step the
		// horizon cutoff above needs, so a DST transition day still steps
		// to the following calendar day rather than 23 or 25 hours later.
		day = day.AddDate(0, 0, 1)
	}

	sort.Slice(slots, func(i, j int) bool { return slots[i].Before(slots[j]) })
	return slots, nil
}

// overlapsAny reports whether [start, end) overlaps any of busy — a
// half-open comparison on both sides, so two intervals meeting exactly at
// a shared instant (an adjacent Busy interval leaving an exact-Duration
// gap) never count as overlapping.
func overlapsAny(start, end time.Time, busy []BusyInterval) bool {
	for _, b := range busy {
		if start.Before(b.End) && b.Start.Before(end) {
			return true
		}
	}
	return false
}
