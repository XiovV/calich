package icalendar

import (
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calendar/server/internal/repository"
)

func mustParse(t *testing.T, master repository.Event, overrides []repository.Event) *ParsedSeries {
	t.Helper()
	cal, _, err := SeriesToICal(master, overrides, SerializationTarget{})
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}
	parsed, err := ParseCalendarObject(cal)
	if err != nil {
		t.Fatalf("parseCalendarObject: %v", err)
	}
	return parsed
}

func TestParseCalendarObject_NonRecurring_RoundTrips(t *testing.T) {
	master := repository.Event{
		ID:          "evt-1",
		Title:       "Standup",
		Description: "Daily sync",
		Location:    "Room 1",
		Start:       time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:         time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	parsed := mustParse(t, master, nil)

	if parsed.UID != "evt-1" {
		t.Fatalf("expected UID evt-1, got %q", parsed.UID)
	}
	if parsed.Master.Title != "Standup" || parsed.Master.Description != "Daily sync" || parsed.Master.Location != "Room 1" {
		t.Fatalf("expected fields to round-trip, got %+v", parsed.Master)
	}
	if !parsed.Master.Start.Equal(master.Start) || !parsed.Master.End.Equal(master.End) {
		t.Fatalf("expected start/end to round-trip, got %v/%v", parsed.Master.Start, parsed.Master.End)
	}
	if parsed.Master.AllDay {
		t.Fatalf("expected AllDay false")
	}
	if parsed.Master.Tzid != nil {
		t.Fatalf("expected a floating (nil tzid) event, got %v", *parsed.Master.Tzid)
	}
	if len(parsed.Overrides) != 0 {
		t.Fatalf("expected no overrides, got %d", len(parsed.Overrides))
	}
}

func TestParseCalendarObject_URL_RoundTrips(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		URL:       "https://example.com/ticket/42",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	parsed := mustParse(t, master, nil)

	if parsed.Master.URL != "https://example.com/ticket/42" {
		t.Fatalf("expected URL to round-trip, got %q", parsed.Master.URL)
	}
}

func TestParseCalendarObject_URLAbsent_RoundTripsEmpty(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	parsed := mustParse(t, master, nil)

	if parsed.Master.URL != "" {
		t.Fatalf("expected empty URL, got %q", parsed.Master.URL)
	}
}

func TestParseCalendarObject_URLNonWebScheme_RoundTripsVerbatim(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		URL:       "message://<abc123@mail.example.com>",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	parsed := mustParse(t, master, nil)

	if parsed.Master.URL != "message://<abc123@mail.example.com>" {
		t.Fatalf("expected non-web-scheme URL to survive verbatim, got %q", parsed.Master.URL)
	}
}

// TestParseCalendarObject_URLBareWord_NeverRejected exercises #207's
// acceptance criterion that a feed value that is not a usable web link is
// stored as-is rather than rejected — a raw VEVENT crafted by hand (not via
// mustParse's SeriesToICal round trip, since buildVEvent's writer side
// wouldn't itself produce a bare word) to simulate what a decode-only path
// (a feed's PUT/import/Refresh) actually receives.
func TestParseCalendarObject_URLBareWord_NeverRejected(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:evt-1
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
SUMMARY:Standup
URL:not a url at all
END:VEVENT
END:VCALENDAR
`
	cal, err := ical.NewDecoder(strings.NewReader(ics)).Decode()
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	parsed, err := ParseCalendarObject(cal)
	if err != nil {
		t.Fatalf("expected a bare-word URL to be accepted rather than rejected, got error: %v", err)
	}
	if parsed.Master.URL != "not a url at all" {
		t.Fatalf("expected the bare word to be stored verbatim, got %q", parsed.Master.URL)
	}
}

func TestParseCalendarObject_OverrideURL_RoundTrips(t *testing.T) {
	recurrenceID := time.Date(2026, 6, 9, 9, 0, 0, 0, time.UTC)
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		URL:       "https://example.com/master",
		Start:     time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC),
		Rrule:     "FREQ=WEEKLY;BYDAY=TU",
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}
	override := repository.Event{
		ID:           "evt-1-override",
		Title:        "Standup (moved)",
		URL:          "https://example.com/override",
		RecurrenceID: &recurrenceID,
		Start:        time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
		End:          time.Date(2026, 6, 9, 10, 30, 0, 0, time.UTC),
		CreatedAt:    time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	parsed := mustParse(t, master, []repository.Event{override})

	if parsed.Master.URL != "https://example.com/master" {
		t.Fatalf("expected master URL to round-trip, got %q", parsed.Master.URL)
	}
	if len(parsed.Overrides) != 1 || parsed.Overrides[0].URL != "https://example.com/override" {
		t.Fatalf("expected override URL to round-trip independently, got %+v", parsed.Overrides)
	}
}

func TestParseCalendarObject_AllDay_RoundTrips(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Conference",
		AllDay:    true,
		Start:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	parsed := mustParse(t, master, nil)

	if !parsed.Master.AllDay {
		t.Fatalf("expected AllDay true")
	}
	if !parsed.Master.Start.Equal(master.Start) || !parsed.Master.End.Equal(master.End) {
		t.Fatalf("expected start/end to round-trip, got %v/%v", parsed.Master.Start, parsed.Master.End)
	}
}

func TestParseCalendarObject_NamedTzid_RoundTrips(t *testing.T) {
	tzid := "Europe/Berlin"
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Tzid:      &tzid,
		Start:     time.Date(2026, 7, 1, 13, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 14, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	parsed := mustParse(t, master, nil)

	if parsed.Master.Tzid == nil || *parsed.Master.Tzid != "Europe/Berlin" {
		t.Fatalf("expected tzid Europe/Berlin, got %v", parsed.Master.Tzid)
	}
	if !parsed.Master.Start.Equal(master.Start) {
		t.Fatalf("expected start instant to round-trip, got %v want %v", parsed.Master.Start, master.Start)
	}
}

func TestParseCalendarObject_EtcUTCTzid_RoundTrips(t *testing.T) {
	tzid := "Etc/UTC"
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Tzid:      &tzid,
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	parsed := mustParse(t, master, nil)

	if parsed.Master.Tzid == nil || *parsed.Master.Tzid != "Etc/UTC" {
		t.Fatalf("expected tzid Etc/UTC, got %v", parsed.Master.Tzid)
	}
}

func TestParseCalendarObject_MasterPlusOverridePlusExdate_RoundTrips(t *testing.T) {
	recurrenceID := time.Date(2026, 6, 9, 9, 0, 0, 0, time.UTC)
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC),
		Rrule:     "FREQ=WEEKLY;BYDAY=TU",
		Exdates:   []time.Time{time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)},
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}
	parentID := "evt-1"
	override := repository.Event{
		ID:           "evt-2",
		ParentID:     &parentID,
		RecurrenceID: &recurrenceID,
		Title:        "Standup (moved)",
		Start:        time.Date(2026, 6, 9, 11, 0, 0, 0, time.UTC),
		End:          time.Date(2026, 6, 9, 11, 30, 0, 0, time.UTC),
		CreatedAt:    time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC),
	}

	parsed := mustParse(t, master, []repository.Event{override})

	if parsed.Rrule != "FREQ=WEEKLY;BYDAY=TU" {
		t.Fatalf("expected rrule to round-trip, got %q", parsed.Rrule)
	}
	if len(parsed.Exdates) != 1 || !parsed.Exdates[0].Equal(master.Exdates[0]) {
		t.Fatalf("expected exdate to round-trip, got %v", parsed.Exdates)
	}
	if len(parsed.Overrides) != 1 {
		t.Fatalf("expected exactly one override, got %d", len(parsed.Overrides))
	}
	got := parsed.Overrides[0]
	if got.Title != "Standup (moved)" {
		t.Fatalf("expected override title to round-trip, got %q", got.Title)
	}
	if !got.RecurrenceID.Equal(recurrenceID) {
		t.Fatalf("expected recurrence-id to round-trip, got %v want %v", got.RecurrenceID, recurrenceID)
	}
	if !got.Start.Equal(override.Start) || !got.End.Equal(override.End) {
		t.Fatalf("expected override start/end to round-trip, got %v/%v", got.Start, got.End)
	}
}

func TestParseCalendarObject_Reminders_RoundTrip(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Reminders: []repository.Reminder{
			{ID: 1, OffsetMinutes: 15, Channel: "notification"},
			{ID: 2, OffsetMinutes: 60, Channel: "email"},
		},
	}

	parsed := mustParse(t, master, nil)

	if len(parsed.Master.Reminders) != 2 {
		t.Fatalf("expected two reminders, got %d: %+v", len(parsed.Master.Reminders), parsed.Master.Reminders)
	}
	byOffset := map[int]string{}
	for _, r := range parsed.Master.Reminders {
		byOffset[r.OffsetMinutes] = r.Channel
	}
	if byOffset[15] != "notification" || byOffset[60] != "email" {
		t.Fatalf("expected reminders to round-trip channel/offset, got %+v", parsed.Master.Reminders)
	}
}

func TestParseCalendarObject_AllDayReminder_RoundTrips(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Conference",
		AllDay:    true,
		Start:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Reminders: []repository.Reminder{{ID: 1, OffsetMinutes: 15, Channel: "notification"}},
	}

	parsed := mustParse(t, master, nil)

	if len(parsed.Master.Reminders) != 1 || parsed.Master.Reminders[0].OffsetMinutes != 15 {
		t.Fatalf("expected the 15-minute-before reminder to round-trip, got %+v", parsed.Master.Reminders)
	}
}

func TestParseCalendarObject_MismatchedUIDs_Errors(t *testing.T) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, ProdID)
	cal.Props.SetText(ical.PropVersion, "2.0")

	v1 := ical.NewEvent()
	v1.Props.SetText(ical.PropUID, "evt-1")
	v1.Props.SetDateTime(ical.PropDateTimeStart, time.Now())
	cal.Children = append(cal.Children, v1.Component)

	v2 := ical.NewEvent()
	v2.Props.SetText(ical.PropUID, "evt-2")
	v2.Props.SetDateTime(ical.PropDateTimeStart, time.Now())
	recurrenceIDProp := ical.NewProp(ical.PropRecurrenceID)
	recurrenceIDProp.SetDateTime(time.Now())
	v2.Props.Add(recurrenceIDProp)
	cal.Children = append(cal.Children, v2.Component)

	if _, err := ParseCalendarObject(cal); err == nil {
		t.Fatalf("expected an error for mismatched UIDs")
	}
}

func TestParseCalendarObject_NoMasterVEvent_Errors(t *testing.T) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, ProdID)
	cal.Props.SetText(ical.PropVersion, "2.0")

	v := ical.NewEvent()
	v.Props.SetText(ical.PropUID, "evt-1")
	v.Props.SetDateTime(ical.PropDateTimeStart, time.Now())
	recurrenceIDProp := ical.NewProp(ical.PropRecurrenceID)
	recurrenceIDProp.SetDateTime(time.Now())
	v.Props.Add(recurrenceIDProp)
	cal.Children = append(cal.Children, v.Component)

	if _, err := ParseCalendarObject(cal); err == nil {
		t.Fatalf("expected an error when no VEVENT lacks RECURRENCE-ID")
	}
}

func TestParseCalendarObject_Color_RoundTrips(t *testing.T) {
	color := "#FF0000FF"
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Color:     &color,
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	parsed := mustParse(t, master, nil)

	if parsed.Master.Color == nil || *parsed.Master.Color != "#FF0000FF" {
		t.Fatalf("expected color #FF0000FF to round-trip losslessly (red is an exact CSS3 keyword), got %v", parsed.Master.Color)
	}
}

func TestParseCalendarObject_NoColorProperty_ColorAbsent(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	parsed := mustParse(t, master, nil)

	if parsed.Master.Color != nil {
		t.Fatalf("expected no COLOR property to decode to a nil (inherited) color, got %v", *parsed.Master.Color)
	}
}

func TestParseCalendarObject_ColorKeyword_DecodesExactDefinedRGB(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	cal, _, err := SeriesToICal(master, nil, SerializationTarget{})
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}
	cal.Children[0].Props.SetText(ical.PropColor, "cornflowerblue")

	parsed, err := ParseCalendarObject(cal)
	if err != nil {
		t.Fatalf("parseCalendarObject: %v", err)
	}
	if parsed.Master.Color == nil || *parsed.Master.Color != "#6495EDFF" {
		t.Fatalf("expected cornflowerblue to decode to #6495EDFF, got %v", parsed.Master.Color)
	}
}

func TestParseCalendarObject_UnrecognizedColorKeyword_IsDropped(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	cal, _, err := SeriesToICal(master, nil, SerializationTarget{})
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}
	cal.Children[0].Props.SetText(ical.PropColor, "not-a-css3-keyword")

	parsed, err := ParseCalendarObject(cal)
	if err != nil {
		t.Fatalf("parseCalendarObject: %v", err)
	}
	if parsed.Master.Color != nil {
		t.Fatalf("expected an unrecognized COLOR keyword to be dropped, got %v", *parsed.Master.Color)
	}
}

func TestParseCalendarObject_UnmodeledValarmAction_IsDropped(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	cal, _, err := SeriesToICal(master, nil, SerializationTarget{})
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}
	alarm := ical.NewComponent(ical.CompAlarm)
	alarm.Props.SetText(ical.PropAction, "AUDIO")
	trigger := ical.NewProp(ical.PropTrigger)
	trigger.SetDuration(-15 * time.Minute)
	alarm.Props.Add(trigger)
	cal.Children[0].Children = append(cal.Children[0].Children, alarm)

	parsed, err := ParseCalendarObject(cal)
	if err != nil {
		t.Fatalf("parseCalendarObject: %v", err)
	}
	if len(parsed.Master.Reminders) != 0 {
		t.Fatalf("expected an unsupported VALARM action to be dropped, got %+v", parsed.Master.Reminders)
	}
}
