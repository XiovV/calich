package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

func newTestWorkspaceService(t *testing.T) (*WorkspaceService, *repository.UserRepository) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	return NewWorkspaceService(sqlDB, repository.NewWorkspaceRepository(sqlDB), repository.NewWorkspaceInviteRepository(sqlDB), repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB)), users
}

func TestWorkspaceService_CreateForOwner_AddsOwnerMembership(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()

	user, err := users.Create(ctx, "alice", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	workspace, err := workspaces.CreateForOwner(ctx, user.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if workspace.OwnerUserID != user.ID {
		t.Fatalf("expected owner_user_id %d, got %d", user.ID, workspace.OwnerUserID)
	}
	if workspace.Name != "Alice's Workspace" {
		t.Fatalf("expected name %q, got %q", "Alice's Workspace", workspace.Name)
	}

	list, err := workspaces.ListForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list for user: %v", err)
	}
	if len(list) != 1 || list[0].ID != workspace.ID {
		t.Fatalf("expected the owner to be a member of their own workspace, got %+v", list)
	}
}

func TestWorkspaceService_ListForUser_EmptyWhenNoMembership(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()

	user, err := users.Create(ctx, "alice", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	list, err := workspaces.ListForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list for user: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no workspaces, got %+v", list)
	}
}

// workspaceWithMembers creates a Workspace owned by owner and adds member,
// via the given role, returning both users' ids alongside the workspace id
// — the fixture every Role-authority test below builds on.
func workspaceWithMembers(t *testing.T, workspaces *WorkspaceService, users *repository.UserRepository, memberRole string) (workspaceID, ownerID, memberID int64) {
	t.Helper()
	ctx := context.Background()

	owner, err := users.Create(ctx, "owner", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member, err := users.Create(ctx, "member", "hash", false)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Test Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	if err := workspaces.WithTx(ctx, func(tx *sql.Tx) error {
		return workspaces.AddMemberInTx(ctx, tx, workspace.ID, member.ID, memberRole)
	}); err != nil {
		t.Fatalf("add member: %v", err)
	}

	return workspace.ID, owner.ID, member.ID
}

func TestWorkspaceService_SetMemberRole_OwnerCanGrantAdmin(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()
	workspaceID, ownerID, memberID := workspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	updated, err := workspaces.SetMemberRole(ctx, ownerID, workspaceID, memberID, repository.WorkspaceRoleAdmin)
	if err != nil {
		t.Fatalf("set member role: %v", err)
	}
	if updated.Role != repository.WorkspaceRoleAdmin {
		t.Fatalf("expected role %q, got %q", repository.WorkspaceRoleAdmin, updated.Role)
	}
}

func TestWorkspaceService_SetMemberRole_OwnerCanRevokeAdmin(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()
	workspaceID, ownerID, adminID := workspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleAdmin)

	updated, err := workspaces.SetMemberRole(ctx, ownerID, workspaceID, adminID, repository.WorkspaceRoleMember)
	if err != nil {
		t.Fatalf("set member role: %v", err)
	}
	if updated.Role != repository.WorkspaceRoleMember {
		t.Fatalf("expected role %q, got %q", repository.WorkspaceRoleMember, updated.Role)
	}
}

func TestWorkspaceService_SetMemberRole_AdminCannotGrantAdmin(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()
	workspaceID, _, adminID := workspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleAdmin)

	other, err := users.Create(ctx, "other", "hash", false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}
	if err := workspaces.WithTx(ctx, func(tx *sql.Tx) error {
		return workspaces.AddMemberInTx(ctx, tx, workspaceID, other.ID, repository.WorkspaceRoleMember)
	}); err != nil {
		t.Fatalf("add other member: %v", err)
	}

	_, err = workspaces.SetMemberRole(ctx, adminID, workspaceID, other.ID, repository.WorkspaceRoleAdmin)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound refusing a non-owner actor, got %v", err)
	}
}

func TestWorkspaceService_SetMemberRole_InvalidRoleRejected(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()
	workspaceID, ownerID, memberID := workspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	_, err := workspaces.SetMemberRole(ctx, ownerID, workspaceID, memberID, "owner")
	if !errors.Is(err, ErrInvalidWorkspaceRole) {
		t.Fatalf("expected ErrInvalidWorkspaceRole, got %v", err)
	}
}

func TestWorkspaceService_SetMemberRole_CannotChangeOwnerRole(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()
	workspaceID, ownerID, _ := workspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	_, err := workspaces.SetMemberRole(ctx, ownerID, workspaceID, ownerID, repository.WorkspaceRoleAdmin)
	if !errors.Is(err, ErrCannotChangeOwnerRole) {
		t.Fatalf("expected ErrCannotChangeOwnerRole, got %v", err)
	}
}

func TestWorkspaceService_RemoveMember_AdminCanRemovePlainMember(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()
	workspaceID, _, adminID := workspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleAdmin)

	target, err := users.Create(ctx, "target", "hash", false)
	if err != nil {
		t.Fatalf("create target user: %v", err)
	}
	if err := workspaces.WithTx(ctx, func(tx *sql.Tx) error {
		return workspaces.AddMemberInTx(ctx, tx, workspaceID, target.ID, repository.WorkspaceRoleMember)
	}); err != nil {
		t.Fatalf("add target member: %v", err)
	}

	if err := workspaces.RemoveMember(ctx, adminID, workspaceID, target.ID, nil); err != nil {
		t.Fatalf("remove member: %v", err)
	}

	if _, err := workspaces.workspaces.GetMember(ctx, workspaceID, target.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected member to be removed, got %v", err)
	}
}

func TestWorkspaceService_RemoveMember_AdminRefusedAgainstOwner(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()
	workspaceID, ownerID, adminID := workspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleAdmin)

	err := workspaces.RemoveMember(ctx, adminID, workspaceID, ownerID, nil)
	if !errors.Is(err, ErrCannotRemoveOwner) {
		t.Fatalf("expected ErrCannotRemoveOwner, got %v", err)
	}
}

func TestWorkspaceService_RemoveMember_AdminRefusedAgainstAnotherAdmin(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()
	workspaceID, _, adminID := workspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleAdmin)

	otherAdmin, err := users.Create(ctx, "other-admin", "hash", false)
	if err != nil {
		t.Fatalf("create other admin: %v", err)
	}
	if err := workspaces.WithTx(ctx, func(tx *sql.Tx) error {
		return workspaces.AddMemberInTx(ctx, tx, workspaceID, otherAdmin.ID, repository.WorkspaceRoleAdmin)
	}); err != nil {
		t.Fatalf("add other admin: %v", err)
	}

	err = workspaces.RemoveMember(ctx, adminID, workspaceID, otherAdmin.ID, nil)
	if !errors.Is(err, ErrAdminCannotRemoveAdmin) {
		t.Fatalf("expected ErrAdminCannotRemoveAdmin, got %v", err)
	}
}

func TestWorkspaceService_RemoveMember_OwnerCanRemoveAdmin(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()
	workspaceID, ownerID, adminID := workspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleAdmin)

	if err := workspaces.RemoveMember(ctx, ownerID, workspaceID, adminID, nil); err != nil {
		t.Fatalf("expected owner to remove an admin, got %v", err)
	}
}

func TestWorkspaceService_RemoveMember_OwnerCannotBeRemoved(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()
	workspaceID, ownerID, memberID := workspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	err := workspaces.RemoveMember(ctx, memberID, workspaceID, ownerID, nil)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound refusing a plain-member actor, got %v", err)
	}
}

func TestWorkspaceService_UpdateSettings_OwnerCanRenameAndSetSharePrivacy(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()
	workspaceID, ownerID, _ := workspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	updated, err := workspaces.UpdateSettings(ctx, ownerID, workspaceID, "Renamed", repository.DefaultSharePrivacyWorkspace)
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if updated.Name != "Renamed" {
		t.Fatalf("expected name %q, got %q", "Renamed", updated.Name)
	}
	if updated.DefaultSharePrivacy != repository.DefaultSharePrivacyWorkspace {
		t.Fatalf("expected default_share_privacy %q, got %q", repository.DefaultSharePrivacyWorkspace, updated.DefaultSharePrivacy)
	}
}

func TestWorkspaceService_UpdateSettings_AdminCanRename(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()
	workspaceID, _, adminID := workspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleAdmin)

	updated, err := workspaces.UpdateSettings(ctx, adminID, workspaceID, "Renamed by Admin", repository.DefaultSharePrivacyPrivate)
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if updated.Name != "Renamed by Admin" {
		t.Fatalf("expected name %q, got %q", "Renamed by Admin", updated.Name)
	}
}

func TestWorkspaceService_UpdateSettings_MemberRefused(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()
	workspaceID, _, memberID := workspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	_, err := workspaces.UpdateSettings(ctx, memberID, workspaceID, "Renamed", repository.DefaultSharePrivacyPrivate)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound refusing a plain-member actor, got %v", err)
	}
}

// TestWorkspaceService_RemoveMemberImpact_ReportsOwnedCalendarsAndTransferCandidates
// covers #160's preview: every Calendar the target owns within the
// Workspace, its Share count, and who else in that Workspace could receive
// it — a Calendar the target owns in a different Workspace never appears.
func TestWorkspaceService_RemoveMemberImpact_ReportsOwnedCalendarsAndTransferCandidates(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, bobWorkspace := h.register(t, "bob")
	carol, _ := h.register(t, "carol")
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)
	h.addMember(t, aliceWorkspace.ID, carol.ID, repository.WorkspaceRoleMember)

	bobsCalendar := h.createCalendar(t, bob.ID, aliceWorkspace.ID, "cal-bob-alice-ws", "Bob's shared-workspace calendar")
	if _, err := h.calendars.Share(ctx, bob.ID, bobsCalendar.ID, "carol", repository.RoleViewer); err != nil {
		t.Fatalf("share with carol: %v", err)
	}
	// Bob also owns a calendar in his own workspace — untouched by removal
	// from alice's workspace, so it must never appear in this impact.
	h.createCalendar(t, bob.ID, bobWorkspace.ID, "cal-bob-own-ws", "Bob's own workspace calendar")

	impact, err := h.workspaces.RemoveMemberImpact(ctx, alice.ID, aliceWorkspace.ID, bob.ID)
	if err != nil {
		t.Fatalf("remove member impact: %v", err)
	}

	if len(impact.Calendars) != 1 {
		t.Fatalf("expected exactly one calendar (scoped to alice's workspace), got %+v", impact.Calendars)
	}
	got := impact.Calendars[0]
	if got.ID != bobsCalendar.ID {
		t.Fatalf("expected bob's alice-workspace calendar, got %+v", got)
	}
	if got.ShareCount != 1 {
		t.Fatalf("expected 1 share, got %d", got.ShareCount)
	}
	if got.WorkspaceID != aliceWorkspace.ID || got.WorkspaceName != aliceWorkspace.Name {
		t.Fatalf("expected the calendar's own workspace, got %+v", got)
	}

	candidateUsernames := map[string]bool{}
	for _, c := range got.TransferCandidates {
		candidateUsernames[c.Username] = true
	}
	if !candidateUsernames["alice"] || !candidateUsernames["carol"] {
		t.Fatalf("expected alice and carol as transfer candidates, got %+v", got.TransferCandidates)
	}
	if candidateUsernames["bob"] {
		t.Fatalf("expected bob not to be his own transfer candidate")
	}
}

// TestWorkspaceService_RemoveMember_BlockedWithoutADispositionForAnOwnedCalendar
// covers #160's core guard: removal is refused while a Calendar the target
// owns in that Workspace has no disposition.
func TestWorkspaceService_RemoveMember_BlockedWithoutADispositionForAnOwnedCalendar(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, _ := h.register(t, "bob")
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)
	h.createCalendar(t, bob.ID, aliceWorkspace.ID, "cal-1", "Bob's")

	if err := h.workspaces.RemoveMember(ctx, alice.ID, aliceWorkspace.ID, bob.ID, nil); !errors.Is(err, ErrMissingCalendarDisposition) {
		t.Fatalf("expected ErrMissingCalendarDisposition, got %v", err)
	}

	if _, err := h.workspaces.workspaces.GetMember(ctx, aliceWorkspace.ID, bob.ID); err != nil {
		t.Fatalf("expected bob to remain a member when removal is blocked: %v", err)
	}
}

// TestWorkspaceService_RemoveMember_SucceedsAfterTransfer covers removal
// once the target's owned Calendar is transferred to another Member of the
// same Workspace: the Calendar survives under its new owner, and the
// Membership is gone.
func TestWorkspaceService_RemoveMember_SucceedsAfterTransfer(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, _ := h.register(t, "bob")
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)
	calendar := h.createCalendar(t, bob.ID, aliceWorkspace.ID, "cal-1", "Bob's")

	if err := h.workspaces.RemoveMember(ctx, alice.ID, aliceWorkspace.ID, bob.ID, []CalendarDisposition{
		{CalendarID: calendar.ID, Disposition: DispositionTransfer, TransferTo: &alice.ID},
	}); err != nil {
		t.Fatalf("remove bob with transfer: %v", err)
	}

	transferred, err := repository.NewCalendarRepository(h.db).GetByIDAny(ctx, calendar.ID)
	if err != nil {
		t.Fatalf("expected the calendar to survive the transfer: %v", err)
	}
	if transferred.UserID != alice.ID {
		t.Fatalf("expected the calendar to be owned by alice, got owner %d", transferred.UserID)
	}

	if _, err := h.workspaces.workspaces.GetMember(ctx, aliceWorkspace.ID, bob.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected bob to no longer be a member, got %v", err)
	}
}

// TestWorkspaceService_RemoveMember_SucceedsAfterDelete covers removal once
// the target's owned Calendar is deleted: the Calendar (and its Events, via
// the existing cascade) is gone, and the Membership is gone.
func TestWorkspaceService_RemoveMember_SucceedsAfterDelete(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, _ := h.register(t, "bob")
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)
	calendar := h.createCalendar(t, bob.ID, aliceWorkspace.ID, "cal-1", "Bob's")

	if err := h.workspaces.RemoveMember(ctx, alice.ID, aliceWorkspace.ID, bob.ID, []CalendarDisposition{
		{CalendarID: calendar.ID, Disposition: DispositionDelete},
	}); err != nil {
		t.Fatalf("remove bob with delete: %v", err)
	}

	if _, err := repository.NewCalendarRepository(h.db).GetByIDAny(ctx, calendar.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the calendar to be deleted, got %v", err)
	}

	if _, err := h.workspaces.workspaces.GetMember(ctx, aliceWorkspace.ID, bob.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected bob to no longer be a member, got %v", err)
	}
}

// TestWorkspaceService_RemoveMember_LeavesCalendarsInOtherWorkspacesUntouched
// covers #160's scoping requirement: a Calendar the target owns in a
// Workspace other than the one they're being removed from is never affected
// or required to carry a disposition.
func TestWorkspaceService_RemoveMember_LeavesCalendarsInOtherWorkspacesUntouched(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, bobWorkspace := h.register(t, "bob")
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)
	bobsOwnCalendar := h.createCalendar(t, bob.ID, bobWorkspace.ID, "cal-bob-own-ws", "Bob's own")

	// Bob owns nothing in alice's workspace, so removal needs no disposition
	// at all.
	if err := h.workspaces.RemoveMember(ctx, alice.ID, aliceWorkspace.ID, bob.ID, nil); err != nil {
		t.Fatalf("remove bob: %v", err)
	}

	stillOwned, err := repository.NewCalendarRepository(h.db).GetByIDAny(ctx, bobsOwnCalendar.ID)
	if err != nil {
		t.Fatalf("expected bob's own-workspace calendar to survive: %v", err)
	}
	if stillOwned.UserID != bob.ID {
		t.Fatalf("expected bob to remain the owner, got %d", stillOwned.UserID)
	}
}

func TestWorkspaceService_RemoveMember_RejectsDuplicateCalendarInDispositions(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, _ := h.register(t, "bob")
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)
	calendar := h.createCalendar(t, bob.ID, aliceWorkspace.ID, "cal-1", "Bob's")

	dispositions := []CalendarDisposition{
		{CalendarID: calendar.ID, Disposition: DispositionDelete},
		{CalendarID: calendar.ID, Disposition: DispositionDelete},
	}
	if err := h.workspaces.RemoveMember(ctx, alice.ID, aliceWorkspace.ID, bob.ID, dispositions); !errors.Is(err, ErrDuplicateDisposition) {
		t.Fatalf("expected ErrDuplicateDisposition, got %v", err)
	}
}

func TestWorkspaceService_RemoveMember_RejectsCalendarNotOwnedByTarget(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, _ := h.register(t, "bob")
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)
	aliceCalendar := h.createCalendar(t, alice.ID, aliceWorkspace.ID, "cal-alice", "Alice's")

	dispositions := []CalendarDisposition{{CalendarID: aliceCalendar.ID, Disposition: DispositionDelete}}
	if err := h.workspaces.RemoveMember(ctx, alice.ID, aliceWorkspace.ID, bob.ID, dispositions); !errors.Is(err, ErrCalendarNotOwnedByRemovedMember) {
		t.Fatalf("expected ErrCalendarNotOwnedByRemovedMember, got %v", err)
	}
}

func TestWorkspaceService_RemoveMember_InvalidDisposition(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, _ := h.register(t, "bob")
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)
	calendar := h.createCalendar(t, bob.ID, aliceWorkspace.ID, "cal-1", "Bob's")

	dispositions := []CalendarDisposition{{CalendarID: calendar.ID, Disposition: "cascade"}}
	if err := h.workspaces.RemoveMember(ctx, alice.ID, aliceWorkspace.ID, bob.ID, dispositions); !errors.Is(err, ErrInvalidDisposition) {
		t.Fatalf("expected ErrInvalidDisposition, got %v", err)
	}
}

func TestWorkspaceService_RemoveMember_TransferRequiresTransferTo(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, _ := h.register(t, "bob")
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)
	calendar := h.createCalendar(t, bob.ID, aliceWorkspace.ID, "cal-1", "Bob's")

	dispositions := []CalendarDisposition{{CalendarID: calendar.ID, Disposition: DispositionTransfer}}
	if err := h.workspaces.RemoveMember(ctx, alice.ID, aliceWorkspace.ID, bob.ID, dispositions); !errors.Is(err, ErrTransferTargetRequired) {
		t.Fatalf("expected ErrTransferTargetRequired, got %v", err)
	}
}

func TestWorkspaceService_RemoveMember_RejectsTransferToTheRemovedMember(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, _ := h.register(t, "bob")
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)
	calendar := h.createCalendar(t, bob.ID, aliceWorkspace.ID, "cal-1", "Bob's")

	dispositions := []CalendarDisposition{{CalendarID: calendar.ID, Disposition: DispositionTransfer, TransferTo: &bob.ID}}
	if err := h.workspaces.RemoveMember(ctx, alice.ID, aliceWorkspace.ID, bob.ID, dispositions); !errors.Is(err, ErrCannotTransferToRemovedMember) {
		t.Fatalf("expected ErrCannotTransferToRemovedMember, got %v", err)
	}
}

func TestWorkspaceService_RemoveMember_RejectsTransferTargetOutsideTheWorkspace(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, _ := h.register(t, "bob")
	carol, _ := h.register(t, "carol") // not a member of alice's workspace
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)
	calendar := h.createCalendar(t, bob.ID, aliceWorkspace.ID, "cal-1", "Bob's")

	dispositions := []CalendarDisposition{{CalendarID: calendar.ID, Disposition: DispositionTransfer, TransferTo: &carol.ID}}
	if err := h.workspaces.RemoveMember(ctx, alice.ID, aliceWorkspace.ID, bob.ID, dispositions); !errors.Is(err, ErrTransferTargetNotWorkspaceMember) {
		t.Fatalf("expected ErrTransferTargetNotWorkspaceMember, got %v", err)
	}
}
