// calendar_group_share_test.go covers #159/ADR-0045's Group-targeted Share:
// dynamic Access resolution off current Group membership, the max(direct,
// group) merge, and the same-Workspace validation Share and ShareWithGroup
// both enforce.
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calendar/server/internal/repository"
)

// groupShareFixture is #159's fixture: an Owner's Calendar, a Group inside
// the same Workspace, and a member User who isn't in the Group yet — the
// setup every dynamic-grant/revoke test in this file starts from.
type groupShareFixture struct {
	calendars   *CalendarService
	groups      *repository.GroupRepository
	workspaces  *repository.WorkspaceRepository
	ownerID     int64
	memberID    int64
	workspaceID int64
	calendarID  string
	groupID     int64
}

func newGroupShareFixture(t *testing.T) groupShareFixture {
	t.Helper()
	ctx := context.Background()

	g := newTestGraph(t)

	users := g.UserRepo
	owner, err := users.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member, err := users.Create(ctx, "member", "member@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	workspaceRepo := g.WorkspaceRepo
	workspace, err := workspaceRepo.Create(ctx, "Test Workspace", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, owner.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add owner as workspace member: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, member.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add member as workspace member: %v", err)
	}

	groupRepo := g.GroupRepo
	group, err := groupRepo.Create(ctx, workspace.ID, "Tech team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	calendars := g.Calendars
	cal, err := calendars.Create(ctx, owner.ID, workspace.ID, "cal-1", CalendarWrite{Name: "Company Holidays", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	return groupShareFixture{
		calendars: calendars, groups: groupRepo, workspaces: workspaceRepo,
		ownerID: owner.ID, memberID: member.ID, workspaceID: workspace.ID,
		calendarID: cal.ID, groupID: group.ID,
	}
}

// TestCalendarService_ShareWithGroup_JoiningGrantsAccessImmediately covers
// ADR-0045's core acceptance criterion: sharing a Calendar with a Group,
// then adding a User to that Group, grants them Access without anyone
// touching the Calendar or its Shares again.
func TestCalendarService_ShareWithGroup_JoiningGrantsAccessImmediately(t *testing.T) {
	f := newGroupShareFixture(t)
	ctx := context.Background()

	if _, err := f.calendars.ShareWithGroup(ctx, f.ownerID, f.calendarID, f.groupID, repository.RoleEditor); err != nil {
		t.Fatalf("share with group: %v", err)
	}

	if access, _, err := f.calendars.Access(ctx, f.memberID, f.calendarID); err != nil || access != AccessNone {
		t.Fatalf("Access before joining the Group = %v, %v; want AccessNone, nil err", access, err)
	}

	if err := f.groups.AddMember(ctx, f.groupID, f.memberID); err != nil {
		t.Fatalf("add member to group: %v", err)
	}

	if access, _, err := f.calendars.Access(ctx, f.memberID, f.calendarID); err != nil || access != AccessEditor {
		t.Fatalf("Access after joining the Group = %v, %v; want AccessEditor, nil err", access, err)
	}
}

// TestCalendarService_ShareWithGroup_LeavingRevokesAccessImmediately covers
// the other half: removing a User from a Group revokes the Access it was
// granting them, with no Calendar or Share row touched.
func TestCalendarService_ShareWithGroup_LeavingRevokesAccessImmediately(t *testing.T) {
	f := newGroupShareFixture(t)
	ctx := context.Background()

	if _, err := f.calendars.ShareWithGroup(ctx, f.ownerID, f.calendarID, f.groupID, repository.RoleViewer); err != nil {
		t.Fatalf("share with group: %v", err)
	}
	if err := f.groups.AddMember(ctx, f.groupID, f.memberID); err != nil {
		t.Fatalf("add member to group: %v", err)
	}
	if access, _, err := f.calendars.Access(ctx, f.memberID, f.calendarID); err != nil || access != AccessViewer {
		t.Fatalf("Access while in the Group = %v, %v; want AccessViewer, nil err", access, err)
	}

	if err := f.groups.RemoveMember(ctx, f.groupID, f.memberID); err != nil {
		t.Fatalf("remove member from group: %v", err)
	}

	if access, _, err := f.calendars.Access(ctx, f.memberID, f.calendarID); err != nil || access != AccessNone {
		t.Fatalf("Access after leaving the Group = %v, %v; want AccessNone, nil err", access, err)
	}
}

// TestCalendarService_Access_MaxOfDirectAndGroupShare covers ADR-0045's
// max(direct share role, any group share role) merge: a Viewer direct Share
// plus an Editor Group Share resolves to Editor, regardless of which one was
// granted first.
func TestCalendarService_Access_MaxOfDirectAndGroupShare(t *testing.T) {
	f := newGroupShareFixture(t)
	ctx := context.Background()

	if _, _, err := f.calendars.Share(ctx, f.ownerID, f.calendarID, "member@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("direct share: %v", err)
	}
	if _, err := f.calendars.ShareWithGroup(ctx, f.ownerID, f.calendarID, f.groupID, repository.RoleEditor); err != nil {
		t.Fatalf("share with group: %v", err)
	}
	if err := f.groups.AddMember(ctx, f.groupID, f.memberID); err != nil {
		t.Fatalf("add member to group: %v", err)
	}

	if access, _, err := f.calendars.Access(ctx, f.memberID, f.calendarID); err != nil || access != AccessEditor {
		t.Fatalf("Access = %v, %v; want AccessEditor (max of viewer direct + editor group), nil err", access, err)
	}
}

// TestCalendarService_ShareWithGroup_ChangesRole covers "an Owner can change
// an existing Group Share's Role", mirroring Share_ChangesRole.
func TestCalendarService_ShareWithGroup_ChangesRole(t *testing.T) {
	f := newGroupShareFixture(t)
	ctx := context.Background()

	if _, err := f.calendars.ShareWithGroup(ctx, f.ownerID, f.calendarID, f.groupID, repository.RoleViewer); err != nil {
		t.Fatalf("initial share: %v", err)
	}
	updated, err := f.calendars.ShareWithGroup(ctx, f.ownerID, f.calendarID, f.groupID, repository.RoleEditor)
	if err != nil {
		t.Fatalf("re-share with new role: %v", err)
	}
	if updated.Role != repository.RoleEditor {
		t.Fatalf("role = %q, want %q", updated.Role, repository.RoleEditor)
	}

	shares, err := f.calendars.ListGroupShares(ctx, f.ownerID, f.calendarID)
	if err != nil {
		t.Fatalf("list group shares: %v", err)
	}
	if len(shares) != 1 {
		t.Fatalf("expected exactly one group share after a role change, got %d", len(shares))
	}
}

func TestCalendarService_ShareWithGroup_InvalidRole(t *testing.T) {
	f := newGroupShareFixture(t)

	_, err := f.calendars.ShareWithGroup(context.Background(), f.ownerID, f.calendarID, f.groupID, "admin")
	if !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("err = %v, want ErrInvalidRole", err)
	}
}

// TestCalendarService_ShareWithGroup_NonOwnerRefused mirrors
// Share_NonOwnerRefused: only the Calendar's Owner may grant a Group Share.
func TestCalendarService_ShareWithGroup_NonOwnerRefused(t *testing.T) {
	f := newGroupShareFixture(t)

	_, err := f.calendars.ShareWithGroup(context.Background(), f.memberID, f.calendarID, f.groupID, repository.RoleViewer)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestCalendarService_ShareWithGroup_UnknownGroupRefused covers a groupID
// that doesn't exist at all.
func TestCalendarService_ShareWithGroup_UnknownGroupRefused(t *testing.T) {
	f := newGroupShareFixture(t)

	_, err := f.calendars.ShareWithGroup(context.Background(), f.ownerID, f.calendarID, 999999, repository.RoleViewer)
	if !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("err = %v, want ErrGroupNotFound", err)
	}
}

// TestCalendarService_ShareWithGroup_RejectsGroupFromAnotherWorkspace covers
// #159's "Share creation is refused if the target ... Group doesn't belong
// to the Calendar's Workspace" acceptance criterion.
func TestCalendarService_ShareWithGroup_RejectsGroupFromAnotherWorkspace(t *testing.T) {
	f := newGroupShareFixture(t)
	ctx := context.Background()

	otherWorkspace, err := f.workspaces.Create(ctx, "Other Workspace", f.ownerID)
	if err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	foreignGroup, err := f.groups.Create(ctx, otherWorkspace.ID, "Foreign group")
	if err != nil {
		t.Fatalf("create foreign group: %v", err)
	}

	_, err = f.calendars.ShareWithGroup(ctx, f.ownerID, f.calendarID, foreignGroup.ID, repository.RoleViewer)
	if !errors.Is(err, ErrShareTargetNotInWorkspace) {
		t.Fatalf("err = %v, want ErrShareTargetNotInWorkspace", err)
	}
}

// TestCalendarService_Share_RejectsUserFromAnotherWorkspace covers #159's
// same acceptance criterion for a direct, per-User Share: a User who exists
// on the instance but doesn't belong to the Calendar's own Workspace is
// refused.
func TestCalendarService_Share_RejectsUserFromAnotherWorkspace(t *testing.T) {
	f := newGroupShareFixture(t)
	ctx := context.Background()

	outsider, err := f.calendars.users.Create(ctx, "outsider", "outsider@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create outsider: %v", err)
	}
	otherWorkspace, err := f.workspaces.Create(ctx, "Other Workspace", outsider.ID)
	if err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	if err := f.workspaces.AddMember(ctx, otherWorkspace.ID, outsider.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add outsider as their own workspace's owner: %v", err)
	}

	_, _, err = f.calendars.Share(ctx, f.ownerID, f.calendarID, "outsider@example.com", repository.RoleViewer)
	if !errors.Is(err, ErrShareTargetNotInWorkspace) {
		t.Fatalf("err = %v, want ErrShareTargetNotInWorkspace", err)
	}
}

// TestCalendarService_RevokeGroupShare covers revoking a Group Share
// directly (distinct from a member simply leaving the Group): Access drops
// even for every current Group Member.
func TestCalendarService_RevokeGroupShare(t *testing.T) {
	f := newGroupShareFixture(t)
	ctx := context.Background()

	if _, err := f.calendars.ShareWithGroup(ctx, f.ownerID, f.calendarID, f.groupID, repository.RoleEditor); err != nil {
		t.Fatalf("share with group: %v", err)
	}
	if err := f.groups.AddMember(ctx, f.groupID, f.memberID); err != nil {
		t.Fatalf("add member to group: %v", err)
	}

	if err := f.calendars.RevokeGroupShare(ctx, f.ownerID, f.calendarID, f.groupID); err != nil {
		t.Fatalf("revoke group share: %v", err)
	}

	if access, _, err := f.calendars.Access(ctx, f.memberID, f.calendarID); err != nil || access != AccessNone {
		t.Fatalf("Access after revoking the group share = %v, %v; want AccessNone, nil err", access, err)
	}
}

// TestCalendarService_RevokeGroupShare_NonOwnerRefused mirrors
// RevokeShare_NonOwnerRefused.
func TestCalendarService_RevokeGroupShare_NonOwnerRefused(t *testing.T) {
	f := newGroupShareFixture(t)
	ctx := context.Background()

	if _, err := f.calendars.ShareWithGroup(ctx, f.ownerID, f.calendarID, f.groupID, repository.RoleViewer); err != nil {
		t.Fatalf("share with group: %v", err)
	}

	err := f.calendars.RevokeGroupShare(ctx, f.memberID, f.calendarID, f.groupID)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestCalendarService_ListAccessible_IncludesGroupSharedCalendars covers
// ListAccessible surfacing a Calendar reached only via a Group Share,
// resolved with the Group's Role.
func TestCalendarService_ListAccessible_IncludesGroupSharedCalendars(t *testing.T) {
	f := newGroupShareFixture(t)
	ctx := context.Background()

	if _, err := f.calendars.ShareWithGroup(ctx, f.ownerID, f.calendarID, f.groupID, repository.RoleEditor); err != nil {
		t.Fatalf("share with group: %v", err)
	}
	if err := f.groups.AddMember(ctx, f.groupID, f.memberID); err != nil {
		t.Fatalf("add member to group: %v", err)
	}

	result, err := f.calendars.ListAccessible(ctx, f.memberID)
	if err != nil {
		t.Fatalf("list accessible: %v", err)
	}
	if len(result) != 1 || result[0].ID != f.calendarID || result[0].Access != AccessEditor {
		t.Fatalf("unexpected accessible calendars: %+v", result)
	}
}

// TestCalendarService_ShareTargets_ExcludesOwnerAndScopesToWorkspace covers
// #159's share-dialog data source: every other Member and every Group of the
// Calendar's own Workspace, never the Owner themselves.
func TestCalendarService_ShareTargets_ExcludesOwnerAndScopesToWorkspace(t *testing.T) {
	f := newGroupShareFixture(t)
	ctx := context.Background()

	users, groups, err := f.calendars.ShareTargets(ctx, f.ownerID, f.calendarID)
	if err != nil {
		t.Fatalf("share targets: %v", err)
	}

	for _, u := range users {
		if u.UserID == f.ownerID {
			t.Fatalf("expected the Owner to be excluded from share targets, got %+v", users)
		}
	}
	if len(users) != 1 || users[0].UserID != f.memberID {
		t.Fatalf("unexpected user targets: %+v", users)
	}
	if len(groups) != 1 || groups[0].ID != f.groupID {
		t.Fatalf("unexpected group targets: %+v", groups)
	}
}
