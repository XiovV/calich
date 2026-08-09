package service

import (
	"context"
	"strings"
	"testing"
)

// TestAttachmentSweeper_NeverDeletesAFileWithARow is the acceptance
// criterion's own wording: the sweeper must never delete a file that has a
// row, only files that don't.
func TestAttachmentSweeper_NeverDeletesAFileWithARow(t *testing.T) {
	f := newAttachmentTestFixture(t, 10)
	ctx := context.Background()

	kept, err := f.attachments.Upload(ctx, f.ownerID, f.masterID, "keep.txt", "text/plain", strings.NewReader("keep"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	sweeper := NewAttachmentSweeper(f.attachments.attachments, f.attachments.store)
	if err := sweeper.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if _, _, err := f.attachments.Download(ctx, f.ownerID, kept.ID); err != nil {
		t.Fatalf("expected the attachment with a row to survive the sweep: %v", err)
	}
}

// TestAttachmentSweeper_ReclaimsOrphanFromCrashBetweenSaveAndCommit is the
// "crash between file write and row commit" acceptance criterion: a file on
// disk with no row is exactly what a crash there leaves, and the sweeper
// must reclaim it.
func TestAttachmentSweeper_ReclaimsOrphanFromCrashBetweenSaveAndCommit(t *testing.T) {
	f := newAttachmentTestFixture(t, 10)
	ctx := context.Background()

	orphanID := "aabbccdd-0000-0000-0000-000000000000"
	if _, err := f.attachments.store.Save(orphanID, strings.NewReader("orphan")); err != nil {
		t.Fatalf("save orphan file directly (simulating a crash before the row commit): %v", err)
	}

	sweeper := NewAttachmentSweeper(f.attachments.attachments, f.attachments.store)
	if err := sweeper.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if _, err := f.attachments.store.Open(orphanID); err == nil {
		t.Fatal("expected the orphaned file to be reclaimed by the sweep")
	}
}

// TestAttachmentSweeper_ReclaimsFilesLeftByCascadeDelete covers the sweeper
// catching a Calendar/series/account cascade that removed
// event_attachments rows inside SQLite with no Go code watching
// file-by-file (ADR-0040) — here simulated by deleting the Event directly,
// which cascades the Attachment row via ON DELETE CASCADE but leaves the
// file on disk untouched.
func TestAttachmentSweeper_ReclaimsFilesLeftByCascadeDelete(t *testing.T) {
	f := newAttachmentTestFixture(t, 10)
	ctx := context.Background()

	created, err := f.attachments.Upload(ctx, f.ownerID, f.masterID, "x.txt", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	if err := f.events.Delete(ctx, f.ownerID, f.masterID); err != nil {
		t.Fatalf("delete event: %v", err)
	}

	// The row is already gone via cascade; the file is still on disk until
	// the sweep runs.
	if _, err := f.attachments.store.Open(created.ID); err != nil {
		t.Fatalf("expected the file to still be on disk right after the cascade delete: %v", err)
	}

	sweeper := NewAttachmentSweeper(f.attachments.attachments, f.attachments.store)
	if err := sweeper.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if _, err := f.attachments.store.Open(created.ID); err == nil {
		t.Fatal("expected the file to be reclaimed by the sweep after its row cascade-deleted")
	}
}

// TestAttachmentSweeper_ReclaimsFilesLeftByCalendarDeletion is the
// acceptance criterion "Deleting a Calendar leaves no files behind after a
// sweep": CalendarService.Delete cascades calendars -> events ->
// event_attachments entirely inside SQLite, with no Go code seeing the
// Attachment row go — the sweep is what reclaims its file.
func TestAttachmentSweeper_ReclaimsFilesLeftByCalendarDeletion(t *testing.T) {
	f := newAttachmentTestFixture(t, 10)
	ctx := context.Background()

	created, err := f.attachments.Upload(ctx, f.ownerID, f.masterID, "x.txt", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	if err := f.calendars.Delete(ctx, f.ownerID, f.calendarID); err != nil {
		t.Fatalf("delete calendar: %v", err)
	}

	if _, err := f.attachments.store.Open(created.ID); err != nil {
		t.Fatalf("expected the file to still be on disk right after the calendar's cascade delete: %v", err)
	}

	sweeper := NewAttachmentSweeper(f.attachments.attachments, f.attachments.store)
	if err := sweeper.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if _, err := f.attachments.store.Open(created.ID); err == nil {
		t.Fatal("expected the file to be reclaimed by the sweep after its calendar cascade-deleted")
	}
}

// TestAttachmentSweeper_ReclaimsFilesLeftByAccountDeletion is the
// acceptance criterion "Deleting a User leaves no files behind after a
// sweep" (ADR-0037): deleting the owning User cascades users -> calendars
// -> events -> event_attachments, again with no Go code watching
// file-by-file.
func TestAttachmentSweeper_ReclaimsFilesLeftByAccountDeletion(t *testing.T) {
	f := newAttachmentTestFixture(t, 10)
	ctx := context.Background()

	created, err := f.attachments.Upload(ctx, f.ownerID, f.masterID, "x.txt", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	// The owner's personal Workspace (created by the fixture so Calendar
	// Create can pass its membership guard, #155) has no ON DELETE behavior
	// on workspaces.owner_user_id (ADR-0044: ownership must transfer before
	// a User can be deleted) — clear it directly here since this test
	// exercises the users -> calendars -> events -> event_attachments
	// cascade specifically, not Workspace lifecycle.
	if _, err := f.sqlDB.ExecContext(ctx, "DELETE FROM workspace_members WHERE user_id = ?", f.ownerID); err != nil {
		t.Fatalf("delete owner's workspace membership: %v", err)
	}
	if _, err := f.sqlDB.ExecContext(ctx, "DELETE FROM workspaces WHERE owner_user_id = ?", f.ownerID); err != nil {
		t.Fatalf("delete owner's workspace: %v", err)
	}

	if _, err := f.sqlDB.ExecContext(ctx, "DELETE FROM users WHERE id = ?", f.ownerID); err != nil {
		t.Fatalf("delete owner: %v", err)
	}

	if _, err := f.attachments.store.Open(created.ID); err != nil {
		t.Fatalf("expected the file to still be on disk right after the account's cascade delete: %v", err)
	}

	sweeper := NewAttachmentSweeper(f.attachments.attachments, f.attachments.store)
	if err := sweeper.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if _, err := f.attachments.store.Open(created.ID); err == nil {
		t.Fatal("expected the file to be reclaimed by the sweep after its owning account cascade-deleted")
	}
}
