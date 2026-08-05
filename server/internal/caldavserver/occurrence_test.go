package caldavserver

import (
	"testing"
	"time"
)

// mustTZ loads an IANA zone or fails the test, keeping expandOccurrences
// fixtures readable.
func mustTZ(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %q: %v", name, err)
	}
	return loc
}

func inLoc(t *testing.T, loc *time.Location, year int, month time.Month, day, hour, min int) time.Time {
	t.Helper()
	return time.Date(year, month, day, hour, min, 0, 0, loc)
}

// TestExpandOccurrences is the (rrule, tzid, window) -> expected occurrence
// starts fixture the query-time expander must satisfy, including DST
// behavior (#64 acceptance criteria).
func TestExpandOccurrences(t *testing.T) {
	berlin := mustTZ(t, "Europe/Berlin")
	berlinTZID := "Europe/Berlin"

	tests := []struct {
		name    string
		rrule   string
		tzid    *string
		dtstart time.Time
		from    time.Time
		to      time.Time
		want    []time.Time
	}{
		{
			name:    "non-recurring inside window",
			rrule:   "",
			dtstart: time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC),
			from:    time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			to:      time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			want:    []time.Time{time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)},
		},
		{
			name:    "non-recurring outside window",
			rrule:   "",
			dtstart: time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC),
			from:    time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			to:      time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			want:    nil,
		},
		{
			name:    "weekly floating rule, window excludes end boundary",
			rrule:   "FREQ=WEEKLY;BYDAY=TU",
			dtstart: time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC), // a Tuesday
			from:    time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC),
			to:      time.Date(2026, 3, 17, 9, 0, 0, 0, time.UTC),
			want: []time.Time{
				time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC),
				time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC),
				// Mar 17 sits exactly on the exclusive upper bound.
			},
		},
		{
			name:    "weekly rule anchored in Europe/Berlin holds local wall-clock across the spring DST jump",
			rrule:   "FREQ=WEEKLY;BYDAY=TU",
			tzid:    &berlinTZID,
			dtstart: inLoc(t, berlin, 2026, 3, 24, 9, 0).UTC(), // a Tuesday, before the Mar 29 2026 DST jump
			from:    inLoc(t, berlin, 2026, 3, 24, 0, 0).UTC(),
			to:      inLoc(t, berlin, 2026, 4, 1, 0, 0).UTC(),
			want: []time.Time{
				inLoc(t, berlin, 2026, 3, 24, 9, 0).UTC(), // CET, UTC+1
				inLoc(t, berlin, 2026, 3, 31, 9, 0).UTC(), // CEST, UTC+2 — still local 09:00
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandOccurrences(tt.rrule, tt.tzid, tt.dtstart, tt.from, tt.to)
			if err != nil {
				t.Fatalf("expandOccurrences: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
			for i := range got {
				if !got[i].Equal(tt.want[i]) {
					t.Fatalf("expected %v, got %v", tt.want, got)
				}
			}
		})
	}
}

func TestExpandOccurrences_BerlinDSTOffsetActuallyShifts(t *testing.T) {
	berlin := mustTZ(t, "Europe/Berlin")
	berlinTZID := "Europe/Berlin"

	dtstart := inLoc(t, berlin, 2026, 3, 24, 9, 0).UTC()
	got, err := expandOccurrences("FREQ=WEEKLY;BYDAY=TU", &berlinTZID, dtstart,
		inLoc(t, berlin, 2026, 3, 24, 0, 0).UTC(), inLoc(t, berlin, 2026, 4, 1, 0, 0).UTC())
	if err != nil {
		t.Fatalf("expandOccurrences: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 occurrences, got %d: %v", len(got), got)
	}

	beforeDST := got[0].In(berlin)
	afterDST := got[1].In(berlin)
	if _, offsetBefore := beforeDST.Zone(); offsetBefore != 3600 {
		t.Fatalf("expected CET (UTC+1) before the jump, got offset %d", offsetBefore)
	}
	if _, offsetAfter := afterDST.Zone(); offsetAfter != 7200 {
		t.Fatalf("expected CEST (UTC+2) after the jump, got offset %d", offsetAfter)
	}
	if beforeDST.Hour() != 9 || afterDST.Hour() != 9 {
		t.Fatalf("expected local wall-clock to stay 09:00 across DST, got %v and %v", beforeDST, afterDST)
	}
}
