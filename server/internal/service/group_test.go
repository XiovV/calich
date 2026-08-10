package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// newTestGroupService returns a GroupService plus a WorkspaceService sharing
// the same in-memory database, and a UserRepository — every fixture below
// needs Users and Workspaces to satisfy groups' and group_members' foreign
// keys.
func newTestGroupService(t *testing.T) (*GroupService, *WorkspaceService, *repository.UserRepository) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspaceService := NewWorkspaceService(sqlDB, workspaceRepo, repository.NewWorkspaceInviteRepository(sqlDB), repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB))
	groupService := NewGroupService(repository.NewGroupRepository(sqlDB), workspaceRepo)

	return groupService, workspaceService, users
}

// groupWorkspaceWithMembers creates a Workspace owned by owner, adds member
// with memberRole, and returns every id the Group tests need.
func groupWorkspaceWithMembers(t *testing.T, workspaces *WorkspaceService, users *repository.UserRepository, memberRole string) (workspaceID, ownerID, memberID int64) {
	t.Helper()
	ctx := context.Background()

	owner, err := users.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member, err := users.Create(ctx, "member", "member@example.com", "hash", false)
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

func TestGroupService_Create_OwnerCanCreate(t *testing.T) {
	groups, workspaces, users := newTestGroupService(t)
	ctx := context.Background()
	workspaceID, ownerID, _ := groupWorkspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	group, err := groups.Create(ctx, ownerID, workspaceID, "Tech team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if group.Name != "Tech team" {
		t.Fatalf("expected name %q, got %q", "Tech team", group.Name)
	}
	if group.WorkspaceID != workspaceID {
		t.Fatalf("expected workspace id %d, got %d", workspaceID, group.WorkspaceID)
	}
}

func TestGroupService_Create_AdminCanCreate(t *testing.T) {
	groups, workspaces, users := newTestGroupService(t)
	ctx := context.Background()
	workspaceID, _, adminID := groupWorkspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleAdmin)

	if _, err := groups.Create(ctx, adminID, workspaceID, "Tech team"); err != nil {
		t.Fatalf("create group: %v", err)
	}
}

func TestGroupService_Create_PlainMemberRefused(t *testing.T) {
	groups, workspaces, users := newTestGroupService(t)
	ctx := context.Background()
	workspaceID, _, memberID := groupWorkspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	_, err := groups.Create(ctx, memberID, workspaceID, "Tech team")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGroupService_Create_EmptyNameRefused(t *testing.T) {
	groups, workspaces, users := newTestGroupService(t)
	ctx := context.Background()
	workspaceID, ownerID, _ := groupWorkspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	_, err := groups.Create(ctx, ownerID, workspaceID, "   ")
	if !errors.Is(err, ErrInvalidGroupName) {
		t.Fatalf("expected ErrInvalidGroupName, got %v", err)
	}
}

func TestGroupService_Rename_OwnerCanRename(t *testing.T) {
	groups, workspaces, users := newTestGroupService(t)
	ctx := context.Background()
	workspaceID, ownerID, _ := groupWorkspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	group, err := groups.Create(ctx, ownerID, workspaceID, "Tech team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	renamed, err := groups.Rename(ctx, ownerID, group.ID, "Engineering")
	if err != nil {
		t.Fatalf("rename group: %v", err)
	}
	if renamed.Name != "Engineering" {
		t.Fatalf("expected name %q, got %q", "Engineering", renamed.Name)
	}
}

func TestGroupService_Rename_PlainMemberRefused(t *testing.T) {
	groups, workspaces, users := newTestGroupService(t)
	ctx := context.Background()
	workspaceID, ownerID, memberID := groupWorkspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	group, err := groups.Create(ctx, ownerID, workspaceID, "Tech team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	_, err = groups.Rename(ctx, memberID, group.ID, "Engineering")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGroupService_Delete_OwnerCanDelete(t *testing.T) {
	groups, workspaces, users := newTestGroupService(t)
	ctx := context.Background()
	workspaceID, ownerID, _ := groupWorkspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	group, err := groups.Create(ctx, ownerID, workspaceID, "Tech team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	if err := groups.Delete(ctx, ownerID, group.ID); err != nil {
		t.Fatalf("delete group: %v", err)
	}

	if _, err := groups.groups.GetByID(ctx, group.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected group to be gone, got %v", err)
	}
}

func TestGroupService_Delete_PlainMemberRefused(t *testing.T) {
	groups, workspaces, users := newTestGroupService(t)
	ctx := context.Background()
	workspaceID, ownerID, memberID := groupWorkspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	group, err := groups.Create(ctx, ownerID, workspaceID, "Tech team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	if err := groups.Delete(ctx, memberID, group.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGroupService_AddMember_OwnerCanAddWorkspaceMember(t *testing.T) {
	groups, workspaces, users := newTestGroupService(t)
	ctx := context.Background()
	workspaceID, ownerID, memberID := groupWorkspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	group, err := groups.Create(ctx, ownerID, workspaceID, "Tech team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	if err := groups.AddMember(ctx, ownerID, group.ID, memberID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	members, err := groups.ListMembers(ctx, ownerID, group.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 1 || members[0].UserID != memberID {
		t.Fatalf("expected [%d], got %v", memberID, members)
	}
}

func TestGroupService_AddMember_PlainMemberRefused(t *testing.T) {
	groups, workspaces, users := newTestGroupService(t)
	ctx := context.Background()
	workspaceID, ownerID, memberID := groupWorkspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	group, err := groups.Create(ctx, ownerID, workspaceID, "Tech team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	if err := groups.AddMember(ctx, memberID, group.ID, memberID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGroupService_AddMember_RefusesUserOutsideWorkspace(t *testing.T) {
	groups, workspaces, users := newTestGroupService(t)
	ctx := context.Background()
	workspaceID, ownerID, _ := groupWorkspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	outsider, err := users.Create(ctx, "outsider", "outsider@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create outsider: %v", err)
	}

	group, err := groups.Create(ctx, ownerID, workspaceID, "Tech team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	err = groups.AddMember(ctx, ownerID, group.ID, outsider.ID)
	if !errors.Is(err, ErrGroupMemberNotInWorkspace) {
		t.Fatalf("expected ErrGroupMemberNotInWorkspace, got %v", err)
	}
}

func TestGroupService_AddMember_RefusesUserFromAnotherWorkspace(t *testing.T) {
	groups, workspaces, users := newTestGroupService(t)
	ctx := context.Background()
	workspaceID, ownerID, _ := groupWorkspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	otherWorkspaceOwner, err := users.Create(ctx, "other-owner", "other-owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other workspace owner: %v", err)
	}
	if _, err := workspaces.CreateForOwner(ctx, otherWorkspaceOwner.ID, "Other Workspace"); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}

	group, err := groups.Create(ctx, ownerID, workspaceID, "Tech team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	err = groups.AddMember(ctx, ownerID, group.ID, otherWorkspaceOwner.ID)
	if !errors.Is(err, ErrGroupMemberNotInWorkspace) {
		t.Fatalf("expected ErrGroupMemberNotInWorkspace, got %v", err)
	}
}

func TestGroupService_RemoveMember_OwnerCanRemove(t *testing.T) {
	groups, workspaces, users := newTestGroupService(t)
	ctx := context.Background()
	workspaceID, ownerID, memberID := groupWorkspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	group, err := groups.Create(ctx, ownerID, workspaceID, "Tech team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := groups.AddMember(ctx, ownerID, group.ID, memberID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	if err := groups.RemoveMember(ctx, ownerID, group.ID, memberID); err != nil {
		t.Fatalf("remove member: %v", err)
	}

	members, err := groups.ListMembers(ctx, ownerID, group.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("expected no members, got %v", members)
	}
}

func TestGroupService_RemoveMember_PlainMemberRefused(t *testing.T) {
	groups, workspaces, users := newTestGroupService(t)
	ctx := context.Background()
	workspaceID, ownerID, memberID := groupWorkspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	group, err := groups.Create(ctx, ownerID, workspaceID, "Tech team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := groups.AddMember(ctx, ownerID, group.ID, memberID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	if err := groups.RemoveMember(ctx, memberID, group.ID, memberID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGroupService_UserCanBelongToMultipleGroups(t *testing.T) {
	groups, workspaces, users := newTestGroupService(t)
	ctx := context.Background()
	workspaceID, ownerID, memberID := groupWorkspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	techTeam, err := groups.Create(ctx, ownerID, workspaceID, "Tech team")
	if err != nil {
		t.Fatalf("create tech team: %v", err)
	}
	leads, err := groups.Create(ctx, ownerID, workspaceID, "Leads")
	if err != nil {
		t.Fatalf("create leads: %v", err)
	}

	if err := groups.AddMember(ctx, ownerID, techTeam.ID, memberID); err != nil {
		t.Fatalf("add to tech team: %v", err)
	}
	if err := groups.AddMember(ctx, ownerID, leads.ID, memberID); err != nil {
		t.Fatalf("add to leads: %v", err)
	}

	techMembers, err := groups.ListMembers(ctx, ownerID, techTeam.ID)
	if err != nil {
		t.Fatalf("list tech team members: %v", err)
	}
	leadMembers, err := groups.ListMembers(ctx, ownerID, leads.ID)
	if err != nil {
		t.Fatalf("list leads members: %v", err)
	}

	if len(techMembers) != 1 || techMembers[0].UserID != memberID {
		t.Fatalf("expected tech team to contain %d, got %v", memberID, techMembers)
	}
	if len(leadMembers) != 1 || leadMembers[0].UserID != memberID {
		t.Fatalf("expected leads to contain %d, got %v", memberID, leadMembers)
	}
}

func TestGroupService_ListByWorkspace_ReturnsWorkspaceGroups(t *testing.T) {
	groups, workspaces, users := newTestGroupService(t)
	ctx := context.Background()
	workspaceID, ownerID, memberID := groupWorkspaceWithMembers(t, workspaces, users, repository.WorkspaceRoleMember)

	if _, err := groups.Create(ctx, ownerID, workspaceID, "Tech team"); err != nil {
		t.Fatalf("create group: %v", err)
	}

	list, err := groups.ListByWorkspace(ctx, memberID, workspaceID)
	if err != nil {
		t.Fatalf("list by workspace: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Tech team" {
		t.Fatalf("expected [Tech team], got %+v", list)
	}
}
