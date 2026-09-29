package availability

import (
	"testing"
	"time"
)

func mustLoadLocation(t *testing.T, tzid string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(tzid)
	if err != nil {
		t.Fatalf("load location %q: %v", tzid, err)
	}
	return loc
}

// formatAll renders slots as "15:04" in loc, for assertions that only care
// about the wall-clock hour, not the date.
func formatAll(slots []time.Time, loc *time.Location) []string {
	out := make([]string, len(slots))
	for i, s := range slots {
		out[i] = s.In(loc).Format("15:04")
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDeriveSlots_EmptySchedule(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	slots, err := DeriveSlots(Params{
		Ranges:               nil,
		Tzid:                 "Etc/UTC",
		DurationMinutes:      30,
		MinimumNoticeMinutes: 0,
		BookingHorizonDays:   7,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("derive slots: %v", err)
	}
	if len(slots) != 0 {
		t.Fatalf("expected no slots for an empty schedule, got %v", slots)
	}
}

func TestDeriveSlots_OneRange(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	weekday := int(now.Weekday())

	slots, err := DeriveSlots(Params{
		Ranges:               []Range{{Weekday: weekday, StartMinute: 9 * 60, EndMinute: 17 * 60}},
		Tzid:                 "Etc/UTC",
		DurationMinutes:      30,
		MinimumNoticeMinutes: 0,
		BookingHorizonDays:   1,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("derive slots: %v", err)
	}
	if len(slots) != 16 {
		t.Fatalf("expected 16 half-hour slots across an 8-hour range, got %d: %v", len(slots), slots)
	}
	if !slots[0].Equal(time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected the first slot at 09:00, got %v", slots[0])
	}
	last := slots[len(slots)-1]
	if !last.Equal(time.Date(2026, 6, 1, 16, 30, 0, 0, time.UTC)) {
		t.Fatalf("expected the last slot at 16:30, got %v", last)
	}
}

// TestDeriveSlots_SeveralRangesOnAWeekday covers a split day, and that
// results are sorted ascending regardless of the input Ranges' own order.
func TestDeriveSlots_SeveralRangesOnAWeekday(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	weekday := int(now.Weekday())

	slots, err := DeriveSlots(Params{
		Ranges: []Range{
			// Afternoon listed first, deliberately, to prove sorting.
			{Weekday: weekday, StartMinute: 14 * 60, EndMinute: 17 * 60},
			{Weekday: weekday, StartMinute: 9 * 60, EndMinute: 12 * 60},
		},
		Tzid:                 "Etc/UTC",
		DurationMinutes:      60,
		MinimumNoticeMinutes: 0,
		BookingHorizonDays:   1,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("derive slots: %v", err)
	}
	got := formatAll(slots, time.UTC)
	want := []string{"09:00", "10:00", "11:00", "14:00", "15:00", "16:00"}
	if !equalStrings(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestDeriveSlots_BusyIntervalInsideARange(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	weekday := int(now.Weekday())

	slots, err := DeriveSlots(Params{
		Ranges: []Range{{Weekday: weekday, StartMinute: 9 * 60, EndMinute: 17 * 60}},
		Busy: []BusyInterval{
			{Start: time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)},
		},
		Tzid:                 "Etc/UTC",
		DurationMinutes:      60,
		MinimumNoticeMinutes: 0,
		BookingHorizonDays:   1,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("derive slots: %v", err)
	}
	got := formatAll(slots, time.UTC)
	want := []string{"09:00", "10:00", "12:00", "13:00", "14:00", "15:00", "16:00"}
	if !equalStrings(got, want) {
		t.Fatalf("expected the 11:00 slot alone removed, got %v", got)
	}
}

func TestDeriveSlots_BusyIntervalStraddlingRangeStart(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	weekday := int(now.Weekday())

	slots, err := DeriveSlots(Params{
		Ranges: []Range{{Weekday: weekday, StartMinute: 9 * 60, EndMinute: 17 * 60}},
		Busy: []BusyInterval{
			{Start: time.Date(2026, 6, 1, 8, 30, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)},
		},
		Tzid:                 "Etc/UTC",
		DurationMinutes:      60,
		MinimumNoticeMinutes: 0,
		BookingHorizonDays:   1,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("derive slots: %v", err)
	}
	got := formatAll(slots, time.UTC)
	want := []string{"10:00", "11:00", "12:00", "13:00", "14:00", "15:00", "16:00"}
	if !equalStrings(got, want) {
		t.Fatalf("expected only the 09:00 slot removed, got %v", got)
	}
}

func TestDeriveSlots_BusyIntervalStraddlingRangeEnd(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	weekday := int(now.Weekday())

	slots, err := DeriveSlots(Params{
		Ranges: []Range{{Weekday: weekday, StartMinute: 9 * 60, EndMinute: 17 * 60}},
		Busy: []BusyInterval{
			{Start: time.Date(2026, 6, 1, 16, 30, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 17, 30, 0, 0, time.UTC)},
		},
		Tzid:                 "Etc/UTC",
		DurationMinutes:      60,
		MinimumNoticeMinutes: 0,
		BookingHorizonDays:   1,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("derive slots: %v", err)
	}
	got := formatAll(slots, time.UTC)
	want := []string{"09:00", "10:00", "11:00", "12:00", "13:00", "14:00", "15:00"}
	if !equalStrings(got, want) {
		t.Fatalf("expected only the 16:00 slot removed, got %v", got)
	}
}

func TestDeriveSlots_BusyIntervalCoveringARangeEntirely(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	weekday := int(now.Weekday())

	slots, err := DeriveSlots(Params{
		Ranges: []Range{{Weekday: weekday, StartMinute: 9 * 60, EndMinute: 17 * 60}},
		Busy: []BusyInterval{
			{Start: time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 18, 0, 0, 0, time.UTC)},
		},
		Tzid:                 "Etc/UTC",
		DurationMinutes:      60,
		MinimumNoticeMinutes: 0,
		BookingHorizonDays:   1,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("derive slots: %v", err)
	}
	if len(slots) != 0 {
		t.Fatalf("expected a Busy interval covering the whole range to remove every slot, got %v", slots)
	}
}

// TestDeriveSlots_AdjacentIntervalsLeaveAnExactDurationGap covers the AC
// line directly: two Busy intervals with an exact-Duration gap between them
// must still leave that gap's one slot bookable.
func TestDeriveSlots_AdjacentIntervalsLeaveAnExactDurationGap(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	weekday := int(now.Weekday())

	slots, err := DeriveSlots(Params{
		Ranges: []Range{{Weekday: weekday, StartMinute: 9 * 60, EndMinute: 17 * 60}},
		Busy: []BusyInterval{
			{Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC)},
			{Start: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)},
		},
		Tzid:                 "Etc/UTC",
		DurationMinutes:      60,
		MinimumNoticeMinutes: 0,
		BookingHorizonDays:   1,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("derive slots: %v", err)
	}
	got := formatAll(slots, time.UTC)
	want := []string{"11:00"}
	if !equalStrings(got, want) {
		t.Fatalf("expected exactly the 11:00 gap slot, got %v", got)
	}
}

// TestDeriveSlots_DurationNotDividingARangeEvenly covers the AC line
// directly: a 50-minute range with a 30-minute Duration fits exactly one
// slot, the leftover 20 minutes going unused rather than producing a
// short slot.
func TestDeriveSlots_DurationNotDividingARangeEvenly(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	weekday := int(now.Weekday())

	slots, err := DeriveSlots(Params{
		Ranges:               []Range{{Weekday: weekday, StartMinute: 9 * 60, EndMinute: 9*60 + 50}},
		Tzid:                 "Etc/UTC",
		DurationMinutes:      30,
		MinimumNoticeMinutes: 0,
		BookingHorizonDays:   1,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("derive slots: %v", err)
	}
	got := formatAll(slots, time.UTC)
	want := []string{"09:00"}
	if !equalStrings(got, want) {
		t.Fatalf("expected exactly one slot with the 20-minute remainder unused, got %v", got)
	}
}

// TestDeriveSlots_MinimumNoticeTruncatesTheNearEdge covers the AC line
// directly.
func TestDeriveSlots_MinimumNoticeTruncatesTheNearEdge(t *testing.T) {
	now := time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)
	weekday := int(now.Weekday())

	slots, err := DeriveSlots(Params{
		Ranges:               []Range{{Weekday: weekday, StartMinute: 9 * 60, EndMinute: 17 * 60}},
		Tzid:                 "Etc/UTC",
		DurationMinutes:      60,
		MinimumNoticeMinutes: 90,
		BookingHorizonDays:   1,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("derive slots: %v", err)
	}
	got := formatAll(slots, time.UTC)
	want := []string{"11:00", "12:00", "13:00", "14:00", "15:00", "16:00"}
	if !equalStrings(got, want) {
		t.Fatalf("expected 09:00 and 10:00 truncated by minimum notice, got %v", got)
	}
}

// TestDeriveSlots_BookingHorizonBoundsTheFarEdge covers the AC line
// directly: a schedule matching every weekday still stops producing slots
// once the horizon cutoff is reached.
func TestDeriveSlots_BookingHorizonBoundsTheFarEdge(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	everyWeekday := make([]Range, 7)
	for i := range everyWeekday {
		everyWeekday[i] = Range{Weekday: i, StartMinute: 9 * 60, EndMinute: 17 * 60}
	}

	slots, err := DeriveSlots(Params{
		Ranges:               everyWeekday,
		Tzid:                 "Etc/UTC",
		DurationMinutes:      60,
		MinimumNoticeMinutes: 0,
		BookingHorizonDays:   1,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("derive slots: %v", err)
	}
	if len(slots) != 8 {
		t.Fatalf("expected only today's 8 slots within a 1-day horizon, got %d: %v", len(slots), slots)
	}
	for _, s := range slots {
		if s.Day() != 1 {
			t.Fatalf("expected every slot on June 1, got one on day %d: %v", s.Day(), s)
		}
	}
}

// TestDeriveSlots_DSTSpringForward covers a range spanning the US
// spring-forward transition (2026-03-08, clocks jump 02:00 -> 03:00): the
// nonexistent 02:00-03:00 hour is skipped, not double-counted or errored,
// and the surviving slots read the wall-clock hours actually stated.
func TestDeriveSlots_DSTSpringForward(t *testing.T) {
	loc := mustLoadLocation(t, "America/New_York")
	now := time.Date(2026, 3, 8, 0, 0, 0, 0, loc)
	weekday := int(now.Weekday())

	slots, err := DeriveSlots(Params{
		Ranges:               []Range{{Weekday: weekday, StartMinute: 60, EndMinute: 240}}, // 01:00-04:00
		Tzid:                 "America/New_York",
		DurationMinutes:      60,
		MinimumNoticeMinutes: 0,
		BookingHorizonDays:   1,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("derive slots: %v", err)
	}
	got := formatAll(slots, loc)
	want := []string{"01:00", "03:00"}
	if !equalStrings(got, want) {
		t.Fatalf("expected the nonexistent 02:00 hour skipped, got %v", got)
	}
}

// TestDeriveSlots_DSTFallBack covers a range spanning the US fall-back
// transition (2026-11-01, clocks repeat 01:00-02:00): the repeated hour
// produces two genuinely distinct slots, an hour of real time apart, even
// though both display the same wall-clock label.
func TestDeriveSlots_DSTFallBack(t *testing.T) {
	loc := mustLoadLocation(t, "America/New_York")
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
	weekday := int(now.Weekday())

	slots, err := DeriveSlots(Params{
		Ranges:               []Range{{Weekday: weekday, StartMinute: 0, EndMinute: 180}}, // 00:00-03:00
		Tzid:                 "America/New_York",
		DurationMinutes:      60,
		MinimumNoticeMinutes: 0,
		BookingHorizonDays:   1,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("derive slots: %v", err)
	}
	got := formatAll(slots, loc)
	want := []string{"00:00", "01:00", "01:00", "02:00"}
	if !equalStrings(got, want) {
		t.Fatalf("expected the repeated 01:00 hour to produce two slots, got %v", got)
	}
	if len(slots) != 4 {
		t.Fatalf("expected 4 slots (a 25-hour day at 60-minute slots), got %d", len(slots))
	}
	_, firstOffset := slots[1].Zone()
	_, secondOffset := slots[2].Zone()
	if firstOffset == secondOffset {
		t.Fatalf("expected the two 01:00 slots to carry different UTC offsets (first pass EDT, second EST), got %d both", firstOffset)
	}
}

// TestDeriveSlots_ScheduleZoneDiffersFromCallers covers the AC line
// directly: Now is given as an instant that is already the next calendar
// day in UTC terms, but still the previous day in the Schedule's own zone
// (Pacific/Honolulu, UTC-10, no DST) — the Range on that still-current
// local day must be found by stepping forward from the Schedule's own
// "today", not from Now's day in whatever zone happened to construct it.
func TestDeriveSlots_ScheduleZoneDiffersFromCallers(t *testing.T) {
	loc := mustLoadLocation(t, "Pacific/Honolulu")
	// 2026-06-02 05:00 UTC is 2026-06-01 19:00 in Honolulu (Monday there,
	// Tuesday in UTC) — verified against Go's own tzdata.
	now := time.Date(2026, 6, 2, 5, 0, 0, 0, time.UTC)

	slots, err := DeriveSlots(Params{
		Ranges:               []Range{{Weekday: int(time.Monday), StartMinute: 20 * 60, EndMinute: 21 * 60}},
		Tzid:                 "Pacific/Honolulu",
		DurationMinutes:      60,
		MinimumNoticeMinutes: 0,
		BookingHorizonDays:   2,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("derive slots: %v", err)
	}
	if len(slots) != 1 {
		t.Fatalf("expected exactly one slot on the Schedule zone's own current day, got %v", slots)
	}
	want := time.Date(2026, 6, 1, 20, 0, 0, 0, loc)
	if !slots[0].Equal(want) {
		t.Fatalf("expected the slot at %v (Honolulu's own Monday), got %v", want, slots[0])
	}
}

func TestDeriveSlots_RejectsNonPositiveDuration(t *testing.T) {
	_, err := DeriveSlots(Params{
		Tzid:               "Etc/UTC",
		DurationMinutes:    0,
		BookingHorizonDays: 1,
		Now:                time.Now(),
	})
	if err == nil {
		t.Fatalf("expected an error for a non-positive duration")
	}
}

func TestDeriveSlots_RejectsInvalidTimezone(t *testing.T) {
	_, err := DeriveSlots(Params{
		Tzid:               "Not/AZone",
		DurationMinutes:    30,
		BookingHorizonDays: 1,
		Now:                time.Now(),
	})
	if err == nil {
		t.Fatalf("expected an error for an invalid timezone")
	}
}
