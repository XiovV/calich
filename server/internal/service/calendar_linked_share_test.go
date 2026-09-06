// calendar_linked_share_test.go covers #294's sharing rules for a Linked
// Calendar: it is Shareable to a Workspace Member like any other Calendar,
// but a Share on it can only ever be created with the Viewer Role — an
// Editor's write would run as the connecting User's own Provider identity
// (ADR-0075's consequences), so the Editor Role is refused outright rather
// than silently downgraded the way ResolveAccess clamps a Subscribed
// Calendar's.
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calich/server/internal/repository"
)

// linkedShareFixture is #294's fixture: an Owner's Linked Calendar (a
// Calendar carrying a Connection-kind Source in the given Mode), a Workspace
// with a Member and a Group, and nothing shared yet.
type linkedShareFixture struct {
	calendars   *CalendarService
	shares      *repository.CalendarShareRepository
	ownerID     int64
	memberID    int64
	memberEmail string
	calendarID  string
	groupID     int64
}

func newLinkedShareFixture(t *testing.T, mode repository.SourceMode) linkedShareFixture {
	t.Helper()
	ctx := context.Background()
	g := newTestGraph(t)

	owner, err := g.UserRepo.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member, err := g.UserRepo.Create(ctx, "member", "member@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	workspace, err := g.WorkspaceRepo.Create(ctx, "Test Workspace", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := g.WorkspaceRepo.AddMember(ctx, workspace.ID, owner.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add owner as workspace member: %v", err)
	}
	if err := g.WorkspaceRepo.AddMember(ctx, workspace.ID, member.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add member as workspace member: %v", err)
	}

	group, err := g.GroupRepo.Create(ctx, workspace.ID, "Team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	cal, err := g.CalendarRepo.Create(ctx, owner.ID, workspace.ID, "cal-linked", repository.CalendarFields{Name: "Work", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	conn, err := g.ConnectionRepo.Upsert(ctx, owner.ID, repository.ProviderGoogle, "someone@gmail.com", repository.ConnectionFields{
		RefreshToken: "encrypted-refresh", Status: repository.ConnectionStatusLive,
	})
	if err != nil {
		t.Fatalf("upsert connection: %v", err)
	}
	externalCalendarID := "primary"
	if _, err := g.SourceRepo.Create(ctx, cal.ID, repository.SourceFields{
		Kind:               repository.SourceKindConnection,
		Mode:               mode,
		ConnectionID:       &conn.ID,
		ExternalCalendarID: &externalCalendarID,
	}); err != nil {
		t.Fatalf("create source: %v", err)
	}

	return linkedShareFixture{
		calendars:   g.Calendars,
		shares:      g.ShareRepo,
		ownerID:     owner.ID,
		memberID:    member.ID,
		memberEmail: "member@example.com",
		calendarID:  cal.ID,
		groupID:     group.ID,
	}
}

// TestCalendarService_Share_LinkedCalendar_ViewerSucceeds covers #294's "a
// Linked Calendar can be Shared to a Workspace Member": the Viewer Role is
// accepted and resolves to Viewer Access for the Member.
func TestCalendarService_Share_LinkedCalendar_ViewerSucceeds(t *testing.T) {
	f := newLinkedShareFixture(t, repository.SourceModeWritable)
	ctx := context.Background()

	if _, _, err := f.calendars.Share(ctx, f.ownerID, f.calendarID, f.memberEmail, repository.RoleViewer); err != nil {
		t.Fatalf("share linked calendar as viewer: %v", err)
	}

	access, _, err := f.calendars.Access(ctx, f.memberID, f.calendarID)
	if err != nil {
		t.Fatalf("resolve member access: %v", err)
	}
	if access != AccessViewer {
		t.Fatalf("member Access = %v, want AccessViewer", access)
	}
}

// TestCalendarService_Share_LinkedCalendar_EditorRejected covers #294's "a
// Share on a Linked Calendar cannot be created with the Editor Role": the
// call is refused outright, not silently downgraded, and no Share row is
// written.
func TestCalendarService_Share_LinkedCalendar_EditorRejected(t *testing.T) {
	f := newLinkedShareFixture(t, repository.SourceModeWritable)
	ctx := context.Background()

	_, _, err := f.calendars.Share(ctx, f.ownerID, f.calendarID, f.memberEmail, repository.RoleEditor)
	if !errors.Is(err, ErrLinkedCalendarEditorShare) {
		t.Fatalf("share linked calendar as editor: got %v, want ErrLinkedCalendarEditorShare", err)
	}

	shares, err := f.shares.ListByCalendarWithUser(ctx, f.calendarID)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	if len(shares) != 0 {
		t.Fatalf("expected no Share row after a rejected Editor share, got %d", len(shares))
	}
}

// TestCalendarService_Share_LinkedCalendar_EditorRejectedWhenReadOnly proves
// the clamp keys on the Source's Kind, not its Mode: even a read-only Linked
// Calendar — where ResolveAccess would clamp an Editor to Viewer anyway —
// still refuses the Editor Role at creation, so the rule is one sentence
// regardless of a Calendar's current writability.
func TestCalendarService_Share_LinkedCalendar_EditorRejectedWhenReadOnly(t *testing.T) {
	f := newLinkedShareFixture(t, repository.SourceModeReadOnly)
	ctx := context.Background()

	_, _, err := f.calendars.Share(ctx, f.ownerID, f.calendarID, f.memberEmail, repository.RoleEditor)
	if !errors.Is(err, ErrLinkedCalendarEditorShare) {
		t.Fatalf("share read-only linked calendar as editor: got %v, want ErrLinkedCalendarEditorShare", err)
	}
}

// TestCalendarService_Share_LinkedCalendar_CannotUpgradeViewerToEditor
// covers the same rule on the change-an-existing-Share path (Share upserts):
// a Member already holding a Viewer Share cannot be moved to Editor, and the
// Viewer Share they had is left untouched.
func TestCalendarService_Share_LinkedCalendar_CannotUpgradeViewerToEditor(t *testing.T) {
	f := newLinkedShareFixture(t, repository.SourceModeWritable)
	ctx := context.Background()

	if _, _, err := f.calendars.Share(ctx, f.ownerID, f.calendarID, f.memberEmail, repository.RoleViewer); err != nil {
		t.Fatalf("seed viewer share: %v", err)
	}

	_, _, err := f.calendars.Share(ctx, f.ownerID, f.calendarID, f.memberEmail, repository.RoleEditor)
	if !errors.Is(err, ErrLinkedCalendarEditorShare) {
		t.Fatalf("upgrade viewer share to editor: got %v, want ErrLinkedCalendarEditorShare", err)
	}

	shares, err := f.shares.ListByCalendarWithUser(ctx, f.calendarID)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	if len(shares) != 1 || shares[0].Role != repository.RoleViewer {
		t.Fatalf("expected the existing Viewer Share to be untouched, got %+v", shares)
	}
}

// TestCalendarService_ShareWithGroup_LinkedCalendar_ViewerSucceeds mirrors
// the direct-Share success case on the Group-Share path.
func TestCalendarService_ShareWithGroup_LinkedCalendar_ViewerSucceeds(t *testing.T) {
	f := newLinkedShareFixture(t, repository.SourceModeWritable)
	ctx := context.Background()

	if _, err := f.calendars.ShareWithGroup(ctx, f.ownerID, f.calendarID, f.groupID, repository.RoleViewer); err != nil {
		t.Fatalf("group-share linked calendar as viewer: %v", err)
	}
}

// TestCalendarService_ShareWithGroup_LinkedCalendar_EditorRejected mirrors
// the Editor rejection on the Group-Share path — the same rule, since a
// Group Share resolves to the same per-User Access a direct one would.
func TestCalendarService_ShareWithGroup_LinkedCalendar_EditorRejected(t *testing.T) {
	f := newLinkedShareFixture(t, repository.SourceModeWritable)
	ctx := context.Background()

	_, err := f.calendars.ShareWithGroup(ctx, f.ownerID, f.calendarID, f.groupID, repository.RoleEditor)
	if !errors.Is(err, ErrLinkedCalendarEditorShare) {
		t.Fatalf("group-share linked calendar as editor: got %v, want ErrLinkedCalendarEditorShare", err)
	}
}
