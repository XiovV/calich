package service

import (
	"context"
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
	return NewWorkspaceService(sqlDB, repository.NewWorkspaceRepository(sqlDB)), users
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
