package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
)

// newTestCalendarShareRepository returns a CalendarShareRepository plus a
// real owner id, a real other-user id, and a real calendar id belonging to
// owner — satisfying calendar_shares' foreign keys, which SQLite enforces.
func newTestCalendarShareRepository(t *testing.T) (repo *CalendarShareRepository, ownerID, otherID int64, calendarID string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := NewUserRepository(sqlDB)
	owner, err := users.Create(context.Background(), "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := users.Create(context.Background(), "other", "other@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	workspaces := NewWorkspaceRepository(sqlDB)
	workspace, err := workspaces.Create(context.Background(), "workspace-a", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaces.AddMember(context.Background(), workspace.ID, owner.ID, WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}

	calendars := NewCalendarRepository(sqlDB)
	calendar, err := calendars.Create(context.Background(), owner.ID, workspace.ID, "cal-1", CalendarFields{Name: "Family", Color: "sage"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	return NewCalendarShareRepository(sqlDB), owner.ID, other.ID, calendar.ID
}

func TestCalendarShareRepository_UpsertAndGet(t *testing.T) {
	repo, _, otherID, calendarID := newTestCalendarShareRepository(t)
	ctx := context.Background()

	share, err := repo.Upsert(ctx, calendarID, otherID, RoleEditor)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if share.CalendarID != calendarID || share.UserID != otherID || share.Role != RoleEditor {
		t.Fatalf("unexpected share: %+v", share)
	}

	fetched, err := repo.Get(ctx, calendarID, otherID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched != share {
		t.Fatalf("fetched %+v != upserted %+v", fetched, share)
	}
}

// TestCalendarShareRepository_UpsertChangesRole covers an Owner changing an
// existing Share's Role (#100's acceptance criteria) — Upsert must replace
// the row's role rather than erroring on the duplicate (calendar_id,
// user_id) key.
func TestCalendarShareRepository_UpsertChangesRole(t *testing.T) {
	repo, _, otherID, calendarID := newTestCalendarShareRepository(t)
	ctx := context.Background()

	if _, err := repo.Upsert(ctx, calendarID, otherID, RoleViewer); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}

	updated, err := repo.Upsert(ctx, calendarID, otherID, RoleEditor)
	if err != nil {
		t.Fatalf("role-changing upsert: %v", err)
	}
	if updated.Role != RoleEditor {
		t.Fatalf("role = %q, want %q", updated.Role, RoleEditor)
	}

	shares, err := repo.ListByCalendarWithUser(ctx, calendarID)
	if err != nil {
		t.Fatalf("list by calendar: %v", err)
	}
	if len(shares) != 1 {
		t.Fatalf("expected exactly one share after a role change, got %d", len(shares))
	}
}

func TestCalendarShareRepository_Get_NotFound(t *testing.T) {
	repo, _, otherID, calendarID := newTestCalendarShareRepository(t)
	ctx := context.Background()

	_, err := repo.Get(ctx, calendarID, otherID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCalendarShareRepository_Delete(t *testing.T) {
	repo, _, otherID, calendarID := newTestCalendarShareRepository(t)
	ctx := context.Background()

	if _, err := repo.Upsert(ctx, calendarID, otherID, RoleViewer); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := repo.Delete(ctx, calendarID, otherID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := repo.Get(ctx, calendarID, otherID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err after delete = %v, want ErrNotFound", err)
	}
}

func TestCalendarShareRepository_Delete_NotFound(t *testing.T) {
	repo, _, otherID, calendarID := newTestCalendarShareRepository(t)
	ctx := context.Background()

	err := repo.Delete(ctx, calendarID, otherID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCalendarShareRepository_ListByCalendarWithUser(t *testing.T) {
	repo, _, otherID, calendarID := newTestCalendarShareRepository(t)
	ctx := context.Background()

	if _, err := repo.Upsert(ctx, calendarID, otherID, RoleEditor); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	shares, err := repo.ListByCalendarWithUser(ctx, calendarID)
	if err != nil {
		t.Fatalf("list by calendar with username: %v", err)
	}
	if len(shares) != 1 {
		t.Fatalf("expected one share, got %d", len(shares))
	}
	if shares[0].Name != "other" || shares[0].Role != RoleEditor {
		t.Fatalf("unexpected share: %+v", shares[0])
	}
}

func TestCalendarShareRepository_CountByCalendar(t *testing.T) {
	repo, _, otherID, calendarID := newTestCalendarShareRepository(t)
	ctx := context.Background()

	count, err := repo.CountByCalendar(ctx, calendarID)
	if err != nil {
		t.Fatalf("count by calendar: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0 before any share", count)
	}

	if _, err := repo.Upsert(ctx, calendarID, otherID, RoleEditor); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	count, err = repo.CountByCalendar(ctx, calendarID)
	if err != nil {
		t.Fatalf("count by calendar: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1 after one share", count)
	}
}

func TestCalendarShareRepository_CascadesOnCalendarDelete(t *testing.T) {
	ctx := context.Background()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := NewUserRepository(sqlDB)
	owner, err := users.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := users.Create(ctx, "other", "other@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	workspaces := NewWorkspaceRepository(sqlDB)
	workspace, err := workspaces.Create(ctx, "workspace-a", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaces.AddMember(ctx, workspace.ID, owner.ID, WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}

	calendars := NewCalendarRepository(sqlDB)
	calendar, err := calendars.Create(ctx, owner.ID, workspace.ID, "cal-1", CalendarFields{Name: "Family", Color: "sage"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	shares := NewCalendarShareRepository(sqlDB)
	if _, err := shares.Upsert(ctx, calendar.ID, other.ID, RoleViewer); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := calendars.Delete(ctx, owner.ID, calendar.ID); err != nil {
		t.Fatalf("delete calendar: %v", err)
	}

	if _, err := shares.Get(ctx, calendar.ID, other.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err after calendar delete = %v, want ErrNotFound (ON DELETE CASCADE)", err)
	}
}
