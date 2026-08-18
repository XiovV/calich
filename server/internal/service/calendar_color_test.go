// calendar_color_test.go covers #106/ADR-0038's per-User colour override —
// that an override affects only the User who set it, and that revoking or
// leaving a Share cleans up the leaver's override — and #194/ADR-0057's
// auto-assignment: a shared Calendar with no override gets one computed and
// persisted on first resolution, rather than falling back to the Owner's.
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calich/server/internal/repository"
)

func TestCalendarService_SetColorOverride_ResolvesForThatUserOnly(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}
	if _, err := svc.SetColorOverride(ctx, otherID, calendarID, "#654321"); err != nil {
		t.Fatalf("set color override: %v", err)
	}

	otherView, err := svc.AccessWithColor(ctx, otherID, calendarID)
	if err != nil {
		t.Fatalf("resolve other's view: %v", err)
	}
	if otherView.Color != "#654321FF" {
		t.Fatalf("expected other's resolved colour to be their override, got %q", otherView.Color)
	}

	ownerView, err := svc.AccessWithColor(ctx, ownerID, calendarID)
	if err != nil {
		t.Fatalf("resolve owner's view: %v", err)
	}
	if ownerView.Color != "#12809CFF" {
		t.Fatalf("expected the owner's resolved colour to stay the calendar's own, got %q", ownerView.Color)
	}
}

// ADR-0057 amends ADR-0038: a shared Calendar with no override no longer
// falls back to the Owner's colour — it gets one auto-assigned and
// persisted the first time it resolves for that viewer.
func TestCalendarService_ResolveDisplayColor_AutoAssignsAFreshColorWithNoOverride(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}

	otherView, err := svc.AccessWithColor(ctx, otherID, calendarID)
	if err != nil {
		t.Fatalf("resolve other's view: %v", err)
	}
	// The Owner's own calendar colour (peacock) isn't visible to otherID as
	// an already-resolved colour to avoid — it's the very calendar being
	// resolved, with no override yet — so the first Swatch (tomato) wins.
	if otherView.Color != "#E2483DFF" {
		t.Fatalf("expected other's resolved colour to be the first free Swatch, got %q", otherView.Color)
	}

	// The assignment persists — the same viewer's next resolution returns
	// the same colour rather than recomputing it.
	again, err := svc.AccessWithColor(ctx, otherID, calendarID)
	if err != nil {
		t.Fatalf("resolve other's view again: %v", err)
	}
	if again.Color != otherView.Color {
		t.Fatalf("expected the auto-assigned colour to persist, got %q then %q", otherView.Color, again.Color)
	}

	ownerView, err := svc.AccessWithColor(ctx, ownerID, calendarID)
	if err != nil {
		t.Fatalf("resolve owner's view: %v", err)
	}
	if ownerView.Color != "#12809CFF" {
		t.Fatalf("expected the owner's resolved colour to stay the calendar's own, got %q", ownerView.Color)
	}
}

func TestCalendarService_SetColorOverride_RejectsInvalidColor(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}

	_, err := svc.SetColorOverride(ctx, otherID, calendarID, "not-a-color")
	if !errors.Is(err, ErrInvalidColor) {
		t.Fatalf("err = %v, want ErrInvalidColor", err)
	}
}

func TestCalendarService_SetColorOverride_NormalizesColor(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}

	color, err := svc.SetColorOverride(ctx, otherID, calendarID, "#abc")
	if err != nil {
		t.Fatalf("set color override: %v", err)
	}
	if color != "#AABBCCFF" {
		t.Fatalf("expected the override to widen and canonicalize to #AABBCCFF, got %q", color)
	}
}

// A stranger — no Access at all — cannot set a colour override, the same
// not-found treatment every other permission check in this package gives an
// unauthorized caller.
func TestCalendarService_SetColorOverride_StrangerRefused(t *testing.T) {
	svc, _, _, otherID, calendarID := newTestShareService(t)

	_, err := svc.SetColorOverride(context.Background(), otherID, calendarID, "#654321")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// Clearing a non-Owner's override doesn't leave them tracking the Owner's
// colour under ADR-0057 — the very next resolution auto-assigns a fresh
// personal one instead.
func TestCalendarService_ClearColorOverride_ReassignsAFreshColor(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}
	if _, err := svc.SetColorOverride(ctx, otherID, calendarID, "#654321"); err != nil {
		t.Fatalf("set color override: %v", err)
	}

	if err := svc.ClearColorOverride(ctx, otherID, calendarID); err != nil {
		t.Fatalf("clear color override: %v", err)
	}

	view, err := svc.AccessWithColor(ctx, otherID, calendarID)
	if err != nil {
		t.Fatalf("resolve view: %v", err)
	}
	if view.Color != "#E2483DFF" {
		t.Fatalf("expected the colour to be freshly auto-assigned (first free Swatch) after clearing, got %q", view.Color)
	}
}

// Clearing an override nobody set is a no-op, not an error.
func TestCalendarService_ClearColorOverride_NoOverrideSet_IsNoOp(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}

	if err := svc.ClearColorOverride(ctx, otherID, calendarID); err != nil {
		t.Fatalf("clear color override: %v", err)
	}
}

// RevokeShare takes the leaver's colour override with it — an override with
// no Access behind it would otherwise linger invisibly (ADR-0038's
// acceptance criteria, mirroring ADR-0036's Reminder-override cleanup).
func TestCalendarService_RevokeShare_ClearsColorOverride(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}
	if _, err := svc.SetColorOverride(ctx, otherID, calendarID, "#654321"); err != nil {
		t.Fatalf("set color override: %v", err)
	}

	if err := svc.RevokeShare(ctx, ownerID, calendarID, otherID); err != nil {
		t.Fatalf("revoke share: %v", err)
	}

	// Re-granting a Share doesn't resurrect the old override — it must have
	// actually been deleted, not merely hidden while Access is None: a fresh
	// colour gets auto-assigned (ADR-0057) rather than the stale override
	// reappearing.
	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("re-share: %v", err)
	}
	view, err := svc.AccessWithColor(ctx, otherID, calendarID)
	if err != nil {
		t.Fatalf("resolve view: %v", err)
	}
	if view.Color != "#E2483DFF" {
		t.Fatalf("expected the override to have been cleared by revoke and freshly auto-assigned on re-share, got %q", view.Color)
	}
}

// LeaveShare takes the leaver's own colour override with it, mirroring
// RevokeShare (ADR-0038's acceptance criteria).
func TestCalendarService_LeaveShare_ClearsColorOverride(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}
	if _, err := svc.SetColorOverride(ctx, otherID, calendarID, "#654321"); err != nil {
		t.Fatalf("set color override: %v", err)
	}

	if err := svc.LeaveShare(ctx, otherID, calendarID); err != nil {
		t.Fatalf("leave share: %v", err)
	}

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("re-share: %v", err)
	}
	view, err := svc.AccessWithColor(ctx, otherID, calendarID)
	if err != nil {
		t.Fatalf("resolve view: %v", err)
	}
	if view.Color != "#E2483DFF" {
		t.Fatalf("expected the override to have been cleared by leaving and freshly auto-assigned on re-share, got %q", view.Color)
	}
}

// ListAccessible reports each accessible Calendar's colour resolved for the
// caller, not the Owner's raw stored value — the REST Calendar list's
// per-caller colour (ADR-0038).
func TestCalendarService_ListAccessible_ResolvesColorPerCaller(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, _, err := svc.Share(ctx, ownerID, calendarID, "other@example.com", repository.RoleEditor); err != nil {
		t.Fatalf("share: %v", err)
	}
	if _, err := svc.SetColorOverride(ctx, otherID, calendarID, "#654321"); err != nil {
		t.Fatalf("set color override: %v", err)
	}

	result, err := svc.ListAccessible(ctx, otherID)
	if err != nil {
		t.Fatalf("list accessible: %v", err)
	}
	byID := make(map[string]CalendarWithAccess, len(result))
	for _, c := range result {
		byID[c.ID] = c
	}
	if byID[calendarID].Color != "#654321FF" {
		t.Fatalf("expected other's resolved colour in the list to be their override, got %q", byID[calendarID].Color)
	}

	ownerResult, err := svc.ListAccessible(ctx, ownerID)
	if err != nil {
		t.Fatalf("list accessible: %v", err)
	}
	ownerByID := make(map[string]CalendarWithAccess, len(ownerResult))
	for _, c := range ownerResult {
		ownerByID[c.ID] = c
	}
	if ownerByID[calendarID].Color != "#12809CFF" {
		t.Fatalf("expected the owner's resolved colour to stay the calendar's own, got %q", ownerByID[calendarID].Color)
	}
}

// An unshared Calendar behaves exactly as it does today: its Owner's own
// resolved colour is simply the Calendar's stored colour, with no override
// machinery in play.
func TestCalendarService_UnsharedCalendar_ResolvedColorIsJustTheCalendarsColor(t *testing.T) {
	svc, userID, workspaceID := newTestCalendarService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, userID, workspaceID, "cal-1", CalendarWrite{Name: "Personal", Color: "#12809CFF"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	view, err := svc.AccessWithColor(ctx, userID, "cal-1")
	if err != nil {
		t.Fatalf("resolve view: %v", err)
	}
	if view.Color != "#12809CFF" {
		t.Fatalf("expected the resolved colour to be the calendar's own, got %q", view.Color)
	}
}
