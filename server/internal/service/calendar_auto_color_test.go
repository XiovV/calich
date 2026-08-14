// calendar_auto_color_test.go covers #194/ADR-0057's integration surface:
// resolveDisplayColor auto-assigning and persisting a free colour for a
// shared Calendar, sourced via a direct Share and via a Group Share
// (including a member who joins the Group only after the Share exists),
// avoiding collision with every Calendar the viewer can already see, falling
// back once the Workspace's 8 Swatches are exhausted, and assigning a batch
// of newly-visible Calendars in Calendar-id order.
package service

import (
	"context"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// TestCalendarService_ResolveDisplayColor_AvoidsCollisionWithVisibleCalendars
// covers the picker comparing against every Calendar the viewer can already
// see, not just the Swatch list in isolation: otherID already owns a
// Calendar coloured with the first Swatch, so a newly shared Calendar must
// skip it.
func TestCalendarService_ResolveDisplayColor_AvoidsCollisionWithVisibleCalendars(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	otherWorkspaces, err := svc.workspaces.ListForUser(ctx, otherID)
	if err != nil || len(otherWorkspaces) == 0 {
		t.Fatalf("list other's workspaces: %v", err)
	}
	if _, err := svc.Create(ctx, otherID, otherWorkspaces[0].ID, "cal-other-own", CalendarWrite{Name: "Mine", Color: calendarSwatches[0]}); err != nil {
		t.Fatalf("create other's own calendar: %v", err)
	}

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}

	view, err := svc.AccessWithColor(ctx, otherID, calendarID)
	if err != nil {
		t.Fatalf("resolve other's view: %v", err)
	}
	if view.Color == calendarSwatches[0] {
		t.Fatalf("expected the auto-assigned colour to skip the first Swatch, already used by other's own calendar, got %q", view.Color)
	}
	if view.Color != calendarSwatches[1] {
		t.Fatalf("expected the second Swatch to be assigned, got %q", view.Color)
	}
}

// TestCalendarService_ResolveDisplayColor_FallsBackOnceEverySwatchIsTaken
// covers the palette-exhaustion path: once every one of the 8 Swatches is
// already in use across otherID's visible Calendars in the Workspace, a
// newly shared Calendar gets a random hex outside the Swatch set rather than
// reusing one.
func TestCalendarService_ResolveDisplayColor_FallsBackOnceEverySwatchIsTaken(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	otherWorkspaces, err := svc.workspaces.ListForUser(ctx, otherID)
	if err != nil || len(otherWorkspaces) == 0 {
		t.Fatalf("list other's workspaces: %v", err)
	}
	for i, swatch := range calendarSwatches {
		id := "cal-other-" + string(rune('a'+i))
		if _, err := svc.Create(ctx, otherID, otherWorkspaces[0].ID, id, CalendarWrite{Name: "Mine", Color: swatch}); err != nil {
			t.Fatalf("create other's calendar %d: %v", i, err)
		}
	}

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}

	view, err := svc.AccessWithColor(ctx, otherID, calendarID)
	if err != nil {
		t.Fatalf("resolve other's view: %v", err)
	}
	for _, swatch := range calendarSwatches {
		if view.Color == swatch {
			t.Fatalf("expected a random fallback once every Swatch is taken, got a Swatch %q", view.Color)
		}
	}
	assertCanonicalHex(t, view.Color)
	assertWithinSwatchSaturationAndLightness(t, view.Color)
}

// TestCalendarService_ListAccessible_BatchAssignsInCalendarIDOrder covers
// the stable tiebreak ADR-0057 requires when several Calendars the viewer
// can see all lack an override at once: they're assigned in Calendar-id
// order, not the order Share was called in or the order the query happens
// to return. "cal-zzz" is shared first but sorts after "cal-aaa" by id, so
// if id order weren't honoured this test would assign the Swatches the
// other way around.
func TestCalendarService_ListAccessible_BatchAssignsInCalendarIDOrder(t *testing.T) {
	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	ctx := context.Background()

	users := repository.NewUserRepository(sqlDB)
	owner, err := users.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := users.Create(ctx, "other", "other@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspace, err := workspaceRepo.Create(ctx, "Test Workspace", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, owner.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add owner as workspace member: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, other.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add other as workspace member: %v", err)
	}
	svc := NewCalendarService(repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB), users, repository.NewEventReminderRepository(sqlDB), repository.NewCalendarDefaultReminderRepository(sqlDB), repository.NewEventReminderExplicitRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))

	last, err := svc.Create(ctx, owner.ID, workspace.ID, "cal-zzz", CalendarWrite{Name: "Last alphabetically", Color: "#123456FF"})
	if err != nil {
		t.Fatalf("create cal-zzz: %v", err)
	}
	first, err := svc.Create(ctx, owner.ID, workspace.ID, "cal-aaa", CalendarWrite{Name: "First alphabetically", Color: "#654321FF"})
	if err != nil {
		t.Fatalf("create cal-aaa: %v", err)
	}
	// created_at ties (or even favours cal-zzz) in a fast test, which would
	// let ORDER BY created_at, id in the repository's own query mask a
	// missing Calendar-id sort. Force cal-zzz's created_at earlier than
	// cal-aaa's by a wide margin, so only an explicit id-order sort in
	// resolveDisplayColor's caller — not creation time — can make cal-aaa
	// resolve first.
	if _, err := sqlDB.ExecContext(ctx, `UPDATE calendars SET created_at = datetime('now', '-1 hour') WHERE id = ?`, last.ID); err != nil {
		t.Fatalf("backdate cal-zzz: %v", err)
	}

	// Shared in reverse-id order deliberately, so a bug that assigned in
	// Share-call order rather than Calendar-id order would be caught.
	if _, _, err := svc.Share(ctx, owner.ID, last.ID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share cal-zzz: %v", err)
	}
	if _, _, err := svc.Share(ctx, owner.ID, first.ID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share cal-aaa: %v", err)
	}

	result, err := svc.ListAccessible(ctx, other.ID)
	if err != nil {
		t.Fatalf("list accessible: %v", err)
	}
	byID := make(map[string]string, len(result))
	for _, c := range result {
		byID[c.ID] = c.Color
	}

	if byID[first.ID] != calendarSwatches[0] {
		t.Fatalf("cal-aaa (lower id, created later) color = %q, want the first Swatch %q", byID[first.ID], calendarSwatches[0])
	}
	if byID[last.ID] != calendarSwatches[1] {
		t.Fatalf("cal-zzz (higher id, created earlier) color = %q, want the second Swatch %q", byID[last.ID], calendarSwatches[1])
	}
}

// TestCalendarService_ResolveDisplayColor_PersistsAcrossCalls covers "the
// same colour is returned on subsequent list/get calls rather than being
// recomputed each time" — a second pickFreeColor call after the viewer
// gains another visible Calendar must not reassign the first Calendar's
// colour just because the used set changed.
func TestCalendarService_ResolveDisplayColor_PersistsAcrossCalls(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}
	first, err := svc.AccessWithColor(ctx, otherID, calendarID)
	if err != nil {
		t.Fatalf("resolve first view: %v", err)
	}

	// A second Calendar becomes visible to otherID after the first
	// resolution — must not perturb the already-assigned colour.
	ownerWorkspaces, err := svc.workspaces.ListForUser(ctx, ownerID)
	if err != nil || len(ownerWorkspaces) == 0 {
		t.Fatalf("list owner's workspaces: %v", err)
	}
	second, err := svc.Create(ctx, ownerID, ownerWorkspaces[0].ID, "cal-second", CalendarWrite{Name: "Second", Color: "#111111FF"})
	if err != nil {
		t.Fatalf("create second calendar: %v", err)
	}
	if _, _, err := svc.Share(ctx, ownerID, second.ID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share second calendar: %v", err)
	}

	again, err := svc.AccessWithColor(ctx, otherID, calendarID)
	if err != nil {
		t.Fatalf("resolve first view again: %v", err)
	}
	if again.Color != first.Color {
		t.Fatalf("expected the first calendar's colour to stay %q, got %q", first.Color, again.Color)
	}
}

// TestCalendarService_GroupShare_CurrentMemberGetsAutoAssignedColor covers
// auto-assignment reached via a Group Share for a User already in the
// Group when the Share is created.
func TestCalendarService_GroupShare_CurrentMemberGetsAutoAssignedColor(t *testing.T) {
	f := newGroupShareFixture(t)
	ctx := context.Background()

	if err := f.groups.AddMember(ctx, f.groupID, f.memberID); err != nil {
		t.Fatalf("add member to group: %v", err)
	}
	if _, err := f.calendars.ShareWithGroup(ctx, f.ownerID, f.calendarID, f.groupID, repository.RoleViewer); err != nil {
		t.Fatalf("share with group: %v", err)
	}

	view, err := f.calendars.AccessWithColor(ctx, f.memberID, f.calendarID)
	if err != nil {
		t.Fatalf("resolve member's view: %v", err)
	}
	if view.Color != calendarSwatches[0] {
		t.Fatalf("expected the member's resolved colour to be the first free Swatch, got %q", view.Color)
	}

	again, err := f.calendars.AccessWithColor(ctx, f.memberID, f.calendarID)
	if err != nil {
		t.Fatalf("resolve member's view again: %v", err)
	}
	if again.Color != view.Color {
		t.Fatalf("expected the auto-assigned colour to persist, got %q then %q", view.Color, again.Color)
	}
}

// TestCalendarService_GroupShare_MemberJoiningLaterGetsAutoAssignedColor
// covers ADR-0057's core claim for a Group Share: a User who joins the
// Group only after the Share was created still gets an auto-assigned
// colour the first time they view the Calendar — no separate write was
// needed at ShareWithGroup time, and no backfill runs when they join.
func TestCalendarService_GroupShare_MemberJoiningLaterGetsAutoAssignedColor(t *testing.T) {
	f := newGroupShareFixture(t)
	ctx := context.Background()

	if _, err := f.calendars.ShareWithGroup(ctx, f.ownerID, f.calendarID, f.groupID, repository.RoleViewer); err != nil {
		t.Fatalf("share with group: %v", err)
	}

	// memberID isn't in the Group yet — resolving now would refuse for lack
	// of Access, not assign a colour. Joining afterwards is what ADR-0057
	// promises still works with no separate write.
	if err := f.groups.AddMember(ctx, f.groupID, f.memberID); err != nil {
		t.Fatalf("add member to group after share: %v", err)
	}

	view, err := f.calendars.AccessWithColor(ctx, f.memberID, f.calendarID)
	if err != nil {
		t.Fatalf("resolve member's view: %v", err)
	}
	if view.Color != calendarSwatches[0] {
		t.Fatalf("expected the member's resolved colour to be the first free Swatch, got %q", view.Color)
	}
}

// TestCalendarService_GroupShare_OwnerColorUnaffected covers "Owners are
// unaffected" for the Group Share path specifically: the Owner's own
// Calendar colour is untouched by a Group Share's auto-assignment for other
// members.
func TestCalendarService_GroupShare_OwnerColorUnaffected(t *testing.T) {
	f := newGroupShareFixture(t)
	ctx := context.Background()

	if err := f.groups.AddMember(ctx, f.groupID, f.memberID); err != nil {
		t.Fatalf("add member to group: %v", err)
	}
	if _, err := f.calendars.ShareWithGroup(ctx, f.ownerID, f.calendarID, f.groupID, repository.RoleViewer); err != nil {
		t.Fatalf("share with group: %v", err)
	}
	if _, err := f.calendars.AccessWithColor(ctx, f.memberID, f.calendarID); err != nil {
		t.Fatalf("resolve member's view: %v", err)
	}

	ownerView, err := f.calendars.AccessWithColor(ctx, f.ownerID, f.calendarID)
	if err != nil {
		t.Fatalf("resolve owner's view: %v", err)
	}
	if ownerView.Color != "#12809CFF" {
		t.Fatalf("expected the owner's resolved colour to stay the calendar's own, got %q", ownerView.Color)
	}
}
