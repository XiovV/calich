package service

import (
	"context"
	"testing"

	"github.com/XiovV/calendar/server/internal/repository"
)

// TestCalendarService_OwnershipMeta_UnclampedByAccess is #111's core
// regression: the Owner of a Subscribed Calendar has Access clamped to
// Viewer (ADR-0032), but OwnershipMeta's IsOwner must stay true — the trap
// a web app gating Calendar management on Access would fall into.
func TestCalendarService_OwnershipMeta_UnclampedByAccess(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	subscribed, err := svc.Create(ctx, ownerID, "cal-sub", CalendarWrite{Name: "Feed", Color: "#12809CFF", SourceURL: strPtr("https://example.com/feed.ics")})
	if err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}

	access, calendar, err := svc.Access(ctx, ownerID, subscribed.ID)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	if access != AccessViewer {
		t.Fatalf("owner access to subscribed calendar = %v, want AccessViewer (clamped)", access)
	}

	isOwner, ownerUsername, shareCount, err := svc.OwnershipMeta(ctx, ownerID, calendar)
	if err != nil {
		t.Fatalf("ownership meta: %v", err)
	}
	if !isOwner {
		t.Fatalf("isOwner = false, want true — un-clamped ownership must survive the Subscription clamp")
	}
	if ownerUsername != "owner" {
		t.Fatalf("ownerUsername = %q, want %q", ownerUsername, "owner")
	}
	if shareCount != 0 {
		t.Fatalf("shareCount = %d, want 0", shareCount)
	}

	// A stranger reading the same un-clamped ownership question gets false,
	// and sees the Owner's Username rather than their own.
	if _, err := svc.Share(ctx, ownerID, calendarID, "other", repository.RoleViewer); err != nil {
		t.Fatalf("share: %v", err)
	}
	_, ordinary, err := svc.Access(ctx, otherID, calendarID)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	isOwner, ownerUsername, shareCount, err = svc.OwnershipMeta(ctx, otherID, ordinary)
	if err != nil {
		t.Fatalf("ownership meta: %v", err)
	}
	if isOwner {
		t.Fatalf("isOwner = true for a Viewer, want false")
	}
	if ownerUsername != "owner" {
		t.Fatalf("ownerUsername = %q, want %q", ownerUsername, "owner")
	}
	if shareCount != 1 {
		t.Fatalf("shareCount = %d, want 1", shareCount)
	}
}

// TestCalendarService_ListAccessible_CarriesOwnershipMeta covers the List
// endpoint's data source directly: every row — owned or shared — carries
// IsOwner, OwnerUsername and ShareCount alongside Access (#111).
func TestCalendarService_ListAccessible_CarriesOwnershipMeta(t *testing.T) {
	svc, _, ownerID, otherID, calendarID := newTestShareService(t)
	ctx := context.Background()

	if _, err := svc.Share(ctx, ownerID, calendarID, "other", repository.RoleEditor); err != nil {
		t.Fatalf("share: %v", err)
	}

	ownerList, err := svc.ListAccessible(ctx, ownerID)
	if err != nil {
		t.Fatalf("list accessible (owner): %v", err)
	}
	var ownerRow CalendarWithAccess
	for _, c := range ownerList {
		if c.ID == calendarID {
			ownerRow = c
		}
	}
	if !ownerRow.IsOwner || ownerRow.OwnerUsername != "owner" || ownerRow.ShareCount != 1 {
		t.Fatalf("owner's row = %+v, want IsOwner=true, OwnerUsername=owner, ShareCount=1", ownerRow)
	}

	otherList, err := svc.ListAccessible(ctx, otherID)
	if err != nil {
		t.Fatalf("list accessible (other): %v", err)
	}
	var otherRow CalendarWithAccess
	for _, c := range otherList {
		if c.ID == calendarID {
			otherRow = c
		}
	}
	if otherRow.IsOwner || otherRow.OwnerUsername != "owner" || otherRow.ShareCount != 1 {
		t.Fatalf("editor's row = %+v, want IsOwner=false, OwnerUsername=owner, ShareCount=1", otherRow)
	}
}
