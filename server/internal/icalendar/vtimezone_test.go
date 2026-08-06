package icalendar

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/repository"
)

func meetingWithTzid(tzid string) repository.Event {
	return repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Tzid:      &tzid,
		Start:     time.Date(2026, 7, 1, 13, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 14, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestSeriesToICal_NorthernDSTZone_EmitsStandardAndDaylight(t *testing.T) {
	body := mustEncode(t, meetingWithTzid("Europe/Berlin"), nil)

	if strings.Count(body, "BEGIN:VTIMEZONE") != 1 {
		t.Fatalf("expected exactly one VTIMEZONE, got:\n%s", body)
	}
	if !strings.Contains(body, "TZID:Europe/Berlin") {
		t.Fatalf("expected TZID:Europe/Berlin, got:\n%s", body)
	}
	if !strings.Contains(body, "BEGIN:DAYLIGHT") {
		t.Fatalf("expected a DAYLIGHT observance, got:\n%s", body)
	}
	if !strings.Contains(body, "BEGIN:STANDARD") {
		t.Fatalf("expected a STANDARD observance, got:\n%s", body)
	}
	// Central European Summer Time starts the last Sunday of March.
	if !strings.Contains(body, "RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU") {
		t.Fatalf("expected the DST-start rule (last Sunday of March), got:\n%s", body)
	}
	// Central European (standard) Time resumes the last Sunday of October.
	if !strings.Contains(body, "RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU") {
		t.Fatalf("expected the standard-time-resumes rule (last Sunday of October), got:\n%s", body)
	}
	if !strings.Contains(body, "TZNAME:CEST") || !strings.Contains(body, "TZNAME:CET") {
		t.Fatalf("expected both CEST and CET TZNAMEs, got:\n%s", body)
	}
	if !strings.Contains(body, "TZOFFSETTO:+0200") || !strings.Contains(body, "TZOFFSETTO:+0100") {
		t.Fatalf("expected TZOFFSETTO +0200 (summer) and +0100 (winter), got:\n%s", body)
	}
	// DTSTART is conventionally the pre-jump wall-clock reading (RFC 5545's
	// own VTIMEZONE examples use this convention, e.g.
	// "DTSTART:19700405T020000" with "TZOFFSETFROM:-0500"): Berlin's spring
	// jump is CET 02:00 -> CEST 03:00, so DAYLIGHT's DTSTART reads 02:00 (the
	// CET reading); the autumn jump is CEST 03:00 -> CET 02:00, so
	// STANDARD's DTSTART reads 03:00 (the CEST reading).
	if !regexp.MustCompile(`DTSTART:\d{8}T020000`).MatchString(body) {
		t.Fatalf("expected a DTSTART reading 02:00 (DAYLIGHT's pre-jump CET wall clock), got:\n%s", body)
	}
	if !regexp.MustCompile(`DTSTART:\d{8}T030000`).MatchString(body) {
		t.Fatalf("expected a DTSTART reading 03:00 (STANDARD's pre-jump CEST wall clock), got:\n%s", body)
	}
}

func TestSeriesToICal_SouthernDSTZone_EmitsStandardAndDaylight(t *testing.T) {
	body := mustEncode(t, meetingWithTzid("Australia/Sydney"), nil)

	if !strings.Contains(body, "TZID:Australia/Sydney") {
		t.Fatalf("expected TZID:Australia/Sydney, got:\n%s", body)
	}
	// Australian DST starts the first Sunday of October and ends the first
	// Sunday of April — the reverse seasonal placement of the northern
	// hemisphere case, so the generator must not assume DST always starts
	// earlier in the year than standard time does.
	if !strings.Contains(body, "RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=1SU") {
		t.Fatalf("expected the DST-start rule (first Sunday of October), got:\n%s", body)
	}
	if !strings.Contains(body, "RRULE:FREQ=YEARLY;BYMONTH=4;BYDAY=1SU") {
		t.Fatalf("expected the standard-time-resumes rule (first Sunday of April), got:\n%s", body)
	}
	if !strings.Contains(body, "TZNAME:AEDT") || !strings.Contains(body, "TZNAME:AEST") {
		t.Fatalf("expected both AEDT and AEST TZNAMEs, got:\n%s", body)
	}
	if !strings.Contains(body, "TZOFFSETTO:+1100") || !strings.Contains(body, "TZOFFSETTO:+1000") {
		t.Fatalf("expected TZOFFSETTO +1100 (DST) and +1000 (standard), got:\n%s", body)
	}
}

func TestSeriesToICal_NoDSTNamedZone_EmitsSingleStandardObservance(t *testing.T) {
	body := mustEncode(t, meetingWithTzid("Asia/Kolkata"), nil)

	if strings.Count(body, "BEGIN:VTIMEZONE") != 1 {
		t.Fatalf("expected exactly one VTIMEZONE, got:\n%s", body)
	}
	if strings.Contains(body, "BEGIN:DAYLIGHT") {
		t.Fatalf("expected no DAYLIGHT observance for a zone with no DST, got:\n%s", body)
	}
	if strings.Count(body, "BEGIN:STANDARD") != 1 {
		t.Fatalf("expected exactly one STANDARD observance, got:\n%s", body)
	}
	if !strings.Contains(body, "TZNAME:IST") {
		t.Fatalf("expected TZNAME:IST, got:\n%s", body)
	}
	if !strings.Contains(body, "TZOFFSETFROM:+0530") || !strings.Contains(body, "TZOFFSETTO:+0530") {
		t.Fatalf("expected a constant +0530 offset, got:\n%s", body)
	}
	if strings.Contains(body, "RRULE") {
		t.Fatalf("expected no RRULE on a permanent single observance, got:\n%s", body)
	}
}

func TestSeriesToICal_EtcUTC_EmitsSingleZeroOffsetObservance(t *testing.T) {
	body := mustEncode(t, meetingWithTzid("Etc/UTC"), nil)

	if strings.Count(body, "BEGIN:VTIMEZONE") != 1 {
		t.Fatalf("expected exactly one VTIMEZONE, got:\n%s", body)
	}
	if !strings.Contains(body, "TZOFFSETFROM:+0000") || !strings.Contains(body, "TZOFFSETTO:+0000") {
		t.Fatalf("expected a constant +0000 offset, got:\n%s", body)
	}
}

func TestSeriesToICal_ZoneThatAbolishedDST_EmitsSingleStandardObservance(t *testing.T) {
	// Europe/Moscow has observed permanent standard time since 2014, so the
	// current year carries no transition at all — the same code path as any
	// other no-DST zone.
	body := mustEncode(t, meetingWithTzid("Europe/Moscow"), nil)

	if strings.Contains(body, "BEGIN:DAYLIGHT") {
		t.Fatalf("expected no DAYLIGHT observance, got:\n%s", body)
	}
	if strings.Count(body, "BEGIN:STANDARD") != 1 {
		t.Fatalf("expected exactly one STANDARD observance, got:\n%s", body)
	}
	if !strings.Contains(body, "TZNAME:MSK") {
		t.Fatalf("expected TZNAME:MSK, got:\n%s", body)
	}
	if !strings.Contains(body, "TZOFFSETFROM:+0300") || !strings.Contains(body, "TZOFFSETTO:+0300") {
		t.Fatalf("expected a constant +0300 offset, got:\n%s", body)
	}
}

func TestSeriesToICal_FloatingEvent_EmitsNoVTimezone(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncode(t, master, nil)

	if strings.Contains(body, "BEGIN:VTIMEZONE") {
		t.Fatalf("expected no VTIMEZONE for a floating (nil tzid) event, got:\n%s", body)
	}
}

func TestSeriesToICal_MasterAndOverrideShareTzid_EmitsOneVTimezone(t *testing.T) {
	tzid := "Europe/Berlin"
	recurrenceID := time.Date(2026, 6, 9, 9, 0, 0, 0, time.UTC)
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Tzid:      &tzid,
		Start:     time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC),
		Rrule:     "FREQ=WEEKLY;BYDAY=TU",
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}
	parentID := "evt-1"
	override := repository.Event{
		ID:           "evt-2",
		ParentID:     &parentID,
		RecurrenceID: &recurrenceID,
		Tzid:         &tzid,
		Title:        "Standup (moved)",
		Start:        time.Date(2026, 6, 9, 11, 0, 0, 0, time.UTC),
		End:          time.Date(2026, 6, 9, 11, 30, 0, 0, time.UTC),
		CreatedAt:    time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncode(t, master, []repository.Event{override})

	if strings.Count(body, "BEGIN:VTIMEZONE") != 1 {
		t.Fatalf("expected exactly one VTIMEZONE shared by master and override, got:\n%s", body)
	}
}
