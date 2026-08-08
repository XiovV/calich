package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// newTestImportService returns an ImportService plus a real user id and an
// existing calendar id (for action "existing" targets) to satisfy events'
// foreign keys.
func newTestImportService(t *testing.T) (svc *ImportService, events *EventService, calendars *CalendarService, userID int64, existingCalendarID string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	user, err := users.Create(context.Background(), "user-a", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	calendarRepo := repository.NewCalendarRepository(sqlDB)
	calendarSvc := NewCalendarService(calendarRepo, repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB))
	cal, err := calendarRepo.Create(context.Background(), user.ID, "cal-1", repository.CalendarFields{Name: "Existing", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	eventSvc := NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendarSvc, users, repository.NewAttachmentRepository(sqlDB))

	return NewImportService(eventSvc, calendarSvc), eventSvc, calendarSvc, user.ID, cal.ID
}

const singleEventICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
X-WR-CALNAME:Imported Calendar
BEGIN:VEVENT
UID:foreign-uid-1
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
SUMMARY:Standup
BEGIN:VALARM
ACTION:DISPLAY
TRIGGER:-PT10M
END:VALARM
BEGIN:VALARM
ACTION:EMAIL
TRIGGER:-PT10M
END:VALARM
END:VEVENT
END:VCALENDAR
`

func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

func TestImportService_DryRun_NewCalendar_DoesNotWrite(t *testing.T) {
	svc, events, calendars, userID, _ := newTestImportService(t)
	ctx := context.Background()

	summary, err := svc.Import(ctx, userID, "invite.ics", []byte(crlf(singleEventICS)), []ImportTarget{
		{Filename: "invite.ics", Action: ImportTargetNew},
	}, true)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(summary.Files) != 1 {
		t.Fatalf("expected 1 file summary, got %d", len(summary.Files))
	}
	fs := summary.Files[0]
	if fs.CalendarName != "Imported Calendar" {
		t.Fatalf("expected calendar name from X-WR-CALNAME, got %q", fs.CalendarName)
	}
	if fs.CalendarID != "" {
		t.Fatalf("expected no calendar created on a dry run, got %q", fs.CalendarID)
	}
	if fs.EventCount != 1 {
		t.Fatalf("expected 1 event, got %d", fs.EventCount)
	}
	if fs.Reminders.Notification != 1 || fs.Reminders.Email != 1 {
		t.Fatalf("expected 1 notification + 1 email reminder, got %+v", fs.Reminders)
	}

	cals, err := calendars.List(ctx, userID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}
	if len(cals) != 1 { // just the pre-existing "Existing" calendar
		t.Fatalf("expected no new calendar on a dry run, got %+v", cals)
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected no events written on a dry run, got %+v", all)
	}
}

func TestImportService_RealRun_NewCalendar_Writes(t *testing.T) {
	svc, events, calendars, userID, _ := newTestImportService(t)
	ctx := context.Background()

	summary, err := svc.Import(ctx, userID, "invite.ics", []byte(crlf(singleEventICS)), []ImportTarget{
		{Filename: "invite.ics", Action: ImportTargetNew},
	}, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(summary.Files) != 1 || summary.Files[0].CalendarID == "" {
		t.Fatalf("expected a created calendar id, got %+v", summary.Files)
	}

	cals, err := calendars.List(ctx, userID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}
	if len(cals) != 2 {
		t.Fatalf("expected the new calendar to be created, got %+v", cals)
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(all) != 1 || all[0].Title != "Standup" {
		t.Fatalf("expected the imported event to be written, got %+v", all)
	}
}

func TestImportService_ExistingCalendar_WritesIntoIt(t *testing.T) {
	svc, events, _, userID, existingCalendarID := newTestImportService(t)
	ctx := context.Background()

	summary, err := svc.Import(ctx, userID, "invite.ics", []byte(crlf(singleEventICS)), []ImportTarget{
		{Filename: "invite.ics", Action: ImportTargetExisting, CalendarID: existingCalendarID},
	}, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.Files[0].CalendarID != existingCalendarID {
		t.Fatalf("expected the existing calendar id, got %q", summary.Files[0].CalendarID)
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(all) != 1 || all[0].CalendarID != existingCalendarID {
		t.Fatalf("expected the event written into the existing calendar, got %+v", all)
	}
}

func TestImportService_ExistingCalendar_UnknownID(t *testing.T) {
	svc, _, _, userID, _ := newTestImportService(t)
	ctx := context.Background()

	_, err := svc.Import(ctx, userID, "invite.ics", []byte(crlf(singleEventICS)), []ImportTarget{
		{Filename: "invite.ics", Action: ImportTargetExisting, CalendarID: "does-not-exist"},
	}, false)
	if !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("expected ErrCalendarNotFound, got %v", err)
	}
}

// ICS import must refuse a Subscribed Calendar as an "existing" target
// (#84, ADR-0032): its Events are written only by Refresh's bypass, so a
// User importing a file into it would silently disappear on the next
// Refresh.
func TestImportService_ExistingCalendar_RejectsSubscribedTarget(t *testing.T) {
	svc, _, calendars, userID, _ := newTestImportService(t)
	ctx := context.Background()

	sourceURL := "https://example.com/feed.ics"
	subCalendar, err := calendars.Create(ctx, userID, "sub-cal-1", CalendarWrite{Name: "Feed", Color: "#123456FF", SourceURL: &sourceURL})
	if err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}

	_, err = svc.Import(ctx, userID, "invite.ics", []byte(crlf(singleEventICS)), []ImportTarget{
		{Filename: "invite.ics", Action: ImportTargetExisting, CalendarID: subCalendar.ID},
	}, false)
	if !errors.Is(err, ErrImportTargetSubscribed) {
		t.Fatalf("expected ErrImportTargetSubscribed, got %v", err)
	}
}

func TestImportService_Zip_RejectsExistingAction(t *testing.T) {
	svc, _, _, userID, existingCalendarID := newTestImportService(t)
	ctx := context.Background()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entry, err := zw.Create("a.ics")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := entry.Write([]byte(crlf(singleEventICS))); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	_, err = svc.Import(ctx, userID, "export.zip", buf.Bytes(), []ImportTarget{
		{Filename: "a.ics", Action: ImportTargetExisting, CalendarID: existingCalendarID},
	}, true)
	if !errors.Is(err, ErrImportExistingNotAllowedForZip) {
		t.Fatalf("expected ErrImportExistingNotAllowedForZip, got %v", err)
	}
}

func TestImportService_Zip_OneCalendarPerEntry(t *testing.T) {
	svc, _, calendars, userID, _ := newTestImportService(t)
	ctx := context.Background()

	otherICS := strings.ReplaceAll(singleEventICS, "foreign-uid-1", "foreign-uid-2")
	otherICS = strings.ReplaceAll(otherICS, "X-WR-CALNAME:Imported Calendar", "X-WR-CALNAME:Second Calendar")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range map[string]string{"a.ics": singleEventICS, "b.ics": otherICS} {
		entry, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := entry.Write([]byte(crlf(content))); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	summary, err := svc.Import(ctx, userID, "export.zip", buf.Bytes(), []ImportTarget{
		{Filename: "a.ics", Action: ImportTargetNew},
		{Filename: "b.ics", Action: ImportTargetNew},
	}, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(summary.Files) != 2 {
		t.Fatalf("expected 2 file summaries, got %d", len(summary.Files))
	}
	if summary.Files[0].CalendarID == summary.Files[1].CalendarID {
		t.Fatalf("expected a distinct calendar per zip entry, got %+v", summary.Files)
	}

	cals, err := calendars.List(ctx, userID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}
	if len(cals) != 3 { // pre-existing + 2 new
		t.Fatalf("expected 3 calendars, got %+v", cals)
	}
}

func TestImportService_MissingTarget(t *testing.T) {
	svc, _, _, userID, _ := newTestImportService(t)
	ctx := context.Background()

	_, err := svc.Import(ctx, userID, "invite.ics", []byte(crlf(singleEventICS)), nil, true)
	if !errors.Is(err, ErrImportTargetMissing) {
		t.Fatalf("expected ErrImportTargetMissing, got %v", err)
	}
}

func TestImportService_SkipAction_NotImported(t *testing.T) {
	svc, events, calendars, userID, _ := newTestImportService(t)
	ctx := context.Background()

	summary, err := svc.Import(ctx, userID, "invite.ics", []byte(crlf(singleEventICS)), []ImportTarget{
		{Filename: "invite.ics", Action: ImportTargetSkip},
	}, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(summary.Files) != 0 {
		t.Fatalf("expected no file summaries for a skipped file, got %+v", summary.Files)
	}

	cals, _ := calendars.List(ctx, userID)
	if len(cals) != 1 {
		t.Fatalf("expected no calendar created for a skipped file, got %+v", cals)
	}
	all, _ := events.List(ctx, userID, nil, nil)
	if len(all) != 0 {
		t.Fatalf("expected no events written for a skipped file, got %+v", all)
	}
}

func TestImportService_SkippedAndAdjustedCountsSurfaceInSummary(t *testing.T) {
	svc, _, _, userID, _ := newTestImportService(t)
	ctx := context.Background()

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
BEGIN:VEVENT
UID:outlook-tz
DTSTART;TZID=Eastern Standard Time:20260601T090000
DTEND;TZID=Eastern Standard Time:20260601T093000
SUMMARY:Outlook event
END:VEVENT
END:VCALENDAR
`
	summary, err := svc.Import(ctx, userID, "mixed.ics", []byte(crlf(ics)), []ImportTarget{
		{Filename: "mixed.ics", Action: ImportTargetNew},
	}, true)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	fs := summary.Files[0]
	if len(fs.Skipped) != 1 || fs.Skipped[0].Reason != "cancelled" || fs.Skipped[0].Count != 1 {
		t.Fatalf("expected 1 cancelled skip, got %+v", fs.Skipped)
	}
	if len(fs.Adjusted) != 1 || fs.Adjusted[0].Reason != AdjustedFloatingDowngrade || fs.Adjusted[0].Count != 1 {
		t.Fatalf("expected 1 floating downgrade, got %+v", fs.Adjusted)
	}
	if fs.EventCount != 1 {
		t.Fatalf("expected 1 imported event (the outlook one), got %d", fs.EventCount)
	}
}

func TestImportService_UnsupportedFileType(t *testing.T) {
	svc, _, _, userID, _ := newTestImportService(t)
	ctx := context.Background()

	_, err := svc.Import(ctx, userID, "notes.txt", []byte("hello"), nil, true)
	if !errors.Is(err, ErrImportUnsupportedFileType) {
		t.Fatalf("expected ErrImportUnsupportedFileType, got %v", err)
	}
}

// blankTitleICS has a VEVENT with no SUMMARY at all — a case
// icalendar.ParseImportFile does not skip (an absent SUMMARY isn't one of
// ADR-0030's named skip reasons), so it reaches ImportSeries' own
// validateEventFields and is rejected as ErrInvalidTitle.
const blankTitleICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:no-title
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
END:VEVENT
END:VCALENDAR
`

func TestImportService_DryRun_ReportsSameValidationErrorAsRealRun(t *testing.T) {
	svc, _, _, userID, _ := newTestImportService(t)
	ctx := context.Background()

	target := []ImportTarget{{Filename: "invite.ics", Action: ImportTargetNew}}

	_, dryErr := svc.Import(ctx, userID, "invite.ics", []byte(crlf(blankTitleICS)), target, true)
	if !errors.Is(dryErr, ErrInvalidTitle) {
		t.Fatalf("expected a dry run to surface ErrInvalidTitle, got %v", dryErr)
	}

	_, realErr := svc.Import(ctx, userID, "invite.ics", []byte(crlf(blankTitleICS)), target, false)
	if !errors.Is(realErr, ErrInvalidTitle) {
		t.Fatalf("expected a real run to surface ErrInvalidTitle, got %v", realErr)
	}
}

func TestImportService_LaterFileInvalid_EarlierFileNotWritten(t *testing.T) {
	svc, events, calendars, userID, _ := newTestImportService(t)
	ctx := context.Background()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range map[string]string{"a.ics": singleEventICS, "b.ics": blankTitleICS} {
		entry, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := entry.Write([]byte(crlf(content))); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	_, err := svc.Import(ctx, userID, "export.zip", buf.Bytes(), []ImportTarget{
		{Filename: "a.ics", Action: ImportTargetNew},
		{Filename: "b.ics", Action: ImportTargetNew},
	}, false)
	if !errors.Is(err, ErrInvalidTitle) {
		t.Fatalf("expected ErrInvalidTitle from the second entry, got %v", err)
	}

	cals, err := calendars.List(ctx, userID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}
	if len(cals) != 1 { // just the pre-existing "Existing" calendar
		t.Fatalf("expected no calendar created for either zip entry, got %+v", cals)
	}
	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected nothing written for the first entry once the second fails validation, got %+v", all)
	}
}

// methodRequestICS is a single-VEVENT emailed invite carrying a
// calendar-level METHOD:REQUEST, per #77's "Test with" list. Import doesn't
// special-case METHOD — it isn't part of ADR-0030's model — so the VEVENT
// should just import like any other.
const methodRequestICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
METHOD:REQUEST
BEGIN:VEVENT
UID:invite-1
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
SUMMARY:Project kickoff
ORGANIZER:mailto:alice@example.com
END:VEVENT
END:VCALENDAR
`

func TestImportService_MethodRequestInvite_ImportsNormally(t *testing.T) {
	svc, events, _, userID, _ := newTestImportService(t)
	ctx := context.Background()

	summary, err := svc.Import(ctx, userID, "invite.ics", []byte(crlf(methodRequestICS)), []ImportTarget{
		{Filename: "invite.ics", Action: ImportTargetNew},
	}, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.Files[0].EventCount != 1 {
		t.Fatalf("expected 1 imported event, got %+v", summary.Files[0])
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(all) != 1 || all[0].Title != "Project kickoff" {
		t.Fatalf("expected the invite's event to import, got %+v", all)
	}
}
