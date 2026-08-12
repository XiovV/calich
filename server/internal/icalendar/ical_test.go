package icalendar

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calendar/server/internal/repository"
)

func mustEncode(t *testing.T, master repository.Event, overrides []repository.Event) string {
	t.Helper()
	cal, err := SeriesToICal(master, overrides, SerializationTarget{})
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.String()
}

func TestSeriesToICal_NonRecurring_SingleVEvent(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncode(t, master, nil)

	if strings.Count(body, "BEGIN:VEVENT") != 1 {
		t.Fatalf("expected exactly one VEVENT, got:\n%s", body)
	}
	if !strings.Contains(body, "UID:evt-1") {
		t.Fatalf("expected UID:evt-1, got:\n%s", body)
	}
	if !strings.Contains(body, "SUMMARY:Standup") {
		t.Fatalf("expected SUMMARY:Standup, got:\n%s", body)
	}
	if strings.Contains(body, "RRULE") {
		t.Fatalf("expected no RRULE on a non-recurring event, got:\n%s", body)
	}
}

func TestSeriesToICal_MasterPlusOverridePlusException(t *testing.T) {
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

	body := mustEncode(t, master, []repository.Event{override})

	if strings.Count(body, "BEGIN:VEVENT") != 2 {
		t.Fatalf("expected master + one override VEVENT, got:\n%s", body)
	}
	if strings.Count(body, "UID:evt-1") != 2 {
		t.Fatalf("expected both VEVENTs to share the master's UID, got:\n%s", body)
	}
	if !strings.Contains(body, "RRULE:FREQ=WEEKLY;BYDAY=TU") {
		t.Fatalf("expected the master's RRULE, got:\n%s", body)
	}
	if !strings.Contains(body, "EXDATE:20260616T090000") {
		t.Fatalf("expected an EXDATE for the cancelled occurrence, got:\n%s", body)
	}
	if !strings.Contains(body, "RECURRENCE-ID:20260609T090000") {
		t.Fatalf("expected the override's RECURRENCE-ID to match its master-generated occurrence, got:\n%s", body)
	}
	if !strings.Contains(body, "SUMMARY:Standup (moved)") {
		t.Fatalf("expected the override's own summary, got:\n%s", body)
	}
}

func TestSeriesToICal_AllDay_UsesDateValueNoTime(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Conference",
		AllDay:    true,
		Start:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncode(t, master, nil)

	if !strings.Contains(body, "DTSTART;VALUE=DATE:20260701") {
		t.Fatalf("expected an all-day DTSTART DATE value, got:\n%s", body)
	}
	if !strings.Contains(body, "DTEND;VALUE=DATE:20260703") {
		t.Fatalf("expected an all-day DTEND DATE value, got:\n%s", body)
	}
}

func TestSeriesToICal_NamedTzid_SetsTZIDParam(t *testing.T) {
	tzid := "Europe/Berlin"
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Tzid:      &tzid,
		Start:     time.Date(2026, 7, 1, 13, 0, 0, 0, time.UTC), // 15:00 in Berlin (CEST, UTC+2)
		End:       time.Date(2026, 7, 1, 14, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncode(t, master, nil)

	if !strings.Contains(body, `DTSTART;TZID=Europe/Berlin:20260701T150000`) {
		t.Fatalf("expected DTSTART anchored in Europe/Berlin at the local wall-clock time, got:\n%s", body)
	}
}

func TestSeriesToICal_EtcUTCTzid_SetsExplicitTZIDParam(t *testing.T) {
	tzid := "Etc/UTC"
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Tzid:      &tzid,
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncode(t, master, nil)

	// An absolute instant (ADR-0019) is still an explicit TZID=Etc/UTC
	// anchor, distinct from both a named zone and a bare "Z" floating-free
	// UTC instant.
	if !strings.Contains(body, `DTSTART;TZID=Etc/UTC:20260701T150000`) {
		t.Fatalf("expected DTSTART with an explicit TZID=Etc/UTC param, got:\n%s", body)
	}
}

func TestSeriesToICal_FloatingTzid_NoParamNoZ(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncode(t, master, nil)

	if !strings.Contains(body, "DTSTART:20260701T150000\r\n") {
		t.Fatalf("expected a bare floating DTSTART with no TZID and no Z suffix, got:\n%s", body)
	}
}

func TestSeriesToICal_DescriptionAndLocation(t *testing.T) {
	master := repository.Event{
		ID:          "evt-1",
		Title:       "Meeting",
		Description: "Quarterly planning",
		Location:    "Room 4B",
		Start:       time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:         time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncode(t, master, nil)

	if !strings.Contains(body, "DESCRIPTION:Quarterly planning") {
		t.Fatalf("expected DESCRIPTION, got:\n%s", body)
	}
	if !strings.Contains(body, "LOCATION:Room 4B") {
		t.Fatalf("expected LOCATION, got:\n%s", body)
	}
}

func TestSeriesToICal_ColorSnapsToNearestCSS3Keyword(t *testing.T) {
	color := "#FF0000FF"
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Color:     &color,
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncode(t, master, nil)

	if !strings.Contains(body, "COLOR:red") {
		t.Fatalf("expected COLOR:red, got:\n%s", body)
	}
}

func TestSeriesToICal_NoColor_OmitsColorProperty(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncode(t, master, nil)

	if strings.Contains(body, "COLOR") {
		t.Fatalf("expected no COLOR property for an inherited (nil) color, got:\n%s", body)
	}
}

func TestSeriesToICal_ColorOnOverrideOnly(t *testing.T) {
	overrideColor := "#0000FFFF"
	recurrenceID := time.Date(2026, 7, 8, 15, 0, 0, 0, time.UTC)
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Rrule:     "FREQ=WEEKLY",
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	override := repository.Event{
		ID:           "evt-1-override",
		Title:        "Meeting",
		Color:        &overrideColor,
		RecurrenceID: &recurrenceID,
		Start:        recurrenceID,
		End:          recurrenceID.Add(time.Hour),
		CreatedAt:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncode(t, master, []repository.Event{override})

	if strings.Count(body, "COLOR:blue") != 1 {
		t.Fatalf("expected exactly one COLOR:blue on the override, got:\n%s", body)
	}
}

func TestSeriesToICal_Reminders_SerializeAsValarm(t *testing.T) {
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

	body := mustEncode(t, master, nil)

	if strings.Count(body, "BEGIN:VALARM") != 2 {
		t.Fatalf("expected two VALARMs, got:\n%s", body)
	}
	if !strings.Contains(body, "ACTION:DISPLAY") {
		t.Fatalf("expected ACTION:DISPLAY for the notification channel, got:\n%s", body)
	}
	if !strings.Contains(body, "ACTION:EMAIL") {
		t.Fatalf("expected ACTION:EMAIL for the email channel, got:\n%s", body)
	}
	if !strings.Contains(body, "TRIGGER:-PT900S") {
		t.Fatalf("expected a -15-minute TRIGGER for the 15-minute-before reminder, got:\n%s", body)
	}
	if !strings.Contains(body, "TRIGGER:-PT3600S") {
		t.Fatalf("expected a -60-minute TRIGGER for the 60-minute-before reminder, got:\n%s", body)
	}
}

func TestSeriesToICal_AllDayReminder_TriggerMeasuredFromNineAM(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Conference",
		AllDay:    true,
		Start:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		// 09:00 - 15min = 08:45, which is 8h45m (525m) after the stored
		// midnight DTSTART.
		Reminders: []repository.Reminder{{ID: 1, OffsetMinutes: 15, Channel: "notification"}},
	}

	body := mustEncode(t, master, nil)

	if !strings.Contains(body, "TRIGGER:PT31500S") {
		t.Fatalf("expected the all-day reminder's trigger (8h45m after midnight DTSTART, i.e. 08:45) to be measured from 09:00, got:\n%s", body)
	}
}

func TestSeriesToICal_Organizer_EmittedWithNameAndMailto(t *testing.T) {
	organizerID := int64(7)
	master := repository.Event{
		ID:             "evt-1",
		Title:          "Meeting",
		Start:          time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:            time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt:      time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy:      &organizerID,
		CreatedByName:  "Alice Example",
		CreatedByEmail: "alice@example.com",
	}

	body := mustEncode(t, master, nil)

	if !strings.Contains(body, "ORGANIZER;CN=Alice Example:mailto:alice@example.com") {
		t.Fatalf("expected ORGANIZER naming the creator, got:\n%s", body)
	}
}

func TestSeriesToICal_NoOrganizer_WhenCreatedByNil(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncode(t, master, nil)

	if strings.Contains(body, "ORGANIZER") {
		t.Fatalf("expected no ORGANIZER when CreatedBy is nil (creator deleted, or never recorded), got:\n%s", body)
	}
}

func TestSeriesToICal_Attendees_EmittedWithPartstatAndMailto(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Attendees: []repository.AttendeeWithName{
			{Attendee: repository.Attendee{Response: repository.ResponseAccepted}, Name: "Bob Guest", Email: "bob@example.com"},
			{Attendee: repository.Attendee{Response: repository.ResponseNeedsAction}, Name: "Carol Guest", Email: "carol@example.com"},
		},
	}

	body := mustEncode(t, master, nil)

	if strings.Count(body, "BEGIN:VALARM") != 0 {
		t.Fatalf("test setup error: unexpected VALARM in body:\n%s", body)
	}
	if !strings.Contains(body, "ATTENDEE;CN=Bob Guest;PARTSTAT=ACCEPTED:mailto:bob@example.com") {
		t.Fatalf("expected an ATTENDEE for Bob with PARTSTAT=ACCEPTED, got:\n%s", body)
	}
	if !strings.Contains(body, "ATTENDEE;CN=Carol Guest;PARTSTAT=NEEDS-ACTION:mailto:carol@example.com") {
		t.Fatalf("expected an ATTENDEE for Carol with PARTSTAT=NEEDS-ACTION, got:\n%s", body)
	}
}

func TestSeriesToICal_NoAttendees_OmitsAttendeeProperty(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncode(t, master, nil)

	if strings.Contains(body, "ATTENDEE") {
		t.Fatalf("expected no ATTENDEE property for an Event with no Attendees, got:\n%s", body)
	}
}

func TestSeriesToICal_AttendeesSurviveMasterOverrideSplit(t *testing.T) {
	recurrenceID := time.Date(2026, 6, 9, 9, 0, 0, 0, time.UTC)
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC),
		Rrule:     "FREQ=WEEKLY;BYDAY=TU",
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Attendees: []repository.AttendeeWithName{
			{Attendee: repository.Attendee{Response: repository.ResponseAccepted}, Name: "Bob Guest", Email: "bob@example.com"},
		},
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
		Attendees: []repository.AttendeeWithName{
			{Attendee: repository.Attendee{Response: repository.ResponseTentative}, Name: "Dana Guest", Email: "dana@example.com"},
		},
	}

	body := mustEncode(t, master, []repository.Event{override})

	if !strings.Contains(body, "ATTENDEE;CN=Bob Guest;PARTSTAT=ACCEPTED:mailto:bob@example.com") {
		t.Fatalf("expected the master's own Attendee to survive, got:\n%s", body)
	}
	if !strings.Contains(body, "ATTENDEE;CN=Dana Guest;PARTSTAT=TENTATIVE:mailto:dana@example.com") {
		t.Fatalf("expected the override's own, distinct Attendee to survive, got:\n%s", body)
	}
	if strings.Contains(body, "Carol") {
		t.Fatalf("test setup error: unexpected attendee name in body:\n%s", body)
	}
	// The override must not inherit the master's guest, and vice versa.
	if strings.Count(body, "ATTENDEE") != 2 {
		t.Fatalf("expected exactly one ATTENDEE per VEVENT (two total), got:\n%s", body)
	}
}

func TestCalendarETag_ChangesWhenReconstructionChanges(t *testing.T) {
	base := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	cal1, err := SeriesToICal(base, nil, SerializationTarget{})
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}
	etag1, err := CalendarETag(cal1)
	if err != nil {
		t.Fatalf("calendarETag: %v", err)
	}

	cal2, err := SeriesToICal(base, nil, SerializationTarget{})
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}
	etag2, err := CalendarETag(cal2)
	if err != nil {
		t.Fatalf("calendarETag: %v", err)
	}
	if etag1 != etag2 {
		t.Fatalf("expected the same reconstruction to produce the same ETag, got %q and %q", etag1, etag2)
	}

	changed := base
	changed.Title = "Meeting (renamed)"
	cal3, err := SeriesToICal(changed, nil, SerializationTarget{})
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}
	etag3, err := CalendarETag(cal3)
	if err != nil {
		t.Fatalf("calendarETag: %v", err)
	}
	if etag1 == etag3 {
		t.Fatalf("expected a changed reconstruction to produce a different ETag")
	}
}
