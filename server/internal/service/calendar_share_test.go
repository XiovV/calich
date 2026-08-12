package service

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// newTestShareService returns a CalendarService plus its UserRepository (so
// a test can mint extra fixture Users), an Owner, a second User to share
// with, and an ordinary Calendar owned by the Owner — #100's fixture for
// Share management tests.
func newTestShareService(t *testing.T) (svc *CalendarService, users *repository.UserRepository, ownerID, otherID int64, calendarID string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users = repository.NewUserRepository(sqlDB)
	owner, err := users.Create(context.Background(), "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := users.Create(context.Background(), "other", "other@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspace, err := workspaceRepo.Create(context.Background(), "Test Workspace", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaceRepo.AddMember(context.Background(), workspace.ID, owner.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}
	// A Share can only ever reach someone already inside the Calendar's own
	// Workspace (#159, ADR-0045), so the fixture's share target must be a
	// Member of it too.
	if err := workspaceRepo.AddMember(context.Background(), workspace.ID, other.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add other as workspace member: %v", err)
	}

	svc = NewCalendarService(repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	calendar, err := svc.Create(context.Background(), owner.ID, workspace.ID, "cal-1", CalendarWrite{Name: "Family", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	return svc, users, owner.ID, other.ID, calendar.ID
}

func TestCalendarService_Share(t *testing.T) {
	svc, _, ownerID, _, calendarID := newTestShareService(t)
	ctx := context.Background()

	share, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleEditor)
	if err != nil {
		t.Fatalf("share: %v", err)
	}
	if share.Role != repository.RoleEditor {
		t.Fatalf("role = %q, want %q", share.Role, repository.RoleEditor)
	}
}

// TestCalendarService_Share_CaseInsensitiveEmail covers #196 (ADR-0058):
// Share-target resolution is Email-keyed (ADR-0047), so typing the target's
// address with different case than it's stored in must still resolve to the
// same User, since users.email is COLLATE NOCASE.
func TestCalendarService_Share_CaseInsensitiveEmail(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	share, _, err := svc.Share(ctx, ownerID, calendarID, "Other@Example.com", repository.RoleEditor)
	if err != nil {
		t.Fatalf("share: %v", err)
	}
	if share.UserID != otherID {
		t.Fatalf("expected share to resolve to user %d, got %d", otherID, share.UserID)
	}
}

// TestCalendarService_Share_ChangesRole covers "an Owner can change an
// existing Share's Role" (#100's acceptance criteria) — sharing again with
// a different Role updates it rather than erroring or creating a second
// grant.
func TestCalendarService_Share_ChangesRole(t *testing.T) {
	svc, _, ownerID, _, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("initial share: %v", err)
	}
	updated, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleEditor)
	if err != nil {
		t.Fatalf("re-share with new role: %v", err)
	}
	if updated.Role != repository.RoleEditor {
		t.Fatalf("role = %q, want %q", updated.Role, repository.RoleEditor)
	}

	shares, err := svc.ListShares(ctx, ownerID, calendarID)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	if len(shares) != 1 {
		t.Fatalf("expected exactly one share after a role change, got %d", len(shares))
	}
}

func TestCalendarService_Share_InvalidRole(t *testing.T) {
	svc, _, ownerID, _, calendarID := newTestShareService(t)

	_, _, err := svc.Share(context.Background(), ownerID, calendarID, "other@example.com", "admin")
	if !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("err = %v, want ErrInvalidRole", err)
	}
}

func TestCalendarService_Share_UnknownUsername(t *testing.T) {
	svc, _, ownerID, _, calendarID := newTestShareService(t)

	_, _, err := svc.Share(context.Background(), ownerID, calendarID, "ghost@example.com", repository.RoleViewer)
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
}

// TestCalendarService_Share_HidesDisabledUsers covers ADR-0037's "a Disabled
// User is hidden from the share picker" guard: sharing with one looks
// identical to sharing with a username that doesn't exist.
func TestCalendarService_Share_HidesDisabledUsers(t *testing.T) {
	svc, users, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, err := users.SetDisabled(ctx, otherID, true); err != nil {
		t.Fatalf("disable other user: %v", err)
	}

	_, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer)
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
}

// TestCalendarService_Share_ExistingSharesSurviveDisabling covers ADR-0037's
// "existing Shares survive" guarantee: disabling only blocks *new* Shares —
// one already granted keeps working exactly as before.
func TestCalendarService_Share_ExistingSharesSurviveDisabling(t *testing.T) {
	svc, users, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleEditor); err != nil {
		t.Fatalf("share: %v", err)
	}
	if _, err := users.SetDisabled(ctx, otherID, true); err != nil {
		t.Fatalf("disable other user: %v", err)
	}

	shares, err := svc.ListShares(ctx, ownerID, calendarID)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	if len(shares) != 1 || shares[0].Role != repository.RoleEditor {
		t.Fatalf("expected the existing share to survive disabling, got %+v", shares)
	}
}

func TestCalendarService_Share_WithSelf(t *testing.T) {
	svc, _, ownerID, _, calendarID := newTestShareService(t)

	_, _, err := svc.Share(context.Background(), ownerID, calendarID, "owner@example.com", repository.RoleViewer)
	if !errors.Is(err, ErrCannotShareWithSelf) {
		t.Fatalf("err = %v, want ErrCannotShareWithSelf", err)
	}
}

// TestCalendarService_Share_NonOwnerRefused covers "only the Owner can ...
// re-share" (ADR-0034): a stranger — someone with no Access at all —
// attempting to grant a Share on a Calendar they don't own gets not-found,
// the same as any other address-a-Calendar-you-can't-see attempt.
func TestCalendarService_Share_NonOwnerRefused(t *testing.T) {
	svc, _, _, otherID, calendarID := newTestShareService(t)

	_, _, err := svc.Share(context.Background(), otherID, calendarID, "other@example.com", repository.RoleViewer)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestCalendarService_Share_EditorCannotReshare covers ADR-0034's "Role
// covers Events only" — an Editor, who does have real Access, still can't
// manage the Calendar itself by granting a Share to a third User.
func TestCalendarService_Share_EditorCannotReshare(t *testing.T) {
	svc, users, ownerID, editorID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleEditor); err != nil {
		t.Fatalf("share: %v", err)
	}

	if _, err := users.Create(ctx, "third", "third@example.com", "hash", false); err != nil {
		t.Fatalf("create third user: %v", err)
	}

	_, _, err := svc.Share(ctx, editorID, calendarID, "third@example.com", repository.RoleViewer)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCalendarService_RevokeShare(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}

	if err := svc.RevokeShare(ctx, ownerID, calendarID, otherID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if access, _, err := svc.Access(ctx, otherID, calendarID); err != nil || access != AccessNone {
		t.Fatalf("Access after revoke = %v, %v; want AccessNone, nil err", access, err)
	}
}

func TestCalendarService_RevokeShare_NonOwnerRefused(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}

	err := svc.RevokeShare(ctx, otherID, calendarID, otherID)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCalendarService_ListShares(t *testing.T) {
	svc, _, ownerID, _, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleEditor); err != nil {
		t.Fatalf("share: %v", err)
	}

	shares, err := svc.ListShares(ctx, ownerID, calendarID)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	if len(shares) != 1 || shares[0].Name != "other" || shares[0].Role != repository.RoleEditor {
		t.Fatalf("unexpected shares: %+v", shares)
	}
}

func TestCalendarService_ListShares_NonOwnerRefused(t *testing.T) {
	svc, _, _, otherID, calendarID := newTestShareService(t)

	_, err := svc.ListShares(context.Background(), otherID, calendarID)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestCalendarService_LeaveShare covers "a User can leave a Calendar shared
// with them without involving the Owner" (#100's acceptance criteria).
func TestCalendarService_LeaveShare(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}

	if err := svc.LeaveShare(ctx, otherID, calendarID); err != nil {
		t.Fatalf("leave: %v", err)
	}

	if access, _, err := svc.Access(ctx, otherID, calendarID); err != nil || access != AccessNone {
		t.Fatalf("Access after leaving = %v, %v; want AccessNone, nil err", access, err)
	}
}

// TestCalendarService_LeaveShare_OwnerHasNoShareToLeave covers that an
// Owner, who never holds a Share row of their own, gets not-found rather
// than silently no-op'ing if they call LeaveShare on their own Calendar.
func TestCalendarService_LeaveShare_OwnerHasNoShareToLeave(t *testing.T) {
	svc, _, ownerID, _, calendarID := newTestShareService(t)

	err := svc.LeaveShare(context.Background(), ownerID, calendarID)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCalendarService_ListAccessible(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleEditor); err != nil {
		t.Fatalf("share: %v", err)
	}

	otherWorkspace, err := svc.workspaces.Create(ctx, "Other's Workspace", otherID)
	if err != nil {
		t.Fatalf("create other's workspace: %v", err)
	}
	if err := svc.workspaces.AddMember(ctx, otherWorkspace.ID, otherID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add other's workspace member: %v", err)
	}

	own, err := svc.Create(ctx, otherID, otherWorkspace.ID, "cal-own", CalendarWrite{Name: "Personal", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create other's own calendar: %v", err)
	}

	result, err := svc.ListAccessible(ctx, otherID)
	if err != nil {
		t.Fatalf("list accessible: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 accessible calendars, got %d: %+v", len(result), result)
	}

	byID := make(map[string]Access, len(result))
	for _, c := range result {
		byID[c.ID] = c.Access
	}
	if byID[calendarID] != AccessEditor {
		t.Fatalf("shared calendar Access = %v, want AccessEditor", byID[calendarID])
	}
	if byID[own.ID] != AccessOwner {
		t.Fatalf("own calendar Access = %v, want AccessOwner", byID[own.ID])
	}
}
