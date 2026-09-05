package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calich/server/internal/db"
)

// newTestCalendarRepository returns a CalendarRepository plus two real user
// ids to satisfy calendars.user_id's foreign key — SQLite enforces it, so
// tests can't use arbitrary literal ids the way they could before FK
// enforcement was turned on.
func newTestCalendarRepository(t *testing.T) (repo *CalendarRepository, userID, otherUserID, workspaceID, otherWorkspaceID int64) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	ctx := context.Background()

	users := NewUserRepository(sqlDB)
	user, err := users.Create(ctx, "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	other, err := users.Create(ctx, "user-b", "user-b@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	workspaces := NewWorkspaceRepository(sqlDB)
	workspace, err := workspaces.Create(ctx, "workspace-a", user.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaces.AddMember(ctx, workspace.ID, user.ID, WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}
	otherWorkspace, err := workspaces.Create(ctx, "workspace-b", other.ID)
	if err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	if err := workspaces.AddMember(ctx, otherWorkspace.ID, other.ID, WorkspaceRoleOwner); err != nil {
		t.Fatalf("add other workspace member: %v", err)
	}

	return NewCalendarRepository(sqlDB), user.ID, other.ID, workspace.ID, otherWorkspace.ID
}

func TestCalendarRepository_CreateAndGetByID(t *testing.T) {
	repo, userID, _, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	if created != (Calendar{ID: "cal-1", UserID: userID, WorkspaceID: workspaceID, Name: "Personal", Color: "peacock", CreatedAt: created.CreatedAt}) {
		t.Fatalf("unexpected created calendar: %+v", created)
	}

	fetched, err := repo.GetByID(ctx, userID, "cal-1")
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if fetched != created {
		t.Fatalf("expected fetched calendar %+v to equal created calendar %+v", fetched, created)
	}
}

func TestCalendarRepository_GetByID_NotFound(t *testing.T) {
	repo, userID, _, _, _ := newTestCalendarRepository(t)

	_, err := repo.GetByID(context.Background(), userID, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_GetByID_ScopedToUser(t *testing.T) {
	repo, userID, otherUserID, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	_, err := repo.GetByID(ctx, otherUserID, "cal-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for another user's calendar, got %v", err)
	}
}

func TestCalendarRepository_ListByUser(t *testing.T) {
	repo, userID, otherUserID, workspaceID, otherWorkspaceID := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
		t.Fatalf("create calendar 1: %v", err)
	}
	if _, err := repo.Create(ctx, userID, workspaceID, "cal-2", CalendarFields{Name: "Work", Color: "tomato"}); err != nil {
		t.Fatalf("create calendar 2: %v", err)
	}
	if _, err := repo.Create(ctx, otherUserID, otherWorkspaceID, "cal-3", CalendarFields{Name: "Other user", Color: "sage"}); err != nil {
		t.Fatalf("create calendar for other user: %v", err)
	}

	calendars, err := repo.ListByUser(ctx, userID)
	if err != nil {
		t.Fatalf("list by user: %v", err)
	}

	if len(calendars) != 2 {
		t.Fatalf("expected 2 calendars, got %d", len(calendars))
	}
	if calendars[0].ID != "cal-1" || calendars[1].ID != "cal-2" {
		t.Fatalf("expected calendars in creation order, got %+v", calendars)
	}
}

func TestCalendarRepository_Update(t *testing.T) {
	repo, userID, _, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	updated, err := repo.Update(ctx, userID, "cal-1", CalendarFields{Name: "Renamed", Color: "tomato"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "Renamed" || updated.Color != "tomato" {
		t.Fatalf("expected updated fields, got %+v", updated)
	}
}

func TestCalendarRepository_Update_NotFound(t *testing.T) {
	repo, userID, _, _, _ := newTestCalendarRepository(t)

	_, err := repo.Update(context.Background(), userID, "nope", CalendarFields{Name: "Renamed", Color: "tomato"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_Update_ScopedToUser(t *testing.T) {
	repo, userID, otherUserID, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	_, err := repo.Update(ctx, otherUserID, "cal-1", CalendarFields{Name: "Renamed", Color: "tomato"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound updating another user's calendar, got %v", err)
	}
}

func TestCalendarRepository_Delete(t *testing.T) {
	repo, userID, _, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	if err := repo.Delete(ctx, userID, "cal-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := repo.GetByID(ctx, userID, "cal-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestCalendarRepository_Delete_NotFound(t *testing.T) {
	repo, userID, _, _, _ := newTestCalendarRepository(t)

	err := repo.Delete(context.Background(), userID, "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarRepository_Delete_ScopedToUser(t *testing.T) {
	repo, userID, otherUserID, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	err := repo.Delete(ctx, otherUserID, "cal-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting another user's calendar, got %v", err)
	}

	if _, err := repo.GetByID(ctx, userID, "cal-1"); err != nil {
		t.Fatalf("expected calendar to still exist, got %v", err)
	}
}

func TestCalendarRepository_TransferOwnershipOne(t *testing.T) {
	repo, userID, otherUserID, workspaceID, _ := newTestCalendarRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, userID, workspaceID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"}); err != nil {
		t.Fatalf("create cal-1: %v", err)
	}
	if _, err := repo.Create(ctx, userID, workspaceID, "cal-2", CalendarFields{Name: "Family", Color: "blue"}); err != nil {
		t.Fatalf("create cal-2: %v", err)
	}

	if err := repo.TransferOwnershipOne(ctx, userID, "cal-1", otherUserID); err != nil {
		t.Fatalf("transfer ownership: %v", err)
	}

	if _, err := repo.GetByID(ctx, userID, "cal-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cal-1 to no longer belong to the old owner, got %v", err)
	}
	transferred, err := repo.GetByID(ctx, otherUserID, "cal-1")
	if err != nil {
		t.Fatalf("expected cal-1 to belong to the new owner: %v", err)
	}
	if transferred.Name != "Personal" {
		t.Fatalf("expected the calendar's other fields to survive the transfer, got %+v", transferred)
	}
	if _, err := repo.GetByID(ctx, userID, "cal-2"); err != nil {
		t.Fatalf("expected cal-2, untouched by the single-calendar transfer, to still belong to the old owner: %v", err)
	}
}

func TestCalendarRepository_TransferOwnershipOne_UnknownCalendar_ReturnsErrNotFound(t *testing.T) {
	repo, userID, otherUserID, _, _ := newTestCalendarRepository(t)

	if err := repo.TransferOwnershipOne(context.Background(), userID, "ghost", otherUserID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound transferring a nonexistent calendar, got %v", err)
	}
}

func TestCalendarRepository_ListSharedWithUser(t *testing.T) {
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

	repo := NewCalendarRepository(sqlDB)
	owned, err := repo.Create(ctx, owner.ID, workspace.ID, "cal-owned", CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create owned calendar: %v", err)
	}
	shared, err := repo.Create(ctx, owner.ID, workspace.ID, "cal-shared", CalendarFields{Name: "Family", Color: "sage"})
	if err != nil {
		t.Fatalf("create shared calendar: %v", err)
	}

	shares := NewCalendarShareRepository(sqlDB)
	if _, err := shares.Upsert(ctx, shared.ID, other.ID, RoleEditor); err != nil {
		t.Fatalf("upsert share: %v", err)
	}

	result, err := repo.ListSharedWithUser(ctx, other.ID)
	if err != nil {
		t.Fatalf("list shared with user: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected exactly one shared calendar, got %d: %+v", len(result), result)
	}
	if result[0].ID != shared.ID || result[0].Role != RoleEditor {
		t.Fatalf("unexpected result: %+v", result[0])
	}

	// owned isn't otherUserID's own and carries no Share, so it must not
	// appear — sanity-checking the join doesn't leak every Calendar.
	for _, c := range result {
		if c.ID == owned.ID {
			t.Fatalf("owner-only calendar %q leaked into another user's shared list", owned.ID)
		}
	}
}
