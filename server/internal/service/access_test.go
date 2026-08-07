package service

import (
	"context"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// TestResolveAccess is a table over ownership x Subscription (ADR-0034):
// Resolve is tested directly here, not only through the operations that
// call it, per #98's acceptance criteria.
func TestResolveAccess(t *testing.T) {
	const owner, stranger, shared int64 = 1, 2, 3

	cases := []struct {
		name       string
		userID     int64
		sourceURL  *string
		shareRole  *string
		wantAccess Access
	}{
		{
			name:       "owner of an ordinary calendar gets Owner",
			userID:     owner,
			sourceURL:  nil,
			wantAccess: AccessOwner,
		},
		{
			name:       "stranger to an ordinary calendar gets None",
			userID:     stranger,
			sourceURL:  nil,
			wantAccess: AccessNone,
		},
		{
			name:       "owner of a subscribed calendar clamps to Viewer",
			userID:     owner,
			sourceURL:  strPtr("https://example.com/feed.ics"),
			wantAccess: AccessViewer,
		},
		{
			name:       "stranger to a subscribed calendar still gets None",
			userID:     stranger,
			sourceURL:  strPtr("https://example.com/feed.ics"),
			wantAccess: AccessNone,
		},
		{
			name:       "editor share resolves to Editor",
			userID:     shared,
			sourceURL:  nil,
			shareRole:  strPtr(repository.RoleEditor),
			wantAccess: AccessEditor,
		},
		{
			name:       "viewer share resolves to Viewer",
			userID:     shared,
			sourceURL:  nil,
			shareRole:  strPtr(repository.RoleViewer),
			wantAccess: AccessViewer,
		},
		{
			name:       "editor share on a subscribed calendar clamps to Viewer",
			userID:     shared,
			sourceURL:  strPtr("https://example.com/feed.ics"),
			shareRole:  strPtr(repository.RoleEditor),
			wantAccess: AccessViewer,
		},
		{
			name:       "the owner's own share row, if any, never overrides ownership",
			userID:     owner,
			sourceURL:  nil,
			shareRole:  strPtr(repository.RoleViewer),
			wantAccess: AccessOwner,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calendar := repository.Calendar{UserID: owner, SourceURL: tc.sourceURL}
			got := ResolveAccess(tc.userID, calendar, tc.shareRole)
			if got != tc.wantAccess {
				t.Fatalf("ResolveAccess(%d, calendar, %v) = %v, want %v", tc.userID, tc.shareRole, got, tc.wantAccess)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

// TestAccess_CanRead_CanWrite_IsOwner asserts each Access value's three
// derived predicates directly, since every permission check calls one of
// these rather than comparing Access values itself.
func TestAccess_CanRead_CanWrite_IsOwner(t *testing.T) {
	cases := []struct {
		access                     Access
		canRead, canWrite, isOwner bool
	}{
		{AccessNone, false, false, false},
		{AccessViewer, true, false, false},
		{AccessEditor, true, true, false},
		{AccessOwner, true, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.access.String(), func(t *testing.T) {
			if got := tc.access.CanRead(); got != tc.canRead {
				t.Errorf("%v.CanRead() = %v, want %v", tc.access, got, tc.canRead)
			}
			if got := tc.access.CanWrite(); got != tc.canWrite {
				t.Errorf("%v.CanWrite() = %v, want %v", tc.access, got, tc.canWrite)
			}
			if got := tc.access.IsOwner(); got != tc.isOwner {
				t.Errorf("%v.IsOwner() = %v, want %v", tc.access, got, tc.isOwner)
			}
		})
	}
}

// TestCalendarService_Access resolves Access through the real service +
// repository seam (rather than constructing repository.Calendar by hand),
// covering owner, stranger, and the Subscription clamp end-to-end.
func TestCalendarService_Access(t *testing.T) {
	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	owner, err := users.Create(context.Background(), "owner", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	stranger, err := users.Create(context.Background(), "stranger", "hash", false)
	if err != nil {
		t.Fatalf("create stranger: %v", err)
	}

	svc := NewCalendarService(repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB))
	ctx := context.Background()
	calendar, err := svc.Create(ctx, owner.ID, "cal-1", CalendarWrite{Name: "Personal", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	if access, got, err := svc.Access(ctx, owner.ID, calendar.ID); err != nil || access != AccessOwner {
		t.Fatalf("owner Access = %v, %+v, %v; want AccessOwner, nil err", access, got, err)
	}
	if access, _, err := svc.Access(ctx, stranger.ID, calendar.ID); err != nil || access != AccessNone {
		t.Fatalf("stranger Access = %v, %v; want AccessNone, nil err", access, err)
	}

	subscribed, err := svc.Create(ctx, owner.ID, "cal-2", CalendarWrite{Name: "Feed", Color: "#12809CFF", SourceURL: strPtr("https://example.com/feed.ics")})
	if err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}
	if access, _, err := svc.Access(ctx, owner.ID, subscribed.ID); err != nil || access != AccessViewer {
		t.Fatalf("owner Access to subscribed calendar = %v, %v; want AccessViewer (clamped), nil err", access, err)
	}

	if _, err := svc.Share(ctx, owner.ID, calendar.ID, "stranger", repository.RoleEditor); err != nil {
		t.Fatalf("share: %v", err)
	}
	if access, _, err := svc.Access(ctx, stranger.ID, calendar.ID); err != nil || access != AccessEditor {
		t.Fatalf("editor Access = %v, %v; want AccessEditor, nil err", access, err)
	}

	if _, err := svc.Share(ctx, owner.ID, subscribed.ID, "stranger", repository.RoleEditor); err != nil {
		t.Fatalf("share subscribed calendar: %v", err)
	}
	if access, _, err := svc.Access(ctx, stranger.ID, subscribed.ID); err != nil || access != AccessViewer {
		t.Fatalf("editor Access to subscribed calendar = %v, %v; want AccessViewer (clamped), nil err", access, err)
	}
}
