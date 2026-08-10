package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// newTestWorkspaceInviteHarness wires a WorkspaceService and an AuthService
// against the same in-memory database — #154's issue and accept paths sit on
// different services (mirroring AccountService/AuthService's split for the
// account-level Invite this replaces), but exercise the same rows.
func newTestWorkspaceInviteHarness(t *testing.T) (*WorkspaceService, *AuthService, *repository.UserRepository) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	inviteRepo := repository.NewWorkspaceInviteRepository(sqlDB)
	workspaces := NewWorkspaceService(sqlDB, repository.NewWorkspaceRepository(sqlDB), inviteRepo, repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB))
	auth := NewAuthService(users, sessions, workspaces, inviteRepo, []byte("test-secret"), "", "", false)

	return workspaces, auth, users
}

func TestWorkspaceService_CreateInvite_RequiresOwnerOrAdmin(t *testing.T) {
	workspaces, _, users := newTestWorkspaceInviteHarness(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	plainMember, err := users.Create(ctx, "bob", "hash", false)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := workspaces.WithTx(ctx, func(tx *sql.Tx) error {
		return workspaces.AddMemberInTx(ctx, tx, workspace.ID, plainMember.ID, repository.WorkspaceRoleMember)
	}); err != nil {
		t.Fatalf("add plain member: %v", err)
	}

	if _, err := workspaces.CreateInvite(ctx, plainMember.ID, workspace.ID, "invitee@example.com"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a plain Member issuing an invite, got %v", err)
	}

	outsider, err := users.Create(ctx, "carol", "hash", false)
	if err != nil {
		t.Fatalf("create outsider: %v", err)
	}
	if _, err := workspaces.CreateInvite(ctx, outsider.ID, workspace.ID, "invitee@example.com"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a non-member issuing an invite, got %v", err)
	}

	if _, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "invitee@example.com"); err != nil {
		t.Fatalf("expected the Owner to be able to issue an invite, got %v", err)
	}
}

func TestWorkspaceService_CreateInvite_ConflictsWithOutstandingInvite(t *testing.T) {
	workspaces, _, users := newTestWorkspaceInviteHarness(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	if _, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "invitee@example.com"); err != nil {
		t.Fatalf("create invite: %v", err)
	}

	if _, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "invitee@example.com"); !errors.Is(err, repository.ErrWorkspaceInviteExists) {
		t.Fatalf("expected ErrWorkspaceInviteExists, got %v", err)
	}
}

// TestWorkspaceService_ReissueInvite_InvalidatesPriorToken covers #154's
// acceptance criterion directly: reissuing overwrites the outstanding
// invite's token, and the token it replaces can no longer be accepted.
func TestWorkspaceService_ReissueInvite_InvalidatesPriorToken(t *testing.T) {
	workspaces, auth, users := newTestWorkspaceInviteHarness(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	first, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "bob@example.com")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	second, err := workspaces.ReissueInvite(ctx, owner.ID, first.Invite.ID)
	if err != nil {
		t.Fatalf("reissue invite: %v", err)
	}
	if second.Token == first.Token {
		t.Fatalf("expected reissue to mint a different token")
	}

	if _, err := auth.AcceptWorkspaceInviteNewAccount(ctx, first.Token, "bob", "hunter2"); !errors.Is(err, ErrWorkspaceInviteInvalid) {
		t.Fatalf("expected the prior token to be invalid, got %v", err)
	}

	if _, err := auth.AcceptWorkspaceInviteNewAccount(ctx, second.Token, "bob", "hunter2"); err != nil {
		t.Fatalf("expected the reissued token to work, got %v", err)
	}
}

func TestWorkspaceService_ReissueInvite_RequiresOwnerOrAdmin(t *testing.T) {
	workspaces, _, users := newTestWorkspaceInviteHarness(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	invite, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "bob@example.com")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	outsider, err := users.Create(ctx, "carol", "hash", false)
	if err != nil {
		t.Fatalf("create outsider: %v", err)
	}

	if _, err := workspaces.ReissueInvite(ctx, outsider.ID, invite.Invite.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a non-member reissuing an invite, got %v", err)
	}
}

func TestWorkspaceService_ListInvites_ReturnsOutstandingInvitesForOwnerOrAdmin(t *testing.T) {
	workspaces, _, users := newTestWorkspaceInviteHarness(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	if _, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "bob@example.com"); err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if _, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "carol@example.com"); err != nil {
		t.Fatalf("create invite: %v", err)
	}

	invites, err := workspaces.ListInvites(ctx, owner.ID, workspace.ID)
	if err != nil {
		t.Fatalf("list invites: %v", err)
	}
	if len(invites) != 2 {
		t.Fatalf("expected 2 outstanding invites, got %d", len(invites))
	}

	plainMember, err := users.Create(ctx, "dave", "hash", false)
	if err != nil {
		t.Fatalf("create plain member: %v", err)
	}
	if err := workspaces.WithTx(ctx, func(tx *sql.Tx) error {
		return workspaces.AddMemberInTx(ctx, tx, workspace.ID, plainMember.ID, repository.WorkspaceRoleMember)
	}); err != nil {
		t.Fatalf("add plain member: %v", err)
	}
	if _, err := workspaces.ListInvites(ctx, plainMember.ID, workspace.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a plain Member listing invites, got %v", err)
	}
}

func TestWorkspaceService_CancelInvite_RemovesItAndRequiresOwnerOrAdmin(t *testing.T) {
	workspaces, _, users := newTestWorkspaceInviteHarness(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	invite, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "bob@example.com")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	outsider, err := users.Create(ctx, "carol", "hash", false)
	if err != nil {
		t.Fatalf("create outsider: %v", err)
	}
	if err := workspaces.CancelInvite(ctx, outsider.ID, invite.Invite.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a non-member canceling an invite, got %v", err)
	}

	if err := workspaces.CancelInvite(ctx, owner.ID, invite.Invite.ID); err != nil {
		t.Fatalf("cancel invite: %v", err)
	}

	invites, err := workspaces.ListInvites(ctx, owner.ID, workspace.ID)
	if err != nil {
		t.Fatalf("list invites: %v", err)
	}
	if len(invites) != 0 {
		t.Fatalf("expected the canceled invite to be gone, got %d", len(invites))
	}
}

// TestAuthService_AcceptWorkspaceInviteNewAccount_CreatesUserAndMembership
// covers #154's first accept path: an invite for an email with no existing
// User creates the User and a Member-role WorkspaceMember atomically, and
// logs them straight in.
func TestAuthService_AcceptWorkspaceInviteNewAccount_CreatesUserAndMembership(t *testing.T) {
	workspaces, auth, users := newTestWorkspaceInviteHarness(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	invite, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "bob@example.com")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	result, err := auth.AcceptWorkspaceInviteNewAccount(ctx, invite.Token, "bob", "hunter2")
	if err != nil {
		t.Fatalf("accept invite: %v", err)
	}
	if result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatalf("expected a session to be issued, got %+v", result)
	}

	newUser, err := users.GetByEmail(ctx, "bob@example.com")
	if err != nil {
		t.Fatalf("get new user: %v", err)
	}
	if newUser.Username != "bob" {
		t.Fatalf("expected username %q, got %q", "bob", newUser.Username)
	}

	list, err := workspaces.ListForUser(ctx, newUser.ID)
	if err != nil {
		t.Fatalf("list workspaces: %v", err)
	}
	if len(list) != 1 || list[0].ID != workspace.ID {
		t.Fatalf("expected the new user to belong to the inviting workspace, got %+v", list)
	}

	// The invite is single-use: accepting it again must fail.
	if _, err := auth.AcceptWorkspaceInviteNewAccount(ctx, invite.Token, "bob2", "hunter2"); !errors.Is(err, ErrWorkspaceInviteInvalid) {
		t.Fatalf("expected a consumed invite to be rejected, got %v", err)
	}
}

// TestAuthService_AcceptWorkspaceInviteExisting_KeepsExistingWorkspaces
// covers #154's second accept path and its multi-workspace acceptance
// criterion: a User accepting an invite into a second Workspace keeps the
// one(s) they already belong to.
func TestAuthService_AcceptWorkspaceInviteExisting_KeepsExistingWorkspaces(t *testing.T) {
	workspaces, auth, users := newTestWorkspaceInviteHarness(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspaceA, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace a: %v", err)
	}

	bob, err := users.Create(ctx, "bob", "hash", false)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if _, err := users.UpdateEmail(ctx, bob.ID, "bob@example.com"); err != nil {
		t.Fatalf("set bob's email: %v", err)
	}
	workspaceB, err := workspaces.CreateForOwner(ctx, bob.ID, "Bob's Workspace")
	if err != nil {
		t.Fatalf("create workspace b: %v", err)
	}

	invite, err := workspaces.CreateInvite(ctx, owner.ID, workspaceA.ID, "bob@example.com")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	joined, err := auth.AcceptWorkspaceInviteExisting(ctx, bob.ID, invite.Token)
	if err != nil {
		t.Fatalf("accept invite: %v", err)
	}
	if joined.ID != workspaceA.ID {
		t.Fatalf("expected to join workspace a, got %+v", joined)
	}

	list, err := workspaces.ListForUser(ctx, bob.ID)
	if err != nil {
		t.Fatalf("list workspaces: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected bob to belong to both workspaces, got %+v", list)
	}
	ids := map[int64]bool{}
	for _, w := range list {
		ids[w.ID] = true
	}
	if !ids[workspaceA.ID] || !ids[workspaceB.ID] {
		t.Fatalf("expected both workspace a and b in %+v", list)
	}

	// The invite is single-use.
	if _, err := auth.AcceptWorkspaceInviteExisting(ctx, bob.ID, invite.Token); !errors.Is(err, ErrWorkspaceInviteInvalid) {
		t.Fatalf("expected a consumed invite to be rejected, got %v", err)
	}
}

func TestAuthService_AcceptWorkspaceInviteExisting_RejectsEmailMismatch(t *testing.T) {
	workspaces, auth, users := newTestWorkspaceInviteHarness(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	bob, err := users.Create(ctx, "bob", "hash", false)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if _, err := users.UpdateEmail(ctx, bob.ID, "bob@example.com"); err != nil {
		t.Fatalf("set bob's email: %v", err)
	}

	invite, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "someone-else@example.com")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	if _, err := auth.AcceptWorkspaceInviteExisting(ctx, bob.ID, invite.Token); !errors.Is(err, ErrWorkspaceInviteEmailMismatch) {
		t.Fatalf("expected ErrWorkspaceInviteEmailMismatch, got %v", err)
	}
}

func TestAuthService_AcceptWorkspaceInviteExisting_RejectsAlreadyMember(t *testing.T) {
	workspaces, auth, users := newTestWorkspaceInviteHarness(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	bob, err := users.Create(ctx, "bob", "hash", false)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if _, err := users.UpdateEmail(ctx, bob.ID, "bob@example.com"); err != nil {
		t.Fatalf("set bob's email: %v", err)
	}

	firstInvite, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "bob@example.com")
	if err != nil {
		t.Fatalf("create first invite: %v", err)
	}
	if _, err := auth.AcceptWorkspaceInviteExisting(ctx, bob.ID, firstInvite.Token); err != nil {
		t.Fatalf("accept first invite: %v", err)
	}

	secondInvite, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "bob@example.com")
	if err != nil {
		t.Fatalf("create second invite: %v", err)
	}
	if _, err := auth.AcceptWorkspaceInviteExisting(ctx, bob.ID, secondInvite.Token); !errors.Is(err, ErrAlreadyWorkspaceMember) {
		t.Fatalf("expected ErrAlreadyWorkspaceMember, got %v", err)
	}
}

func TestAuthService_PreviewWorkspaceInvite_ReportsWhetherUserExists(t *testing.T) {
	workspaces, auth, users := newTestWorkspaceInviteHarness(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	newAccountInvite, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "newperson@example.com")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	preview, err := auth.PreviewWorkspaceInvite(ctx, newAccountInvite.Token)
	if err != nil {
		t.Fatalf("preview invite: %v", err)
	}
	if preview.UserExists {
		t.Fatalf("expected UserExists false for an email with no account")
	}
	if preview.WorkspaceName != "Alice's Workspace" {
		t.Fatalf("expected workspace name %q, got %q", "Alice's Workspace", preview.WorkspaceName)
	}

	bob, err := users.Create(ctx, "bob", "hash", false)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if _, err := users.UpdateEmail(ctx, bob.ID, "bob@example.com"); err != nil {
		t.Fatalf("set bob's email: %v", err)
	}
	existingAccountInvite, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "bob@example.com")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	preview, err = auth.PreviewWorkspaceInvite(ctx, existingAccountInvite.Token)
	if err != nil {
		t.Fatalf("preview invite: %v", err)
	}
	if !preview.UserExists {
		t.Fatalf("expected UserExists true for an email with an existing account")
	}
}
