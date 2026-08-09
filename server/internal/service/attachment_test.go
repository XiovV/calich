package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/attachmentstore"
	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// attachmentTestFixture mirrors eventShareFixture (event_share_test.go): an
// Owner's Calendar shared as Editor to one User and Viewer to another, plus
// a stranger with no Access, so Attachments' ADR-0034 gating (Owner/Editor
// add and remove, Viewer downloads only, a stranger sees nothing) can be
// exercised directly against AttachmentService.
type attachmentTestFixture struct {
	attachments                             *AttachmentService
	events                                  *EventService
	calendars                               *CalendarService
	sqlDB                                   *sql.DB
	ownerID, editorID, viewerID, strangerID int64
	calendarID                              string
	masterID                                string
}

func newAttachmentTestFixture(t *testing.T, maxPerEvent int) attachmentTestFixture {
	t.Helper()
	ctx := context.Background()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	owner, err := users.Create(ctx, "owner", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	editor, err := users.Create(ctx, "editor", "hash", false)
	if err != nil {
		t.Fatalf("create editor: %v", err)
	}
	viewer, err := users.Create(ctx, "viewer", "hash", false)
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	stranger, err := users.Create(ctx, "stranger", "hash", false)
	if err != nil {
		t.Fatalf("create stranger: %v", err)
	}

	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspace, err := workspaceRepo.Create(ctx, "Test Workspace", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, owner.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}
	// A Share can only reach someone already inside the Calendar's own
	// Workspace (#159, ADR-0045) — stranger is deliberately left out.
	if err := workspaceRepo.AddMember(ctx, workspace.ID, editor.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add editor as workspace member: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, viewer.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add viewer as workspace member: %v", err)
	}

	calendars := NewCalendarService(repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	cal, err := calendars.Create(ctx, owner.ID, workspace.ID, "cal-1", CalendarWrite{Name: "Family", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	if _, err := calendars.Share(ctx, owner.ID, cal.ID, "editor", repository.RoleEditor); err != nil {
		t.Fatalf("share editor: %v", err)
	}
	if _, err := calendars.Share(ctx, owner.ID, cal.ID, "viewer", repository.RoleViewer); err != nil {
		t.Fatalf("share viewer: %v", err)
	}

	attachmentsRepo := repository.NewAttachmentRepository(sqlDB)
	eventsRepo := repository.NewEventRepository(sqlDB)
	events := NewEventService(sqlDB, eventsRepo, repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendars, users, attachmentsRepo)

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	master, err := events.Create(ctx, owner.ID, "evt-1", EventWrite{CalendarID: cal.ID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	store := attachmentstore.New(t.TempDir())
	attachments := NewAttachmentService(attachmentsRepo, eventsRepo, calendars, events, store, maxPerEvent)

	return attachmentTestFixture{
		attachments: attachments,
		events:      events,
		calendars:   calendars,
		sqlDB:       sqlDB,
		ownerID:     owner.ID, editorID: editor.ID, viewerID: viewer.ID, strangerID: stranger.ID,
		calendarID: cal.ID,
		masterID:   master.ID,
	}
}

func TestAttachmentService_Upload_OwnerAndEditorSucceed(t *testing.T) {
	f := newAttachmentTestFixture(t, 10)
	ctx := context.Background()

	if _, err := f.attachments.Upload(ctx, f.ownerID, f.masterID, "agenda.pdf", "application/pdf", strings.NewReader("hello")); err != nil {
		t.Fatalf("owner upload: %v", err)
	}
	if _, err := f.attachments.Upload(ctx, f.editorID, f.masterID, "notes.txt", "text/plain", strings.NewReader("hi")); err != nil {
		t.Fatalf("editor upload: %v", err)
	}
}

func TestAttachmentService_Upload_ViewerRefused(t *testing.T) {
	f := newAttachmentTestFixture(t, 10)
	ctx := context.Background()

	_, err := f.attachments.Upload(ctx, f.viewerID, f.masterID, "agenda.pdf", "application/pdf", strings.NewReader("hello"))
	if !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("expected ErrCalendarReadOnly, got %v", err)
	}
}

func TestAttachmentService_Upload_StrangerGetsNotFound(t *testing.T) {
	f := newAttachmentTestFixture(t, 10)
	ctx := context.Background()

	_, err := f.attachments.Upload(ctx, f.strangerID, f.masterID, "agenda.pdf", "application/pdf", strings.NewReader("hello"))
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAttachmentService_Upload_OnOverrideRejected(t *testing.T) {
	f := newAttachmentTestFixture(t, 10)
	ctx := context.Background()

	recurrenceID := time.Date(2026, 1, 8, 9, 0, 0, 0, time.UTC)
	override, err := f.events.Create(ctx, f.ownerID, "evt-1-override", EventWrite{
		CalendarID:   f.calendarID,
		Title:        "Standup (moved)",
		Start:        recurrenceID.Add(30 * time.Minute),
		End:          recurrenceID.Add(90 * time.Minute),
		ParentID:     &f.masterID,
		RecurrenceID: &recurrenceID,
	})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	_, err = f.attachments.Upload(ctx, f.ownerID, override.ID, "x.txt", "text/plain", strings.NewReader("x"))
	if !errors.Is(err, ErrAttachmentOnOverride) {
		t.Fatalf("expected ErrAttachmentOnOverride, got %v", err)
	}
}

func TestAttachmentService_Upload_EnforcesMaxPerEvent(t *testing.T) {
	f := newAttachmentTestFixture(t, 2)
	ctx := context.Background()

	if _, err := f.attachments.Upload(ctx, f.ownerID, f.masterID, "a.txt", "text/plain", strings.NewReader("a")); err != nil {
		t.Fatalf("upload 1: %v", err)
	}
	if _, err := f.attachments.Upload(ctx, f.ownerID, f.masterID, "b.txt", "text/plain", strings.NewReader("b")); err != nil {
		t.Fatalf("upload 2: %v", err)
	}

	_, err := f.attachments.Upload(ctx, f.ownerID, f.masterID, "c.txt", "text/plain", strings.NewReader("c"))
	if !errors.Is(err, ErrTooManyAttachments) {
		t.Fatalf("expected ErrTooManyAttachments, got %v", err)
	}
}

func TestAttachmentService_DeleteAndDownload(t *testing.T) {
	f := newAttachmentTestFixture(t, 10)
	ctx := context.Background()

	created, err := f.attachments.Upload(ctx, f.editorID, f.masterID, "agenda.pdf", "application/pdf", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	// A Viewer can download but not delete.
	_, file, err := f.attachments.Download(ctx, f.viewerID, created.ID)
	if err != nil {
		t.Fatalf("viewer download: %v", err)
	}
	file.Close()

	if err := f.attachments.Delete(ctx, f.viewerID, created.ID); !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("expected viewer delete to be refused with ErrCalendarReadOnly, got %v", err)
	}

	if err := f.attachments.Delete(ctx, f.ownerID, created.ID); err != nil {
		t.Fatalf("owner delete: %v", err)
	}

	if _, _, err := f.attachments.Download(ctx, f.ownerID, created.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

// TestAttachmentService_Replace_OwnerAndEditorSucceed_ViewerAndStrangerRefused
// is Replace's ADR-0034 gating (#133, RFC 8607 attachment-update): the same
// Owner/Editor-write, Viewer/stranger-refused shape Upload and Delete both
// already have.
func TestAttachmentService_Replace_OwnerAndEditorSucceed_ViewerAndStrangerRefused(t *testing.T) {
	f := newAttachmentTestFixture(t, 10)
	ctx := context.Background()

	created, err := f.attachments.Upload(ctx, f.ownerID, f.masterID, "agenda.pdf", "application/pdf", strings.NewReader("v1"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	if _, err := f.attachments.Replace(ctx, f.viewerID, created.ID, "agenda.pdf", "application/pdf", strings.NewReader("v2")); !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("expected viewer replace to be refused with ErrCalendarReadOnly, got %v", err)
	}
	if _, err := f.attachments.Replace(ctx, f.strangerID, created.ID, "agenda.pdf", "application/pdf", strings.NewReader("v2")); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected stranger replace to be refused with ErrNotFound, got %v", err)
	}

	updated, err := f.attachments.Replace(ctx, f.editorID, created.ID, "agenda-v2.pdf", "application/pdf", strings.NewReader("hello there"))
	if err != nil {
		t.Fatalf("editor replace: %v", err)
	}
	if updated.ID != created.ID {
		t.Fatalf("expected the managed-id to stay %q, got %q", created.ID, updated.ID)
	}
	if updated.Filename != "agenda-v2.pdf" {
		t.Fatalf("expected the filename to update, got %q", updated.Filename)
	}
	if updated.SizeBytes != int64(len("hello there")) {
		t.Fatalf("expected size_bytes to reflect the new bytes, got %d", updated.SizeBytes)
	}

	_, file, err := f.attachments.Download(ctx, f.ownerID, created.ID)
	if err != nil {
		t.Fatalf("download after replace: %v", err)
	}
	defer file.Close()
	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read after replace: %v", err)
	}
	if string(got) != "hello there" {
		t.Fatalf("expected the replaced bytes on disk, got %q", got)
	}
}

func TestAttachmentService_Download_StrangerGetsNotFound(t *testing.T) {
	f := newAttachmentTestFixture(t, 10)
	ctx := context.Background()

	created, err := f.attachments.Upload(ctx, f.ownerID, f.masterID, "x.txt", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	_, _, err = f.attachments.Download(ctx, f.strangerID, created.ID)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestAttachmentService_Upload_SubscribedCalendarRefused is the acceptance
// criterion "A Subscribed Calendar rejects uploads": ADR-0032 clamps every
// Access to a Subscribed Calendar to Viewer, Owner included, so an upload —
// gated the same way every other write is, through
// CalendarService.Access/CanWrite — must be refused even for the Owner.
func TestAttachmentService_Upload_SubscribedCalendarRefused(t *testing.T) {
	ctx := context.Background()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	owner, err := users.Create(ctx, "owner", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspace, err := workspaceRepo.Create(ctx, "Test Workspace", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, owner.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}

	calendarRepo := repository.NewCalendarRepository(sqlDB)
	sourceURL := "https://example.com/feed.ics"
	cal, err := calendarRepo.Create(ctx, owner.ID, workspace.ID, "cal-1", repository.CalendarFields{Name: "Feed", Color: "peacock", SourceURL: &sourceURL})
	if err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}

	calendars := NewCalendarService(calendarRepo, repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	attachmentsRepo := repository.NewAttachmentRepository(sqlDB)
	eventsRepo := repository.NewEventRepository(sqlDB)
	events := NewEventService(sqlDB, eventsRepo, repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendars, users, attachmentsRepo)

	// ImportSubscribedSeries is the only writer a Subscribed Calendar's
	// Events go through (ADR-0032) — reach past the write guard the same
	// way a real Refresh would to seed a Master to attach against.
	if _, err := events.ImportSubscribedSeries(ctx, owner.ID, cal.ID, []SeriesWrite{
		{
			Title:       "Standup",
			Start:       time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
			End:         time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
			ExternalUID: "foreign-uid-1",
		},
	}); err != nil {
		t.Fatalf("seed subscribed event: %v", err)
	}
	list, err := events.List(ctx, owner.ID, nil, nil)
	if err != nil || len(list) != 1 {
		t.Fatalf("list seeded event: %v, %+v", err, list)
	}

	attachments := NewAttachmentService(attachmentsRepo, eventsRepo, calendars, events, attachmentstore.New(t.TempDir()), 10)

	_, err = attachments.Upload(ctx, owner.ID, list[0].ID, "x.txt", "text/plain", strings.NewReader("x"))
	if !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("expected ErrCalendarReadOnly for a Subscribed Calendar, got %v", err)
	}
}

func TestEventService_ListAndGet_AttachmentsShowOnMasterOnly(t *testing.T) {
	f := newAttachmentTestFixture(t, 10)
	ctx := context.Background()

	created, err := f.attachments.Upload(ctx, f.ownerID, f.masterID, "agenda.pdf", "application/pdf", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	got, err := f.events.Get(ctx, f.ownerID, f.masterID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].ID != created.ID {
		t.Fatalf("expected the uploaded attachment on the master, got %+v", got.Attachments)
	}
	if got.Attachments[0].UploadedByUsername != "owner" {
		t.Fatalf("expected UploadedByUsername = owner, got %q", got.Attachments[0].UploadedByUsername)
	}

	list, err := f.events.List(ctx, f.ownerID, nil, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var master repository.Event
	for _, e := range list {
		if e.ID == f.masterID {
			master = e
		}
	}
	if len(master.Attachments) != 1 {
		t.Fatalf("expected the attachment to appear in List too, got %+v", master.Attachments)
	}
}
