// calendar_color_test.go covers #106/ADR-0038's per-User colour override:
// DisplayColor's fallback to the Calendar's own colour, that an override
// affects only the User who set it, that the Owner's colour stays the
// default new Shares inherit, and that revoking or leaving a Share cleans
// up the leaver's override.
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calendar/server/internal/repository"
)

func TestCalendarService_SetColorOverride_ResolvesForThatUserOnly(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, err := svc.Share(ctx, ownerID, calendarID, "other", repository.RoleViewer); err != nil {
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

func TestCalendarService_ResolveDisplayColor_FallsBackToCalendarColorWithNoOverride(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, err := svc.Share(ctx, ownerID, calendarID, "other", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}

	otherView, err := svc.AccessWithColor(ctx, otherID, calendarID)
	if err != nil {
		t.Fatalf("resolve other's view: %v", err)
	}
	if otherView.Color != "#12809CFF" {
		t.Fatalf("expected other's resolved colour to fall back to the calendar's own, got %q", otherView.Color)
	}
}

func TestCalendarService_SetColorOverride_RejectsInvalidColor(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, err := svc.Share(ctx, ownerID, calendarID, "other", repository.RoleViewer); err != nil {
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

	if _, err := svc.Share(ctx, ownerID, calendarID, "other", repository.RoleViewer); err != nil {
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

func TestCalendarService_ClearColorOverride_FallsBackToCalendarColor(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, err := svc.Share(ctx, ownerID, calendarID, "other", repository.RoleViewer); err != nil {
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
	if view.Color != "#12809CFF" {
		t.Fatalf("expected the colour to fall back to the calendar's own after clearing, got %q", view.Color)
	}
}

// Clearing an override nobody set is a no-op, not an error — the same
// idempotent-delete convention ReminderOverride's Clear already follows.
func TestCalendarService_ClearColorOverride_NoOverrideSet_IsNoOp(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, err := svc.Share(ctx, ownerID, calendarID, "other", repository.RoleViewer); err != nil {
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

	if _, err := svc.Share(ctx, ownerID, calendarID, "other", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}
	if _, err := svc.SetColorOverride(ctx, otherID, calendarID, "#654321"); err != nil {
		t.Fatalf("set color override: %v", err)
	}

	if err := svc.RevokeShare(ctx, ownerID, calendarID, otherID); err != nil {
		t.Fatalf("revoke share: %v", err)
	}

	// Re-granting a Share doesn't resurrect the old override — it must
	// have actually been deleted, not merely hidden while Access is None.
	if _, err := svc.Share(ctx, ownerID, calendarID, "other", repository.RoleViewer); err != nil {
		t.Fatalf("re-share: %v", err)
	}
	view, err := svc.AccessWithColor(ctx, otherID, calendarID)
	if err != nil {
		t.Fatalf("resolve view: %v", err)
	}
	if view.Color != "#12809CFF" {
		t.Fatalf("expected the override to have been cleared by revoke, got %q", view.Color)
	}
}

// LeaveShare takes the leaver's own colour override with it, mirroring
// RevokeShare (ADR-0038's acceptance criteria).
func TestCalendarService_LeaveShare_ClearsColorOverride(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, err := svc.Share(ctx, ownerID, calendarID, "other", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}
	if _, err := svc.SetColorOverride(ctx, otherID, calendarID, "#654321"); err != nil {
		t.Fatalf("set color override: %v", err)
	}

	if err := svc.LeaveShare(ctx, otherID, calendarID); err != nil {
		t.Fatalf("leave share: %v", err)
	}

	if _, err := svc.Share(ctx, ownerID, calendarID, "other", repository.RoleViewer); err != nil {
		t.Fatalf("re-share: %v", err)
	}
	view, err := svc.AccessWithColor(ctx, otherID, calendarID)
	if err != nil {
		t.Fatalf("resolve view: %v", err)
	}
	if view.Color != "#12809CFF" {
		t.Fatalf("expected the override to have been cleared by leaving, got %q", view.Color)
	}
}

// ListAccessible reports each accessible Calendar's colour resolved for the
// caller, not the Owner's raw stored value — the REST Calendar list's
// per-caller colour (ADR-0038).
func TestCalendarService_ListAccessible_ResolvesColorPerCaller(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, err := svc.Share(ctx, ownerID, calendarID, "other", repository.RoleEditor); err != nil {
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
