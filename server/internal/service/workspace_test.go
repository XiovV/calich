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
	return NewWorkspaceService(sqlDB, repository.NewWorkspaceRepository(sqlDB), repository.NewWorkspaceInviteRepository(sqlDB)), users
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

	if err := workspaces.RemoveMember(ctx, adminID, workspaceID, target.ID); err != nil {
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

	err := workspaces.RemoveMember(ctx, adminID, workspaceID, ownerID)
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

	err = workspaces.RemoveMember(ctx, adminID, workspaceID, otherAdmin.ID)
	if !errors.Is(err, ErrAdminCannotRemoveAdmin) {
		t.Fatalf("expected ErrAdminCannotRemoveAdmin, got %v", err)
	}
}

func TestWorkspaceService_RemoveMember_OwnerCanRemoveAdmin(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()
	workspaceID, ownerID, adminID := workspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleAdmin)

	if err := workspaces.RemoveMember(ctx, ownerID, workspaceID, adminID); err != nil {
		t.Fatalf("expected owner to remove an admin, got %v", err)
	}
}

func TestWorkspaceService_RemoveMember_OwnerCannotBeRemoved(t *testing.T) {
	workspaces, users := newTestWorkspaceService(t)
	ctx := context.Background()
	workspaceID, ownerID, memberID := workspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	err := workspaces.RemoveMember(ctx, memberID, workspaceID, ownerID)
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
