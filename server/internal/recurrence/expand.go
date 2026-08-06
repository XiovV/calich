package recurrence

import (
	"fmt"
	"time"

	"github.com/teambition/rrule-go"
)

// ExpandOccurrences returns rruleStr's Occurrence starts that fall in the
// half-open window [from, to), expanded in tzid's zone so wall-clock
// semantics (and DST transitions) match the frontend's own expansion
// (ADR-0016). dtstart is the series' first Occurrence start (a UTC instant,
// as stored). An empty rruleStr means the Event does not recur: it has at
// most one Occurrence, dtstart itself, reported only if it falls in the
// window.
//
// This is the query-time expander CalDAV's CalendarQuery time-range filter
// drives (ADR-0025's #64 acceptance criteria), ICS export's single-
// Occurrence flatten drives (#76), and EventRepository.ListByUser's
// recurrence-aware windowing drives (#80) — it must stay in sync with the
// frontend's rrule.js expansion (ADR-0016) and the firing engine's own Go
// expander (ADR-0021, internal/reminder), since all three are expected to
// agree on which Occurrences exist.
//
// Lives in its own leaf package (no internal dependencies) rather than
// alongside icalendar's other codec code so EventRepository can import it
// directly without repository -> icalendar -> repository forming a cycle
// (icalendar depends on repository elsewhere, for its Event/Reminder
// shapes).
func ExpandOccurrences(rruleStr string, tzid *string, dtstart, from, to time.Time) ([]time.Time, error) {
	if rruleStr == "" {
		if !dtstart.Before(from) && dtstart.Before(to) {
			return []time.Time{dtstart}, nil
		}
		return nil, nil
	}

	loc, err := ResolveLocation(tzid)
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
