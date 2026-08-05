package caldavserver

import (
	"testing"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calendar/server/internal/repository"
)

func mustParse(t *testing.T, master repository.Event, overrides []repository.Event) *parsedSeries {
	t.Helper()
	cal, err := seriesToICal(master, overrides)
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}
	parsed, err := parseCalendarObject(cal)
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
	cal.Props.SetText(ical.PropProductID, prodID)
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

	if _, err := parseCalendarObject(cal); err == nil {
		t.Fatalf("expected an error for mismatched UIDs")
	}
}

func TestParseCalendarObject_NoMasterVEvent_Errors(t *testing.T) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, prodID)
	cal.Props.SetText(ical.PropVersion, "2.0")

	v := ical.NewEvent()
	v.Props.SetText(ical.PropUID, "evt-1")
	v.Props.SetDateTime(ical.PropDateTimeStart, time.Now())
	recurrenceIDProp := ical.NewProp(ical.PropRecurrenceID)
	recurrenceIDProp.SetDateTime(time.Now())
	v.Props.Add(recurrenceIDProp)
	cal.Children = append(cal.Children, v.Component)

	if _, err := parseCalendarObject(cal); err == nil {
		t.Fatalf("expected an error when no VEVENT lacks RECURRENCE-ID")
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
	cal, err := seriesToICal(master, nil)
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}
	alarm := ical.NewComponent(ical.CompAlarm)
	alarm.Props.SetText(ical.PropAction, "AUDIO")
	trigger := ical.NewProp(ical.PropTrigger)
	trigger.SetDuration(-15 * time.Minute)
	alarm.Props.Add(trigger)
	cal.Children[0].Children = append(cal.Children[0].Children, alarm)

	parsed, err := parseCalendarObject(cal)
	if err != nil {
		t.Fatalf("parseCalendarObject: %v", err)
	}
	if len(parsed.Master.Reminders) != 0 {
		t.Fatalf("expected an unsupported VALARM action to be dropped, got %+v", parsed.Master.Reminders)
	}
}
