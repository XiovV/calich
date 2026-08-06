package icalendar

import (
	"strings"
	"testing"
)

func mustParseFile(t *testing.T, ics string) *ParsedFile {
	t.Helper()
	f, err := ParseImportFile(strings.NewReader(normalizeCRLF(ics)))
	if err != nil {
		t.Fatalf("ParseImportFile: %v", err)
	}
	return f
}

// normalizeCRLF lets test fixtures use plain "\n" while the wire format
// requires CRLF line endings.
func normalizeCRLF(s string) string {
	return strings.ReplaceAll(s, "\n", "\r\n")
}

func TestParseImportFile_GroupsMultipleSeriesByUID(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:series-1
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
SUMMARY:Standup
END:VEVENT
BEGIN:VEVENT
UID:series-2
DTSTART:20260602T100000Z
DTEND:20260602T110000Z
SUMMARY:Planning
END:VEVENT
END:VCALENDAR
`
	f := mustParseFile(t, ics)

	if len(f.Series) != 2 {
		t.Fatalf("expected 2 series, got %d: %+v", len(f.Series), f.Series)
	}
	if len(f.Skipped) != 0 {
		t.Fatalf("expected no skipped series, got %+v", f.Skipped)
	}
	if f.Series[0].UID != "series-1" || f.Series[1].UID != "series-2" {
		t.Fatalf("expected UIDs in order, got %q, %q", f.Series[0].UID, f.Series[1].UID)
	}
}

func TestParseImportFile_GroupsMasterAndOverrideByUID(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:series-1
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
RRULE:FREQ=WEEKLY
SUMMARY:Standup
END:VEVENT
BEGIN:VEVENT
UID:series-1
RECURRENCE-ID:20260608T090000Z
DTSTART:20260608T100000Z
DTEND:20260608T103000Z
SUMMARY:Standup (moved)
END:VEVENT
END:VCALENDAR
`
	f := mustParseFile(t, ics)

	if len(f.Series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(f.Series))
	}
	if len(f.Series[0].Overrides) != 1 {
		t.Fatalf("expected 1 override, got %d", len(f.Series[0].Overrides))
	}
	if f.Series[0].Overrides[0].Title != "Standup (moved)" {
		t.Fatalf("expected override title to round-trip, got %q", f.Series[0].Overrides[0].Title)
	}
}

func TestParseImportFile_OrphanRecurrenceID_Skipped(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:orphan-1
RECURRENCE-ID:20260608T090000Z
DTSTART:20260608T100000Z
DTEND:20260608T103000Z
SUMMARY:Orphan override
END:VEVENT
END:VCALENDAR
`
	f := mustParseFile(t, ics)

	if len(f.Series) != 0 {
		t.Fatalf("expected no series, got %d", len(f.Series))
	}
	if len(f.Skipped) != 1 || f.Skipped[0].Reason != SkipOrphanRecurrence {
		t.Fatalf("expected one orphan-recurrence skip, got %+v", f.Skipped)
	}
	if f.Skipped[0].Title != "Orphan override" {
		t.Fatalf("expected sample title, got %q", f.Skipped[0].Title)
	}
}

func TestParseImportFile_MissingDTStart_Skipped(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:no-dtstart
SUMMARY:No start
END:VEVENT
END:VCALENDAR
`
	f := mustParseFile(t, ics)

	if len(f.Series) != 0 || len(f.Skipped) != 1 || f.Skipped[0].Reason != SkipMissingDTStart {
		t.Fatalf("expected one missing-dtstart skip, got series=%+v skipped=%+v", f.Series, f.Skipped)
	}
}

func TestParseImportFile_CancelledMaster_Skipped(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:cancelled-1
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
STATUS:CANCELLED
SUMMARY:Cancelled meeting
END:VEVENT
END:VCALENDAR
`
	f := mustParseFile(t, ics)

	if len(f.Series) != 0 || len(f.Skipped) != 1 || f.Skipped[0].Reason != SkipCancelled {
		t.Fatalf("expected one cancelled skip, got series=%+v skipped=%+v", f.Series, f.Skipped)
	}
}

func TestParseImportFile_IgnoresVTodoVJournalVFreeBusy(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VTODO
UID:todo-1
SUMMARY:Buy milk
END:VTODO
BEGIN:VJOURNAL
UID:journal-1
SUMMARY:Diary entry
END:VJOURNAL
BEGIN:VFREEBUSY
UID:freebusy-1
END:VFREEBUSY
END:VCALENDAR
`
	f := mustParseFile(t, ics)

	if f.IgnoredVTodo != 1 || f.IgnoredVJournal != 1 || f.IgnoredVFreeBusy != 1 {
		t.Fatalf("expected one of each ignored, got %+v", f)
	}
	if len(f.Series) != 0 || len(f.Skipped) != 0 {
		t.Fatalf("expected no series or skipped, got series=%+v skipped=%+v", f.Series, f.Skipped)
	}
}

func TestParseImportFile_UnresolvableTZID_DowngradesToFloating(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:outlook-tz
DTSTART;TZID=Eastern Standard Time:20260601T090000
DTEND;TZID=Eastern Standard Time:20260601T093000
SUMMARY:Outlook event
END:VEVENT
END:VCALENDAR
`
	f := mustParseFile(t, ics)

	if len(f.Series) != 1 {
		t.Fatalf("expected 1 series, got %d: skipped=%+v", len(f.Series), f.Skipped)
	}
	s := f.Series[0]
	if s.Master.Tzid != nil {
		t.Fatalf("expected the unresolvable tzid to be dropped, got %q", *s.Master.Tzid)
	}
	if s.FloatingDowngrades != 1 {
		t.Fatalf("expected 1 floating downgrade, got %d", s.FloatingDowngrades)
	}
}

func TestParseImportFile_ResolvableTZID_Kept(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:berlin-tz
DTSTART;TZID=Europe/Berlin:20260601T090000
DTEND;TZID=Europe/Berlin:20260601T093000
SUMMARY:Berlin event
END:VEVENT
END:VCALENDAR
`
	f := mustParseFile(t, ics)

	if len(f.Series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(f.Series))
	}
	s := f.Series[0]
	if s.Master.Tzid == nil || *s.Master.Tzid != "Europe/Berlin" {
		t.Fatalf("expected tzid to survive, got %v", s.Master.Tzid)
	}
	if s.FloatingDowngrades != 0 {
		t.Fatalf("expected no floating downgrades, got %d", s.FloatingDowngrades)
	}
}

func TestParseImportFile_UnsupportedValarmTrigger_DroppedAndCounted(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:with-alarm
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
SUMMARY:Has alarms
BEGIN:VALARM
ACTION:DISPLAY
TRIGGER;RELATED=END:-PT15M
END:VALARM
BEGIN:VALARM
ACTION:DISPLAY
TRIGGER:-PT10M
END:VALARM
END:VEVENT
END:VCALENDAR
`
	f := mustParseFile(t, ics)

	if len(f.Series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(f.Series))
	}
	s := f.Series[0]
	if len(s.Master.Reminders) != 1 {
		t.Fatalf("expected 1 surviving reminder, got %d", len(s.Master.Reminders))
	}
	if s.DroppedAlarms != 1 {
		t.Fatalf("expected 1 dropped alarm, got %d", s.DroppedAlarms)
	}
}

func TestParseImportFile_ReadsCalendarNameAndColor(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
X-WR-CALNAME:My Calendar
X-APPLE-CALENDAR-COLOR:#FF0000FF
BEGIN:VEVENT
UID:series-1
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
SUMMARY:Event
END:VEVENT
END:VCALENDAR
`
	f := mustParseFile(t, ics)

	if f.CalendarName != "My Calendar" {
		t.Fatalf("expected calendar name to round-trip, got %q", f.CalendarName)
	}
	if f.Color != "#FF0000FF" {
		t.Fatalf("expected color to round-trip, got %q", f.Color)
	}
}

func TestParseImportFile_MultipleMasters_Unparseable(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:dup-master
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
SUMMARY:First master
END:VEVENT
BEGIN:VEVENT
UID:dup-master
DTSTART:20260602T090000Z
DTEND:20260602T093000Z
SUMMARY:Second master
END:VEVENT
END:VCALENDAR
`
	f := mustParseFile(t, ics)

	if len(f.Series) != 0 || len(f.Skipped) != 1 || f.Skipped[0].Reason != SkipUnparseable {
		t.Fatalf("expected one unparseable skip, got series=%+v skipped=%+v", f.Series, f.Skipped)
	}
}
