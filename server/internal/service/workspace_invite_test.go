package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// newTestWorkspaceInviteHarness wires a WorkspaceService and an AuthService
// against the same in-memory database — #154's issue and accept paths sit on
// different services (mirroring AccountService/AuthService's split for the
// account-level Invite this replaces), but exercise the same rows.
func newTestWorkspaceInviteHarness(t *testing.T) (*WorkspaceService, *AuthService, *repository.UserRepository) {
	workspaces, auth, users, _, _, _ := newTestWorkspaceInviteHarnessWithAttendees(t)
	return workspaces, auth, users
}

// newTestWorkspaceInviteHarnessWithAttendees extends
// newTestWorkspaceInviteHarness with the Calendar/Event/Attendee
// repositories a conversion test (#203, ADR-0058) needs to seed an
// email-shaped Attendee row on an Event ahead of accepting the Workspace
// Invite that should sweep it up.
func newTestWorkspaceInviteHarnessWithAttendees(t *testing.T) (*WorkspaceService, *AuthService, *repository.UserRepository, *repository.CalendarRepository, *repository.EventRepository, *repository.AttendeeRepository) {
	t.Helper()

	g := newTestGraph(t)

	return g.Workspaces, g.Auth, g.UserRepo, g.CalendarRepo, g.EventRepo, g.AttendeeRepo
}

func TestWorkspaceService_CreateInvite_RequiresOwnerOrAdmin(t *testing.T) {
	workspaces, _, users := newTestWorkspaceInviteHarness(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "alice@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	plainMember, err := users.Create(ctx, "bob", "bob@example.com", "hash", false)
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

	outsider, err := users.Create(ctx, "carol", "carol@example.com", "hash", false)
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

	owner, err := users.Create(ctx, "alice", "alice@example.com", "hash", false)
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

	owner, err := users.Create(ctx, "alice", "alice@example.com", "hash", false)
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

	owner, err := users.Create(ctx, "alice", "alice@example.com", "hash", false)
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

	outsider, err := users.Create(ctx, "carol", "carol@example.com", "hash", false)
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

	owner, err := users.Create(ctx, "alice", "alice@example.com", "hash", false)
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

	plainMember, err := users.Create(ctx, "dave", "dave@example.com", "hash", false)
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

	owner, err := users.Create(ctx, "alice", "alice@example.com", "hash", false)
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

	outsider, err := users.Create(ctx, "carol", "carol@example.com", "hash", false)
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

	owner, err := users.Create(ctx, "alice", "alice@example.com", "hash", false)
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
	if newUser.Name != "bob" {
		t.Fatalf("expected name %q, got %q", "bob", newUser.Name)
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

	// #172: the new account gets the three default Calendars, seeded into
	// the Workspace the Invite admitted them to.
	newUserCalendars, err := auth.calendars.List(ctx, newUser.ID)
	if err != nil {
		t.Fatalf("list new user's calendars: %v", err)
	}
	want := map[string]bool{"Personal": true, "Work": true, "Family": true}
	if len(newUserCalendars) != len(want) {
		t.Fatalf("expected %d default calendars, got %d", len(want), len(newUserCalendars))
	}
	for _, c := range newUserCalendars {
		if !want[c.Name] {
			t.Fatalf("unexpected default calendar %q", c.Name)
		}
		if c.WorkspaceID != workspace.ID {
			t.Fatalf("expected default calendar %q to be seeded in the inviting workspace, got %d", c.Name, c.WorkspaceID)
		}
		delete(want, c.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing default calendars: %v", want)
	}
}

// TestAuthService_AcceptWorkspaceInviteExisting_KeepsExistingWorkspaces
// covers #154's second accept path and its multi-workspace acceptance
// criterion: a User accepting an invite into a second Workspace keeps the
// one(s) they already belong to.
func TestAuthService_AcceptWorkspaceInviteExisting_KeepsExistingWorkspaces(t *testing.T) {
	workspaces, auth, users := newTestWorkspaceInviteHarness(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "alice@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspaceA, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace a: %v", err)
	}

	bob, err := users.Create(ctx, "bob", "bob@example.com", "hash", false)
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

	owner, err := users.Create(ctx, "alice", "alice@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	bob, err := users.Create(ctx, "bob", "bob@example.com", "hash", false)
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

	owner, err := users.Create(ctx, "alice", "alice@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	bob, err := users.Create(ctx, "bob", "bob@example.com", "hash", false)
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

// TestWorkspaceService_CreateInvite_FoldsEmailCase covers #196 (ADR-0058):
// an invite typed with stray capitals is stored folded to lowercase, the
// same as every other email write path.
func TestWorkspaceService_CreateInvite_FoldsEmailCase(t *testing.T) {
	workspaces, _, users := newTestWorkspaceInviteHarness(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "alice@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	invite, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "Bob@Example.com")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if invite.Invite.Email != "bob@example.com" {
		t.Fatalf("expected invite email to be folded to lowercase, got %q", invite.Invite.Email)
	}
}

// TestAuthService_AcceptWorkspaceInviteExisting_CaseInsensitiveEmailMatch
// covers #196's third acceptance criterion directly: a Workspace Invite
// issued for "Damir@x.com" is accepted by a User whose stored Email is
// "damir@x.com" — both sides fold to the same value on write, so the plain
// Go string comparison in AcceptWorkspaceInviteExisting agrees.
func TestAuthService_AcceptWorkspaceInviteExisting_CaseInsensitiveEmailMatch(t *testing.T) {
	workspaces, auth, users := newTestWorkspaceInviteHarness(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "alice@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	bob, err := users.Create(ctx, "bob", "damir@x.com", "hash", false)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	invite, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "Damir@x.com")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	joined, err := auth.AcceptWorkspaceInviteExisting(ctx, bob.ID, invite.Token)
	if err != nil {
		t.Fatalf("expected the case-differing invite to be accepted, got: %v", err)
	}
	if joined.ID != workspace.ID {
		t.Fatalf("expected to join %d, got %+v", workspace.ID, joined)
	}
}

func TestAuthService_PreviewWorkspaceInvite_ReportsWhetherUserExists(t *testing.T) {
	workspaces, auth, users := newTestWorkspaceInviteHarness(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "alice@example.com", "hash", false)
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

	bob, err := users.Create(ctx, "bob", "bob@example.com", "hash", false)
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

// mustCreateWorkspaceInviteTestEvent creates a Calendar owned by ownerID in
// workspaceID and one Event on it — the fixture #203's conversion tests need
// to hang an email-shaped Attendee off.
func mustCreateWorkspaceInviteTestEvent(t *testing.T, calendars *repository.CalendarRepository, events *repository.EventRepository, ownerID, workspaceID int64, calendarID, eventID string) {
	t.Helper()
	ctx := context.Background()

	if _, err := calendars.Create(ctx, ownerID, workspaceID, calendarID, repository.CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
		t.Fatalf("create calendar %q: %v", calendarID, err)
	}
	start, err := time.Parse(time.RFC3339, "2026-01-01T09:00:00Z")
	if err != nil {
		t.Fatalf("parse start: %v", err)
	}
	end, err := time.Parse(time.RFC3339, "2026-01-01T10:00:00Z")
	if err != nil {
		t.Fatalf("parse end: %v", err)
	}
	if _, err := events.Create(ctx, eventID, &ownerID, repository.EventFields{CalendarID: calendarID, Title: eventID, Start: start, End: end}, 0); err != nil {
		t.Fatalf("create event %q: %v", eventID, err)
	}
}

// TestAuthService_AcceptWorkspaceInviteExisting_ConvertsEmailAttendees covers
// #203's core acceptance criterion: accepting a Workspace Invite converts an
// outstanding email-shaped Attendee row for that address, on an Event in the
// inviting Workspace, onto the accepting User — carrying its Response
// across.
func TestAuthService_AcceptWorkspaceInviteExisting_ConvertsEmailAttendees(t *testing.T) {
	workspaces, auth, users, calendarRepo, eventRepo, attendeeRepo := newTestWorkspaceInviteHarnessWithAttendees(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "alice@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	mustCreateWorkspaceInviteTestEvent(t, calendarRepo, eventRepo, owner.ID, workspace.ID, "cal-1", "evt-1")

	if _, err := attendeeRepo.AddEmail(ctx, "evt-1", "bob@example.com"); err != nil {
		t.Fatalf("add email attendee: %v", err)
	}
	if _, err := attendeeRepo.SetResponseByEmail(ctx, "evt-1", "bob@example.com", repository.ResponseAccepted); err != nil {
		t.Fatalf("set email attendee response: %v", err)
	}

	bob, err := users.Create(ctx, "bob", "bob@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	invite, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "bob@example.com")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if _, err := auth.AcceptWorkspaceInviteExisting(ctx, bob.ID, invite.Token); err != nil {
		t.Fatalf("accept invite: %v", err)
	}

	converted, err := attendeeRepo.Get(ctx, "evt-1", bob.ID)
	if err != nil {
		t.Fatalf("expected a User-backed Attendee row for bob, got: %v", err)
	}
	if converted.Response != repository.ResponseAccepted {
		t.Fatalf("expected the carried-over response %q, got %q", repository.ResponseAccepted, converted.Response)
	}

	attendees, err := attendeeRepo.ListByEventID(ctx, "evt-1")
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 1 || attendees[0].Name != "bob" {
		t.Fatalf("expected the sole Attendee to render by Name, got %+v", attendees)
	}
}

// TestAuthService_AcceptWorkspaceInviteNewAccount_ConvertsEmailAttendees
// covers #203's other accept path: the brand-new account AcceptWorkspaceInviteNewAccount
// creates is what the email-shaped rows convert onto.
func TestAuthService_AcceptWorkspaceInviteNewAccount_ConvertsEmailAttendees(t *testing.T) {
	workspaces, auth, users, calendarRepo, eventRepo, attendeeRepo := newTestWorkspaceInviteHarnessWithAttendees(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "alice@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := workspaces.CreateForOwner(ctx, owner.ID, "Alice's Workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	mustCreateWorkspaceInviteTestEvent(t, calendarRepo, eventRepo, owner.ID, workspace.ID, "cal-1", "evt-1")

	if _, err := attendeeRepo.AddEmail(ctx, "evt-1", "bob@example.com"); err != nil {
		t.Fatalf("add email attendee: %v", err)
	}
	if _, err := attendeeRepo.SetResponseByEmail(ctx, "evt-1", "bob@example.com", repository.ResponseTentative); err != nil {
		t.Fatalf("set email attendee response: %v", err)
	}

	invite, err := workspaces.CreateInvite(ctx, owner.ID, workspace.ID, "bob@example.com")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if _, err := auth.AcceptWorkspaceInviteNewAccount(ctx, invite.Token, "bob", "hunter2"); err != nil {
		t.Fatalf("accept invite: %v", err)
	}

	newUser, err := users.GetByEmail(ctx, "bob@example.com")
	if err != nil {
		t.Fatalf("get new user: %v", err)
	}

	converted, err := attendeeRepo.Get(ctx, "evt-1", newUser.ID)
	if err != nil {
		t.Fatalf("expected a User-backed Attendee row for the new account, got: %v", err)
	}
	if converted.Response != repository.ResponseTentative {
		t.Fatalf("expected the carried-over response %q, got %q", repository.ResponseTentative, converted.Response)
	}
}

// TestAuthService_AcceptWorkspaceInviteExisting_ConversionOnlyTouchesInvitingWorkspace
// covers #203's "Attendee rows on Events in other Workspaces are untouched"
// acceptance criterion at the service layer: accepting an invite into
// workspace A must not sweep an email-shaped row bob also holds on an Event
// in workspace B.
func TestAuthService_AcceptWorkspaceInviteExisting_ConversionOnlyTouchesInvitingWorkspace(t *testing.T) {
	workspaces, auth, users, calendarRepo, eventRepo, attendeeRepo := newTestWorkspaceInviteHarnessWithAttendees(t)
	ctx := context.Background()

	owner, err := users.Create(ctx, "alice", "alice@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspaceA, err := workspaces.CreateForOwner(ctx, owner.ID, "Workspace A")
	if err != nil {
		t.Fatalf("create workspace a: %v", err)
	}
	workspaceB, err := workspaces.CreateForOwner(ctx, owner.ID, "Workspace B")
	if err != nil {
		t.Fatalf("create workspace b: %v", err)
	}
	mustCreateWorkspaceInviteTestEvent(t, calendarRepo, eventRepo, owner.ID, workspaceA.ID, "cal-a", "evt-a")
	mustCreateWorkspaceInviteTestEvent(t, calendarRepo, eventRepo, owner.ID, workspaceB.ID, "cal-b", "evt-b")

	if _, err := attendeeRepo.AddEmail(ctx, "evt-a", "bob@example.com"); err != nil {
		t.Fatalf("add email attendee in workspace a: %v", err)
	}
	if _, err := attendeeRepo.AddEmail(ctx, "evt-b", "bob@example.com"); err != nil {
		t.Fatalf("add email attendee in workspace b: %v", err)
	}

	bob, err := users.Create(ctx, "bob", "bob@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	invite, err := workspaces.CreateInvite(ctx, owner.ID, workspaceA.ID, "bob@example.com")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if _, err := auth.AcceptWorkspaceInviteExisting(ctx, bob.ID, invite.Token); err != nil {
		t.Fatalf("accept invite: %v", err)
	}

	if _, err := attendeeRepo.Get(ctx, "evt-a", bob.ID); err != nil {
		t.Fatalf("expected workspace a's row converted, got: %v", err)
	}
	attendees, err := attendeeRepo.ListByEventID(ctx, "evt-b")
	if err != nil {
		t.Fatalf("list workspace b attendees: %v", err)
	}
	if len(attendees) != 1 || attendees[0].UserID != nil || attendees[0].Email != "bob@example.com" {
		t.Fatalf("expected workspace b's email-shaped row untouched, got %+v", attendees)
	}
}
