package icalendar

import (
	"strings"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/repository"
)

func mustEncodeCalendar(t *testing.T, name, color string, masters []repository.Event, overridesByParent map[string][]repository.Event) string {
	t.Helper()
	cal, err := CalendarToICal(name, color, masters, overridesByParent, SerializationTarget{})
	if err != nil {
		t.Fatalf("CalendarToICal: %v", err)
	}
	body, err := Encode(cal)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return string(body)
}

func TestCalendarToICal_EmitsNameAndColor(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncodeCalendar(t, "Personal", "#12809CFF", []repository.Event{master}, nil)

	if !strings.Contains(body, "X-WR-CALNAME:Personal") {
		t.Fatalf("expected X-WR-CALNAME:Personal, got:\n%s", body)
	}
	if !strings.Contains(body, "X-APPLE-CALENDAR-COLOR:#12809CFF") {
		t.Fatalf("expected X-APPLE-CALENDAR-COLOR:#12809CFF, got:\n%s", body)
	}
	if !strings.Contains(body, "PRODID:"+ProdID) {
		t.Fatalf("expected the unchanged PRODID, got:\n%s", body)
	}
}

func TestCalendarToICal_OmitsColorPropertyWhenEmpty(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncodeCalendar(t, "Personal", "", []repository.Event{master}, nil)

	if strings.Contains(body, "X-APPLE-CALENDAR-COLOR") {
		t.Fatalf("expected no X-APPLE-CALENDAR-COLOR when color is empty, got:\n%s", body)
	}
}

func TestCalendarToICal_EmitsEverySeriesInTheCalendar(t *testing.T) {
	recurrenceID := time.Date(2026, 6, 9, 9, 0, 0, 0, time.UTC)
	parentID := "evt-1"
	master1 := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Rrule:     "FREQ=WEEKLY;BYDAY=TU",
		Start:     time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}
	override := repository.Event{
		ID:           "evt-1-override",
		ParentID:     &parentID,
		RecurrenceID: &recurrenceID,
		Title:        "Standup (moved)",
		Start:        time.Date(2026, 6, 9, 11, 0, 0, 0, time.UTC),
		End:          time.Date(2026, 6, 9, 11, 30, 0, 0, time.UTC),
		CreatedAt:    time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC),
	}
	master2 := repository.Event{
		ID:        "evt-2",
		Title:     "Retro",
		Start:     time.Date(2026, 6, 3, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 3, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncodeCalendar(t, "Personal", "#12809CFF",
		[]repository.Event{master1, master2},
		map[string][]repository.Event{"evt-1": {override}})

	if strings.Count(body, "BEGIN:VEVENT") != 3 {
		t.Fatalf("expected 3 VEVENTs (2 masters + 1 override), got:\n%s", body)
	}
	if !strings.Contains(body, "SUMMARY:Retro") || !strings.Contains(body, "SUMMARY:Standup (moved)") {
		t.Fatalf("expected both series' VEVENTs, got:\n%s", body)
	}
}

func TestCalendarToICal_SharedTzidAcrossSeriesEmitsOneVTimezone(t *testing.T) {
	tzid := "Europe/Berlin"
	master1 := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Tzid:      &tzid,
		Start:     time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}
	master2 := repository.Event{
		ID:        "evt-2",
		Title:     "Retro",
		Tzid:      &tzid,
		Start:     time.Date(2026, 6, 3, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 3, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncodeCalendar(t, "Personal", "#12809CFF", []repository.Event{master1, master2}, nil)

	if strings.Count(body, "BEGIN:VTIMEZONE") != 1 {
		t.Fatalf("expected exactly one shared VTIMEZONE, got:\n%s", body)
	}
}

func TestCalendarToICal_EmptyCalendarHasNoChildren(t *testing.T) {
	cal, err := CalendarToICal("Work", "#12809CFF", nil, nil, SerializationTarget{})
	if err != nil {
		t.Fatalf("CalendarToICal: %v", err)
	}

	if len(cal.Children) != 0 {
		t.Fatalf("expected no children for an empty calendar, got %d", len(cal.Children))
	}

	if _, err := Encode(cal); err == nil {
		t.Fatalf("expected go-ical's Encode to reject an empty calendar")
	}
}

func TestEncodeEmpty_RendersNameAndColorWithNoVEvents(t *testing.T) {
	cal, err := CalendarToICal("Work", "#12809CFF", nil, nil, SerializationTarget{})
	if err != nil {
		t.Fatalf("CalendarToICal: %v", err)
	}

	body, err := EncodeEmpty(cal)
	if err != nil {
		t.Fatalf("EncodeEmpty: %v", err)
	}

	got := string(body)
	if !strings.Contains(got, "X-WR-CALNAME:Work") {
		t.Fatalf("expected X-WR-CALNAME:Work, got:\n%s", got)
	}
	if !strings.Contains(got, "X-APPLE-CALENDAR-COLOR:#12809CFF") {
		t.Fatalf("expected X-APPLE-CALENDAR-COLOR:#12809CFF, got:\n%s", got)
	}
	if !strings.Contains(got, "PRODID:"+ProdID) {
		t.Fatalf("expected the unchanged PRODID, got:\n%s", got)
	}
	if strings.Contains(got, "BEGIN:VEVENT") {
		t.Fatalf("expected no VEVENTs, got:\n%s", got)
	}
	if !strings.HasPrefix(got, "BEGIN:VCALENDAR\r\n") || !strings.HasSuffix(got, "END:VCALENDAR\r\n") {
		t.Fatalf("expected a well-formed BEGIN/END VCALENDAR envelope, got:\n%s", got)
	}
}

func TestEncodeEmpty_RejectsCalendarWithChildren(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	cal, err := CalendarToICal("Work", "#12809CFF", []repository.Event{master}, nil, SerializationTarget{})
	if err != nil {
		t.Fatalf("CalendarToICal: %v", err)
	}

	if _, err := EncodeEmpty(cal); err == nil {
		t.Fatalf("expected EncodeEmpty to reject a calendar with children")
	}
}

func TestOccurrenceToICal_FreshUIDNoRRuleNoRecurrenceID(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Rrule:     "FREQ=WEEKLY;BYDAY=TU",
		Start:     time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}
	occurrence := master
	occurrence.Start = time.Date(2026, 6, 9, 9, 0, 0, 0, time.UTC)
	occurrence.End = time.Date(2026, 6, 9, 9, 30, 0, 0, time.UTC)
	occurrence.Rrule = ""

	cal, err := OccurrenceToICal("fresh-uid", occurrence)
	if err != nil {
		t.Fatalf("OccurrenceToICal: %v", err)
	}
	body, err := Encode(cal)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if strings.Count(string(body), "BEGIN:VEVENT") != 1 {
		t.Fatalf("expected exactly one VEVENT, got:\n%s", body)
	}
	if !strings.Contains(string(body), "UID:fresh-uid") {
		t.Fatalf("expected the fresh UID, not the series' own id, got:\n%s", body)
	}
	if strings.Contains(string(body), "evt-1") {
		t.Fatalf("expected no trace of the series' own id, got:\n%s", body)
	}
	if strings.Contains(string(body), "RRULE") {
		t.Fatalf("expected no RRULE on a flattened Occurrence, got:\n%s", body)
	}
	if strings.Contains(string(body), "RECURRENCE-ID") {
		t.Fatalf("expected no RECURRENCE-ID on a flattened Occurrence, got:\n%s", body)
	}
	if !strings.Contains(string(body), "DTSTART:20260609T090000") {
		t.Fatalf("expected DTSTART set to the concrete occurrence start, got:\n%s", body)
	}
	if !strings.Contains(string(body), "DTEND:20260609T093000") {
		t.Fatalf("expected DTEND set to the concrete occurrence end, got:\n%s", body)
	}
}

func TestOccurrenceToICal_NamedTzid_EmitsVTimezone(t *testing.T) {
	tzid := "Europe/Berlin"
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Tzid:      &tzid,
		Rrule:     "FREQ=WEEKLY;BYDAY=TU",
		Start:     time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	occurrence := master
	occurrence.Start = time.Date(2026, 6, 9, 9, 0, 0, 0, time.UTC)
	occurrence.End = time.Date(2026, 6, 9, 9, 30, 0, 0, time.UTC)
	occurrence.Rrule = ""

	cal, err := OccurrenceToICal("fresh-uid", occurrence)
	if err != nil {
		t.Fatalf("OccurrenceToICal: %v", err)
	}
	body, err := Encode(cal)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if !strings.Contains(string(body), "BEGIN:VTIMEZONE") {
		t.Fatalf("expected a VTIMEZONE for the occurrence's tzid, got:\n%s", body)
	}
}
