package caldavserver

import (
	"fmt"
	"time"

	"github.com/teambition/rrule-go"
)

// resolveLocation maps an Event's tzid anchor (ADR-0019) onto a
// *time.Location for RRULE expansion: a named IANA zone expands in that
// zone, "Etc/UTC" expands in UTC, and nil (a Floating Event) also expands in
// UTC — the server has no Viewer zone to expand in, and floating instants are
// stored as literal wall-clock components regardless of Location (see
// setDateTimeProp).
func resolveLocation(tzid *string) (*time.Location, error) {
	if tzid == nil {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(*tzid)
	if err != nil {
		return nil, fmt.Errorf("load location %q: %w", *tzid, err)
	}
	return loc, nil
}

// expandOccurrences returns rruleStr's Occurrence starts that fall in the
// half-open window [from, to), expanded in tzid's zone so wall-clock
// semantics (and DST transitions) match the frontend's own expansion
// (ADR-0016). dtstart is the series' first Occurrence start (a UTC instant,
// as stored). An empty rruleStr means the Event does not recur: it has at
// most one Occurrence, dtstart itself, reported only if it falls in the
// window.
//
// This is the query-time expander CalendarQuery's time-range filter drives
// (ADR-0025's #64 acceptance criteria) — it must stay in sync with the
// frontend's rrule.js expansion (ADR-0016) and the firing engine's own Go
// expander (ADR-0021, internal/reminder), since all three are expected to
// agree on which Occurrences exist.
func expandOccurrences(rruleStr string, tzid *string, dtstart, from, to time.Time) ([]time.Time, error) {
	if rruleStr == "" {
		if !dtstart.Before(from) && dtstart.Before(to) {
			return []time.Time{dtstart}, nil
		}
		return nil, nil
	}

	loc, err := resolveLocation(tzid)
	if err != nil {
		return nil, err
	}

	opt, err := rrule.StrToROptionInLocation(rruleStr, loc)
	if err != nil {
		return nil, fmt.Errorf("parse rrule %q: %w", rruleStr, err)
	}
	opt.Dtstart = dtstart.In(loc)

	rule, err := rrule.NewRRule(*opt)
	if err != nil {
		return nil, fmt.Errorf("build rrule %q: %w", rruleStr, err)
	}

	// Between's "inc" only controls whether an occurrence exactly on the
	// boundary is included; combined with the half-open [from, to) window
	// this application wants, the upper bound must be excluded manually
	// since rrule-go has no exclusive-end mode.
	occurrences := rule.Between(from.In(loc), to.In(loc), true)

	starts := make([]time.Time, 0, len(occurrences))
	for _, occ := range occurrences {
		utc := occ.UTC()
		if utc.Before(to) {
			starts = append(starts, utc)
		}
	}
	return starts, nil
}
