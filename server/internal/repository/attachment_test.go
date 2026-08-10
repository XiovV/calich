package repository

import (
	"context"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
)

// newTestAttachmentRepository returns an EventRepository and
// AttachmentRepository sharing one in-memory database, plus a real user id
// and calendar id to satisfy events' foreign keys.
func newTestAttachmentRepository(t *testing.T) (repo *EventRepository, attachments *AttachmentRepository, userID int64, calendarID string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := NewUserRepository(sqlDB)
	user, err := users.Create(context.Background(), "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	workspaces := NewWorkspaceRepository(sqlDB)
	workspace, err := workspaces.Create(context.Background(), "workspace-a", user.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaces.AddMember(context.Background(), workspace.ID, user.ID, WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}

	calendars := NewCalendarRepository(sqlDB)
	cal, err := calendars.Create(context.Background(), user.ID, workspace.ID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	return NewEventRepository(sqlDB), NewAttachmentRepository(sqlDB), user.ID, cal.ID
}

func TestAttachmentRepository_CreateAndGetByID(t *testing.T) {
	repo, attachments, userID, calendarID := newTestAttachmentRepository(t)
	ctx := context.Background()
	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	created, err := attachments.Create(ctx, "att-1", "evt-1", &userID, "agenda.pdf", "application/pdf", 1024)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Filename != "agenda.pdf" || created.ContentType != "application/pdf" || created.SizeBytes != 1024 {
		t.Fatalf("unexpected attachment: %+v", created)
	}
	if created.UploadedBy == nil || *created.UploadedBy != userID {
		t.Fatalf("expected UploadedBy = %d, got %+v", userID, created.UploadedBy)
	}

	got, err := attachments.GetByID(ctx, "att-1")
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.ID != "att-1" || got.EventID != "evt-1" {
		t.Fatalf("unexpected attachment: %+v", got)
	}
}

func TestAttachmentRepository_Update(t *testing.T) {
	repo, attachments, userID, calendarID := newTestAttachmentRepository(t)
	ctx := context.Background()
	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	created, err := attachments.Create(ctx, "att-1", "evt-1", &userID, "agenda.pdf", "application/pdf", 1024)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := attachments.Update(ctx, "att-1", "agenda-v2.pdf", "application/octet-stream", 2048)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Filename != "agenda-v2.pdf" || updated.ContentType != "application/octet-stream" || updated.SizeBytes != 2048 {
		t.Fatalf("unexpected attachment after update: %+v", updated)
	}
	if updated.UploadedBy == nil || *updated.UploadedBy != *created.UploadedBy {
		t.Fatalf("expected uploaded_by to survive an update, got %+v", updated.UploadedBy)
	}
}

func TestAttachmentRepository_Update_NotFound(t *testing.T) {
	_, attachments, _, _ := newTestAttachmentRepository(t)
	if _, err := attachments.Update(context.Background(), "missing", "a.txt", "text/plain", 1); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAttachmentRepository_GetByID_NotFound(t *testing.T) {
	_, attachments, _, _ := newTestAttachmentRepository(t)
	if _, err := attachments.GetByID(context.Background(), "missing"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAttachmentRepository_CountByEventID(t *testing.T) {
	repo, attachments, userID, calendarID := newTestAttachmentRepository(t)
	ctx := context.Background()
	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")

	for _, id := range []string{"att-1", "att-2", "att-3"} {
		if _, err := attachments.Create(ctx, id, "evt-1", &userID, "f.txt", "text/plain", 1); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	count, err := attachments.CountByEventID(ctx, "evt-1")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
}

func TestAttachmentRepository_ListByEventIDs(t *testing.T) {
	repo, attachments, userID, calendarID := newTestAttachmentRepository(t)
	ctx := context.Background()
	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	mustCreateEvent(t, repo, "evt-2", userID, calendarID, "2026-01-02T09:00:00Z", "2026-01-02T10:00:00Z")

	if _, err := attachments.Create(ctx, "att-1", "evt-1", &userID, "a.txt", "text/plain", 1); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := attachments.Create(ctx, "att-2", "evt-1", &userID, "b.txt", "text/plain", 2); err != nil {
		t.Fatalf("create: %v", err)
	}

	byEvent, err := attachments.ListByEventIDs(ctx, []string{"evt-1", "evt-2"})
	if err != nil {
		t.Fatalf("list by event ids: %v", err)
	}
	if len(byEvent["evt-1"]) != 2 {
		t.Fatalf("expected 2 attachments for evt-1, got %+v", byEvent["evt-1"])
	}
	if _, ok := byEvent["evt-2"]; ok {
		t.Fatalf("expected evt-2 absent, got %+v", byEvent["evt-2"])
	}
}

func TestAttachmentRepository_ListByEventIDs_EmptyInput(t *testing.T) {
	_, attachments, _, _ := newTestAttachmentRepository(t)
	byEvent, err := attachments.ListByEventIDs(context.Background(), nil)
	if err != nil {
		t.Fatalf("list by event ids: %v", err)
	}
	if len(byEvent) != 0 {
		t.Fatalf("expected empty map, got %+v", byEvent)
	}
}

func TestAttachmentRepository_Delete(t *testing.T) {
	repo, attachments, userID, calendarID := newTestAttachmentRepository(t)
	ctx := context.Background()
	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	if _, err := attachments.Create(ctx, "att-1", "evt-1", &userID, "a.txt", "text/plain", 1); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := attachments.Delete(ctx, "att-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := attachments.GetByID(ctx, "att-1"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestAttachmentRepository_Delete_NotFound(t *testing.T) {
	_, attachments, _, _ := newTestAttachmentRepository(t)
	if err := attachments.Delete(context.Background(), "missing"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAttachmentRepository_CascadeDeletesWhenEventDeleted(t *testing.T) {
	repo, attachments, userID, calendarID := newTestAttachmentRepository(t)
	ctx := context.Background()
	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	if _, err := attachments.Create(ctx, "att-1", "evt-1", &userID, "a.txt", "text/plain", 1); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := repo.Delete(ctx, "evt-1"); err != nil {
		t.Fatalf("delete event: %v", err)
	}

	if _, err := attachments.GetByID(ctx, "att-1"); err != ErrNotFound {
		t.Fatalf("expected the attachment row to be cascade-deleted with its event, got %v", err)
	}
}

// TestAttachmentRepository_UploaderDeletedSetsNull mirrors
// TestEventRepository_CreatedByPreservedWhenCreatingUserDeleted: uploaded_by
// is attribution only, so deleting the uploader must not take the
// Attachment down with them — ON DELETE SET NULL, not CASCADE.
func TestAttachmentRepository_UploaderDeletedSetsNull(t *testing.T) {
	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	ctx := context.Background()

	users := NewUserRepository(sqlDB)
	owner, err := users.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	uploader, err := users.Create(ctx, "uploader", "uploader@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create uploader: %v", err)
	}

	workspaces := NewWorkspaceRepository(sqlDB)
	workspace, err := workspaces.Create(ctx, "workspace-a", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaces.AddMember(ctx, workspace.ID, owner.ID, WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}

	calendars := NewCalendarRepository(sqlDB)
	cal, err := calendars.Create(ctx, owner.ID, workspace.ID, "cal-1", CalendarFields{Name: "Family", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	events := NewEventRepository(sqlDB)
	if _, err := events.Create(ctx, "evt-1", &owner.ID, EventFields{CalendarID: cal.ID, Title: "Standup", Start: mustParseTime(t, "2026-01-01T09:00:00Z"), End: mustParseTime(t, "2026-01-01T10:00:00Z")}, 0); err != nil {
		t.Fatalf("create event: %v", err)
	}

	attachments := NewAttachmentRepository(sqlDB)
	if _, err := attachments.Create(ctx, "att-1", "evt-1", &uploader.ID, "a.txt", "text/plain", 1); err != nil {
		t.Fatalf("create attachment: %v", err)
	}

	if _, err := sqlDB.ExecContext(ctx, "DELETE FROM users WHERE id = ?", uploader.ID); err != nil {
		t.Fatalf("delete uploader: %v", err)
	}

	fetched, err := attachments.GetByID(ctx, "att-1")
	if err != nil {
		t.Fatalf("expected the attachment to survive its uploader's deletion, got %v", err)
	}
	if fetched.UploadedBy != nil {
		t.Fatalf("expected uploaded_by to be cleared to nil, got %+v", fetched.UploadedBy)
	}
}

func TestAttachmentRepository_ListAllIDs(t *testing.T) {
	repo, attachments, userID, calendarID := newTestAttachmentRepository(t)
	ctx := context.Background()
	mustCreateEvent(t, repo, "evt-1", userID, calendarID, "2026-01-01T09:00:00Z", "2026-01-01T10:00:00Z")
	if _, err := attachments.Create(ctx, "att-1", "evt-1", &userID, "a.txt", "text/plain", 1); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := attachments.Create(ctx, "att-2", "evt-1", &userID, "b.txt", "text/plain", 1); err != nil {
		t.Fatalf("create: %v", err)
	}

	ids, err := attachments.ListAllIDs(ctx)
	if err != nil {
		t.Fatalf("list all ids: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 ids, got %+v", ids)
	}
}
