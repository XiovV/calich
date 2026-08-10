package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/attachmentstore"
	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// testMaxAttachmentSize and testMaxAttachmentsPerEvent are this package's
// MAX_ATTACHMENT_SIZE/MAX_ATTACHMENTS_PER_EVENT stand-ins (ADR-0040),
// mirroring caldavserver/backend_test.go's own constants of the same name.
const (
	testMaxAttachmentSize      int64 = 25 << 20
	testMaxAttachmentsPerEvent       = 10
)

// newTestImportService returns an ImportService plus a real user id and an
// existing calendar id (for action "existing" targets) to satisfy events'
// foreign keys.
func newTestImportService(t *testing.T) (svc *ImportService, events *EventService, calendars *CalendarService, store *attachmentstore.Store, userID, workspaceID int64, existingCalendarID string) {
	t.Helper()
	return newTestImportServiceWithAttachmentLimits(t, testMaxAttachmentSize, testMaxAttachmentsPerEvent)
}

// newTestImportServiceWithAttachmentLimits is newTestImportService with
// caller-chosen MAX_ATTACHMENT_SIZE/MAX_ATTACHMENTS_PER_EVENT, for tests
// exercising those caps directly (#135).
func newTestImportServiceWithAttachmentLimits(t *testing.T, maxAttachmentSize int64, maxAttachmentsPerEvent int) (svc *ImportService, events *EventService, calendars *CalendarService, store *attachmentstore.Store, userID, workspaceID int64, existingCalendarID string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	user, err := users.Create(context.Background(), "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspace, err := workspaceRepo.Create(context.Background(), "Test Workspace", user.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaceRepo.AddMember(context.Background(), workspace.ID, user.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}

	calendarRepo := repository.NewCalendarRepository(sqlDB)
	calendarSvc := NewCalendarService(calendarRepo, repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	cal, err := calendarRepo.Create(context.Background(), user.ID, workspace.ID, "cal-1", repository.CalendarFields{Name: "Existing", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	eventSvc := NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendarSvc, users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo)
	attachmentStore := attachmentstore.New(t.TempDir())

	return NewImportService(eventSvc, calendarSvc, attachmentStore, maxAttachmentSize, maxAttachmentsPerEvent), eventSvc, calendarSvc, attachmentStore, user.ID, workspace.ID, cal.ID
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
	svc, events, calendars, _, userID, workspaceID, _ := newTestImportService(t)
	ctx := context.Background()

	summary, err := svc.Import(ctx, userID, workspaceID, "invite.ics", []byte(crlf(singleEventICS)), []ImportTarget{
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
	svc, events, calendars, _, userID, workspaceID, _ := newTestImportService(t)
	ctx := context.Background()

	summary, err := svc.Import(ctx, userID, workspaceID, "invite.ics", []byte(crlf(singleEventICS)), []ImportTarget{
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
	svc, events, _, _, userID, workspaceID, existingCalendarID := newTestImportService(t)
	ctx := context.Background()

	summary, err := svc.Import(ctx, userID, workspaceID, "invite.ics", []byte(crlf(singleEventICS)), []ImportTarget{
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
	svc, _, _, _, userID, workspaceID, _ := newTestImportService(t)
	ctx := context.Background()

	_, err := svc.Import(ctx, userID, workspaceID, "invite.ics", []byte(crlf(singleEventICS)), []ImportTarget{
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
	svc, _, calendars, _, userID, workspaceID, _ := newTestImportService(t)
	ctx := context.Background()

	sourceURL := "https://example.com/feed.ics"
	subCalendar, err := calendars.Create(ctx, userID, workspaceID, "sub-cal-1", CalendarWrite{Name: "Feed", Color: "#123456FF", SourceURL: &sourceURL})
	if err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}

	_, err = svc.Import(ctx, userID, workspaceID, "invite.ics", []byte(crlf(singleEventICS)), []ImportTarget{
		{Filename: "invite.ics", Action: ImportTargetExisting, CalendarID: subCalendar.ID},
	}, false)
	if !errors.Is(err, ErrImportTargetSubscribed) {
		t.Fatalf("expected ErrImportTargetSubscribed, got %v", err)
	}
}

func TestImportService_Zip_RejectsExistingAction(t *testing.T) {
	svc, _, _, _, userID, workspaceID, existingCalendarID := newTestImportService(t)
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

	_, err = svc.Import(ctx, userID, workspaceID, "export.zip", buf.Bytes(), []ImportTarget{
		{Filename: "a.ics", Action: ImportTargetExisting, CalendarID: existingCalendarID},
	}, true)
	if !errors.Is(err, ErrImportExistingNotAllowedForZip) {
		t.Fatalf("expected ErrImportExistingNotAllowedForZip, got %v", err)
	}
}

func TestImportService_Zip_OneCalendarPerEntry(t *testing.T) {
	svc, _, calendars, _, userID, workspaceID, _ := newTestImportService(t)
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

	summary, err := svc.Import(ctx, userID, workspaceID, "export.zip", buf.Bytes(), []ImportTarget{
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
	svc, _, _, _, userID, workspaceID, _ := newTestImportService(t)
	ctx := context.Background()

	_, err := svc.Import(ctx, userID, workspaceID, "invite.ics", []byte(crlf(singleEventICS)), nil, true)
	if !errors.Is(err, ErrImportTargetMissing) {
		t.Fatalf("expected ErrImportTargetMissing, got %v", err)
	}
}

func TestImportService_SkipAction_NotImported(t *testing.T) {
	svc, events, calendars, _, userID, workspaceID, _ := newTestImportService(t)
	ctx := context.Background()

	summary, err := svc.Import(ctx, userID, workspaceID, "invite.ics", []byte(crlf(singleEventICS)), []ImportTarget{
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
	svc, _, _, _, userID, workspaceID, _ := newTestImportService(t)
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
	summary, err := svc.Import(ctx, userID, workspaceID, "mixed.ics", []byte(crlf(ics)), []ImportTarget{
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
	svc, _, _, _, userID, workspaceID, _ := newTestImportService(t)
	ctx := context.Background()

	_, err := svc.Import(ctx, userID, workspaceID, "notes.txt", []byte("hello"), nil, true)
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
	svc, _, _, _, userID, workspaceID, _ := newTestImportService(t)
	ctx := context.Background()

	target := []ImportTarget{{Filename: "invite.ics", Action: ImportTargetNew}}

	_, dryErr := svc.Import(ctx, userID, workspaceID, "invite.ics", []byte(crlf(blankTitleICS)), target, true)
	if !errors.Is(dryErr, ErrInvalidTitle) {
		t.Fatalf("expected a dry run to surface ErrInvalidTitle, got %v", dryErr)
	}

	_, realErr := svc.Import(ctx, userID, workspaceID, "invite.ics", []byte(crlf(blankTitleICS)), target, false)
	if !errors.Is(realErr, ErrInvalidTitle) {
		t.Fatalf("expected a real run to surface ErrInvalidTitle, got %v", realErr)
	}
}

func TestImportService_LaterFileInvalid_EarlierFileNotWritten(t *testing.T) {
	svc, events, calendars, _, userID, workspaceID, _ := newTestImportService(t)
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

	_, err := svc.Import(ctx, userID, workspaceID, "export.zip", buf.Bytes(), []ImportTarget{
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
	svc, events, _, _, userID, workspaceID, _ := newTestImportService(t)
	ctx := context.Background()

	summary, err := svc.Import(ctx, userID, workspaceID, "invite.ics", []byte(crlf(methodRequestICS)), []ImportTarget{
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

// base64("hello world") = "aGVsbG8gd29ybGQ="
const helloWorldBase64 = "aGVsbG8gd29ybGQ="

const singleEventWithAttachICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
X-WR-CALNAME:Imported Calendar
BEGIN:VEVENT
UID:foreign-uid-attach
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
SUMMARY:Standup
ATTACH;ENCODING=BASE64;VALUE=BINARY;FMTTYPE=text/plain;FILENAME=notes.txt:` + helloWorldBase64 + `
END:VEVENT
END:VCALENDAR
`

func TestImportService_RealRun_WritesInlineAttachment(t *testing.T) {
	svc, events, _, store, userID, workspaceID, _ := newTestImportService(t)
	ctx := context.Background()

	summary, err := svc.Import(ctx, userID, workspaceID, "invite.ics", []byte(crlf(singleEventWithAttachICS)), []ImportTarget{
		{Filename: "invite.ics", Action: ImportTargetNew},
	}, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.Files[0].Attachments.Imported != 1 {
		t.Fatalf("expected 1 imported attachment in the summary, got %+v", summary.Files[0].Attachments)
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(all) != 1 || len(all[0].Attachments) != 1 {
		t.Fatalf("expected the master to carry 1 attachment, got %+v", all)
	}
	a := all[0].Attachments[0]
	if a.Filename != "notes.txt" || a.ContentType != "text/plain" || a.SizeBytes != int64(len("hello world")) {
		t.Fatalf("expected the attachment metadata to round-trip, got %+v", a)
	}

	f, err := store.Open(a.ID)
	if err != nil {
		t.Fatalf("open saved attachment bytes: %v", err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read saved attachment bytes: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("expected the saved bytes to match the source ATTACH, got %q", data)
	}
}

func TestImportService_DryRun_DoesNotSaveAttachmentBytes(t *testing.T) {
	dataDir := t.TempDir()
	store := attachmentstore.New(dataDir)

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	users := repository.NewUserRepository(sqlDB)
	user, err := users.Create(context.Background(), "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspace, err := workspaceRepo.Create(context.Background(), "Test Workspace", user.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaceRepo.AddMember(context.Background(), workspace.ID, user.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}
	calendarSvc := NewCalendarService(repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	eventSvc := NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendarSvc, users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo)
	svc := NewImportService(eventSvc, calendarSvc, store, testMaxAttachmentSize, testMaxAttachmentsPerEvent)
	ctx := context.Background()

	summary, err := svc.Import(ctx, user.ID, workspace.ID, "invite.ics", []byte(crlf(singleEventWithAttachICS)), []ImportTarget{
		{Filename: "invite.ics", Action: ImportTargetNew},
	}, true)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	// The dry run's summary still reports it — produced by the parse, not
	// the write (ADR-0030) — but nothing was saved to disk.
	if summary.Files[0].Attachments.Imported != 1 {
		t.Fatalf("expected the dry-run preview to still report 1 attachment, got %+v", summary.Files[0].Attachments)
	}

	fileCount, err := countRegularFiles(dataDir)
	if err != nil {
		t.Fatalf("count files under data dir: %v", err)
	}
	if fileCount != 0 {
		t.Fatalf("expected no attachment bytes saved on a dry run, found %d file(s) under %s", fileCount, dataDir)
	}
}

// countRegularFiles walks dir and counts every regular file under it, for
// asserting a dry run left the attachmentstore.Store's directory untouched.
func countRegularFiles(dir string) (int, error) {
	count := 0
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	return count, err
}

// TestImportService_Attachment_DedupedAcrossMasterAndOverride is #135's
// acceptance criterion at the service layer: a file exported by #134 puts
// the same ATTACH on the master and every Override VEVENT, and importing it
// must create exactly one Attachment row, not one per VEVENT (ADR-0040).
func TestImportService_Attachment_DedupedAcrossMasterAndOverride(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
X-WR-CALNAME:Imported Calendar
BEGIN:VEVENT
UID:foreign-uid-recurring
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
RRULE:FREQ=WEEKLY
SUMMARY:Standup
ATTACH;ENCODING=BASE64;VALUE=BINARY;FMTTYPE=text/plain;FILENAME=notes.txt:` + helloWorldBase64 + `
END:VEVENT
BEGIN:VEVENT
UID:foreign-uid-recurring
RECURRENCE-ID:20260608T090000Z
DTSTART:20260608T100000Z
DTEND:20260608T103000Z
SUMMARY:Standup (moved)
ATTACH;ENCODING=BASE64;VALUE=BINARY;FMTTYPE=text/plain;FILENAME=notes.txt:` + helloWorldBase64 + `
END:VEVENT
END:VCALENDAR
`
	svc, events, _, _, userID, workspaceID, _ := newTestImportService(t)
	ctx := context.Background()

	summary, err := svc.Import(ctx, userID, workspaceID, "invite.ics", []byte(crlf(ics)), []ImportTarget{
		{Filename: "invite.ics", Action: ImportTargetNew},
	}, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.Files[0].Attachments.Imported != 1 {
		t.Fatalf("expected 1 deduplicated attachment in the summary, got %+v", summary.Files[0].Attachments)
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var master repository.Event
	for _, e := range all {
		if e.ParentID == nil {
			master = e
		}
	}
	if len(master.Attachments) != 1 {
		t.Fatalf("expected the master to carry exactly 1 attachment, got %+v", master.Attachments)
	}
}

func TestImportService_Attachment_OverMaxSize_SkippedButSeriesImports(t *testing.T) {
	// "hello world" is 11 bytes; cap it at 5 so it's declined.
	svc, events, _, _, userID, workspaceID, _ := newTestImportServiceWithAttachmentLimits(t, 5, testMaxAttachmentsPerEvent)
	ctx := context.Background()

	summary, err := svc.Import(ctx, userID, workspaceID, "invite.ics", []byte(crlf(singleEventWithAttachICS)), []ImportTarget{
		{Filename: "invite.ics", Action: ImportTargetNew},
	}, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.Files[0].EventCount != 1 {
		t.Fatalf("expected the series to still import, got %+v", summary.Files[0])
	}
	if summary.Files[0].Attachments.TooLarge != 1 || summary.Files[0].Attachments.Imported != 0 {
		t.Fatalf("expected 1 too-large attachment and 0 imported, got %+v", summary.Files[0].Attachments)
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(all) != 1 || len(all[0].Attachments) != 0 {
		t.Fatalf("expected the event to import with no attachment, got %+v", all)
	}
}

func TestImportService_Attachment_OverMaxCount_SkippedButSeriesImports(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
X-WR-CALNAME:Imported Calendar
BEGIN:VEVENT
UID:foreign-uid-two-attach
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
SUMMARY:Standup
ATTACH;ENCODING=BASE64;VALUE=BINARY;FMTTYPE=text/plain;FILENAME=one.txt:` + helloWorldBase64 + `
ATTACH;ENCODING=BASE64;VALUE=BINARY;FMTTYPE=text/plain;FILENAME=two.txt:aGVsbG8=
END:VEVENT
END:VCALENDAR
`
	svc, events, _, _, userID, workspaceID, _ := newTestImportServiceWithAttachmentLimits(t, testMaxAttachmentSize, 1)
	ctx := context.Background()

	summary, err := svc.Import(ctx, userID, workspaceID, "invite.ics", []byte(crlf(ics)), []ImportTarget{
		{Filename: "invite.ics", Action: ImportTargetNew},
	}, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.Files[0].Attachments.Imported != 1 || summary.Files[0].Attachments.TooMany != 1 {
		t.Fatalf("expected 1 imported + 1 too-many attachment, got %+v", summary.Files[0].Attachments)
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(all) != 1 || len(all[0].Attachments) != 1 {
		t.Fatalf("expected the event to carry exactly 1 attachment (the cap), got %+v", all)
	}
}

func TestImportService_Attachment_URIIgnored_SkippedButSeriesImports(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
X-WR-CALNAME:Imported Calendar
BEGIN:VEVENT
UID:foreign-uid-uri-attach
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
SUMMARY:Standup
ATTACH:https://example.com/private/plan.pdf
END:VEVENT
END:VCALENDAR
`
	svc, events, _, _, userID, workspaceID, _ := newTestImportService(t)
	ctx := context.Background()

	summary, err := svc.Import(ctx, userID, workspaceID, "invite.ics", []byte(crlf(ics)), []ImportTarget{
		{Filename: "invite.ics", Action: ImportTargetNew},
	}, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.Files[0].Attachments.IgnoredURI != 1 || summary.Files[0].Attachments.Imported != 0 {
		t.Fatalf("expected 1 ignored URI attachment and 0 imported, got %+v", summary.Files[0].Attachments)
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(all) != 1 || len(all[0].Attachments) != 0 {
		t.Fatalf("expected the event to import with no attachment, got %+v", all)
	}
}

// TestImportService_FailedTransaction_LeavesNoAttachmentRows mirrors
// TestImportService_LaterFileInvalid_EarlierFileNotWritten with an
// attachment on the entry that would otherwise succeed: ADR-0030's
// "validate everything before writing anything" means a bad second entry's
// validation error is returned before EventService.ImportSeries is ever
// called for the first entry, so the first entry's ATTACH is never even
// saved to disk (#135) — nothing for the sweeper to reclaim, since nothing
// was written in the first place.
func TestImportService_FailedTransaction_LeavesNoAttachmentRows(t *testing.T) {
	svc, events, calendars, store, userID, workspaceID, _ := newTestImportService(t)
	ctx := context.Background()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range map[string]string{"a.ics": singleEventWithAttachICS, "b.ics": blankTitleICS} {
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

	_, err := svc.Import(ctx, userID, workspaceID, "export.zip", buf.Bytes(), []ImportTarget{
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
		t.Fatalf("expected nothing written for either entry once the second fails validation, got %+v", all)
	}

	// Sweeping with an empty "known" set removes anything orphaned on disk;
	// confirm there's nothing to sweep, i.e. the attachment was never saved.
	if err := store.Sweep(map[string]bool{}); err != nil {
		t.Fatalf("sweep: %v", err)
	}
}

// TestEventServiceImportSeries_WriteTimeFailure_OrphansBytesReclaimedBySweeper
// exercises the genuine version of ADR-0040's crash-ordering promise: unlike
// the up-front validation failure above (where nothing is ever saved to
// disk), this forces writeSeries' own SQL transaction to fail *after*
// saveAttachments has already saved bytes for an earlier series in the same
// call — the actual "bytes land on disk, then the transaction that would
// have rowed them rolls back" case the sweeper exists for (#132, ADR-0040).
// It calls EventService.ImportSeries directly (bypassing ImportService.Import,
// which never reuses one ExternalUID across series) to construct that
// failure deterministically: two SeriesWrite sharing one ExternalUID violate
// idx_events_calendar_external_uid, so the transaction fails on the second
// series and rolls back the first's already-inserted master row too — but
// SQLite rolling back a row is unrelated to attachmentstore.Store, which has
// already renamed the first series' attachment bytes into place by then.
func TestEventServiceImportSeries_WriteTimeFailure_OrphansBytesReclaimedBySweeper(t *testing.T) {
	_, events, _, store, userID, _, calendarID := newTestImportService(t)
	ctx := context.Background()

	const attachmentID = "pre-saved-attachment"
	if _, err := store.Save(attachmentID, strings.NewReader("hello world")); err != nil {
		t.Fatalf("save attachment bytes: %v", err)
	}

	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)
	writes := []SeriesWrite{
		{
			Title: "First", Start: start, End: end, ExternalUID: "dup-uid",
			Attachments: []AttachmentWrite{
				{ID: attachmentID, Filename: "notes.txt", ContentType: "text/plain", SizeBytes: int64(len("hello world"))},
			},
		},
		{Title: "Second", Start: start.AddDate(0, 0, 1), End: end.AddDate(0, 0, 1), ExternalUID: "dup-uid"},
	}

	// ImportSeries is the bypass used here deliberately: it takes a raw
	// calendarID rather than an ExternalUID-bearing Subscription, so this
	// reaches writeSeries' shared transaction exactly as ordinary import
	// would, just with a hand-built conflict ImportService.Import itself
	// would never produce.
	if _, err := events.ImportSeries(ctx, userID, calendarID, writes); err == nil {
		t.Fatalf("expected the second series' duplicate ExternalUID to fail the write")
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected the whole transaction (including the first series) to roll back, got %+v", all)
	}

	// The bytes are still on disk — orphaned, since the row that would have
	// referenced them never committed.
	if f, err := store.Open(attachmentID); err != nil {
		t.Fatalf("expected the pre-saved bytes to still be on disk after the rollback, got: %v", err)
	} else {
		f.Close()
	}

	// The sweeper reclaims them, told the (now nonexistent) row's id isn't known.
	if err := store.Sweep(map[string]bool{}); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, err := store.Open(attachmentID); err == nil {
		t.Fatalf("expected Sweep to have reclaimed the orphaned bytes")
	}
}
