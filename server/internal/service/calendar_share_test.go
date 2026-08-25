package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// newTestShareService returns a CalendarService plus its UserRepository (so
// a test can mint extra fixture Users), an Owner, a second User to share
// with, and an ordinary Calendar owned by the Owner — #100's fixture for
// Share management tests.
func newTestShareService(t *testing.T) (svc *CalendarService, users *repository.UserRepository, ownerID, otherID int64, calendarID string) {
	t.Helper()

	g := newTestGraph(t)

	users = g.UserRepo
	owner, err := users.Create(context.Background(), "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := users.Create(context.Background(), "other", "other@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	workspaceRepo := g.WorkspaceRepo
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

	svc = g.Calendars
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

// TestCalendarService_RevokeAndLeaveShare_AreAtomic asserts that when the
// last of RevokeShare's/LeaveShare's five writes fails, the four earlier
// ones in the same sequence — the Share delete and clearUserCalendarState's
// event-reminder, default-reminder, and explicit-marker clears — are rolled
// back too, never leaving the Share gone but its state orphaned (#259,
// ADR-0018). Like TestEventService_Update_DiscardOnRuleChangeIsAtomic, the
// colour override delete is a plain DELETE with no natural collision to trip
// through the public API, so the failure here is forced with a poison
// trigger instead.
func TestCalendarService_RevokeAndLeaveShare_AreAtomic(t *testing.T) {
	tests := []struct {
		name string
		end  func(svc *CalendarService, ctx context.Context, ownerID, otherID int64, calendarID string) error
	}{
		{"RevokeShare", func(svc *CalendarService, ctx context.Context, ownerID, otherID int64, calendarID string) error {
			return svc.RevokeShare(ctx, ownerID, calendarID, otherID)
		}},
		{"LeaveShare", func(svc *CalendarService, ctx context.Context, ownerID, otherID int64, calendarID string) error {
			return svc.LeaveShare(ctx, otherID, calendarID)
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := newTestGraph(t)
			ctx := context.Background()

			owner, err := g.UserRepo.Create(ctx, "owner", "owner@example.com", "hash", false)
			if err != nil {
				t.Fatalf("create owner: %v", err)
			}
			other, err := g.UserRepo.Create(ctx, "other", "other@example.com", "hash", false)
			if err != nil {
				t.Fatalf("create other user: %v", err)
			}
			workspace, err := g.WorkspaceRepo.Create(ctx, "Test Workspace", owner.ID)
			if err != nil {
				t.Fatalf("create workspace: %v", err)
			}
			if err := g.WorkspaceRepo.AddMember(ctx, workspace.ID, owner.ID, repository.WorkspaceRoleOwner); err != nil {
				t.Fatalf("add owner as workspace member: %v", err)
			}
			if err := g.WorkspaceRepo.AddMember(ctx, workspace.ID, other.ID, repository.WorkspaceRoleMember); err != nil {
				t.Fatalf("add other as workspace member: %v", err)
			}

			svc := g.Calendars
			calendar, err := svc.Create(ctx, owner.ID, workspace.ID, "cal-1", CalendarWrite{Name: "Family", Color: "#12809CFF"})
			if err != nil {
				t.Fatalf("create calendar: %v", err)
			}
			if _, _, err := svc.Share(ctx, owner.ID, calendar.ID, "other@example.com", repository.RoleViewer); err != nil {
				t.Fatalf("share: %v", err)
			}

			event, err := g.Events.Create(ctx, owner.ID, "evt-1", EventWrite{CalendarID: calendar.ID, Title: "Standup", Start: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)})
			if err != nil {
				t.Fatalf("create event: %v", err)
			}
			// SetReminders writes both event_reminders and, via its own
			// explicit-marker write, event_reminders_explicit — the two
			// tables clearUserCalendarState's first and third deletes target.
			if _, err := g.Events.SetReminders(ctx, other.ID, event.ID, []repository.Reminder{{OffsetMinutes: 15, Channel: "notification"}}); err != nil {
				t.Fatalf("set event reminders: %v", err)
			}
			if _, err := svc.SetDefaultReminders(ctx, other.ID, calendar.ID, false, []repository.Reminder{{OffsetMinutes: 30, Channel: "notification"}}); err != nil {
				t.Fatalf("set default reminders: %v", err)
			}
			if _, err := svc.SetColorOverride(ctx, other.ID, calendar.ID, "#654321"); err != nil {
				t.Fatalf("set color override: %v", err)
			}

			if _, err := g.DB.ExecContext(ctx, fmt.Sprintf(
				`CREATE TRIGGER poison_color_delete BEFORE DELETE ON calendar_user_colors WHEN OLD.user_id = %d AND OLD.calendar_id = %q BEGIN SELECT RAISE(ABORT, 'boom'); END`,
				other.ID, calendar.ID,
			)); err != nil {
				t.Fatalf("install poison trigger: %v", err)
			}

			if err := tc.end(svc, ctx, owner.ID, other.ID, calendar.ID); err == nil {
				t.Fatalf("expected %s to fail once the color override delete is poisoned", tc.name)
			}

			if access, _, err := svc.Access(ctx, other.ID, calendar.ID); err != nil || access == AccessNone {
				t.Fatalf("Access after failed %s = %v, %v; want the Share to have survived the rollback", tc.name, access, err)
			}

			reminders, err := g.EventReminderRepo.ListByEventIDs(ctx, []string{event.ID}, []int64{other.ID})
			if err != nil {
				t.Fatalf("list event reminders: %v", err)
			}
			if len(reminders[event.ID][other.ID]) != 1 {
				t.Fatalf("expected the event reminder to have survived the rollback, got %+v", reminders)
			}

			explicit, err := g.ExplicitReminderRepo.ListByEventIDs(ctx, []string{event.ID}, []int64{other.ID})
			if err != nil {
				t.Fatalf("list explicit reminder markers: %v", err)
			}
			if !explicit[event.ID][other.ID] {
				t.Fatalf("expected the explicit reminder marker to have survived the rollback, got %+v", explicit)
			}

			timed, _, err := svc.GetDefaultReminders(ctx, other.ID, calendar.ID)
			if err != nil {
				t.Fatalf("get default reminders: %v", err)
			}
			if len(timed) != 1 {
				t.Fatalf("expected the default reminder to have survived the rollback, got %+v", timed)
			}

			view, err := svc.AccessWithColor(ctx, other.ID, calendar.ID)
			if err != nil {
				t.Fatalf("resolve view: %v", err)
			}
			if view.Color != "#654321FF" {
				t.Fatalf("expected the color override to have survived the rollback, got %q", view.Color)
			}
		})
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
