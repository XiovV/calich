package service

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// newTestCalendarService returns a CalendarService plus a real user id to
// satisfy calendars.user_id's foreign key (SQLite enforces it), plus a
// Workspace that user owns and belongs to (#155, ADR-0045) — required for
// Create to pass its Workspace-membership guard.
func newTestCalendarService(t *testing.T) (svc *CalendarService, userID, workspaceID int64) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	user, err := users.Create(context.Background(), "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspace, err := workspaceRepo.Create(context.Background(), "Test Workspace", user.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaceRepo.AddMember(context.Background(), workspace.ID, user.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}

	return NewCalendarService(repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB)), user.ID, workspace.ID
}

func TestCalendarService_Create(t *testing.T) {
	svc, userID, workspaceID := newTestCalendarService(t)
	ctx := context.Background()

	calendar, err := svc.Create(ctx, userID, workspaceID, "cal-1", CalendarWrite{Name: "Personal", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if calendar.Name != "Personal" || calendar.Color != "#12809CFF" {
		t.Fatalf("unexpected calendar: %+v", calendar)
	}
}

func TestCalendarService_Create_SetsWorkspaceID(t *testing.T) {
	svc, userID, workspaceID := newTestCalendarService(t)
	ctx := context.Background()

	calendar, err := svc.Create(ctx, userID, workspaceID, "cal-1", CalendarWrite{Name: "Personal", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if calendar.WorkspaceID != workspaceID {
		t.Fatalf("expected calendar.WorkspaceID %d, got %d", workspaceID, calendar.WorkspaceID)
	}
}

func TestCalendarService_Create_RejectsNonMemberWorkspace(t *testing.T) {
	svc, userID, _ := newTestCalendarService(t)
	ctx := context.Background()

	otherUser, err := svc.users.Create(ctx, "user-b", "user-b@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	// A second, unrelated Workspace owned by a different user, with no
	// membership row added for userID — Create must refuse it (#155,
	// ADR-0045).
	otherWorkspace, err := svc.workspaces.Create(ctx, "Other Workspace", otherUser.ID)
	if err != nil {
		t.Fatalf("create other workspace: %v", err)
	}

	_, err = svc.Create(ctx, userID, otherWorkspace.ID, "cal-1", CalendarWrite{Name: "Personal", Color: "#12809CFF"})
	if !errors.Is(err, ErrNotWorkspaceMember) {
		t.Fatalf("expected ErrNotWorkspaceMember, got %v", err)
	}
}

func TestCalendarService_Create_NormalizesColor(t *testing.T) {
	svc, userID, workspaceID := newTestCalendarService(t)
	ctx := context.Background()

	calendar, err := svc.Create(ctx, userID, workspaceID, "cal-1", CalendarWrite{Name: "Personal", Color: "#12809c"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if calendar.Color != "#12809CFF" {
		t.Fatalf("expected the color to widen and canonicalize to #12809CFF, got %q", calendar.Color)
	}
}

func TestCalendarService_Create_RejectsInvalidColor(t *testing.T) {
	svc, userID, workspaceID := newTestCalendarService(t)

	_, err := svc.Create(context.Background(), userID, workspaceID, "cal-1", CalendarWrite{Name: "Personal", Color: "not-a-real-color"})
	if !errors.Is(err, ErrInvalidColor) {
		t.Fatalf("expected ErrInvalidColor, got %v", err)
	}
}

func TestCalendarService_Create_RejectsEmptyName(t *testing.T) {
	svc, userID, workspaceID := newTestCalendarService(t)

	_, err := svc.Create(context.Background(), userID, workspaceID, "cal-1", CalendarWrite{Name: "  ", Color: "#12809CFF"})
	if !errors.Is(err, ErrInvalidName) {
		t.Fatalf("expected ErrInvalidName, got %v", err)
	}
}

func TestCalendarService_List(t *testing.T) {
	svc, userID, workspaceID := newTestCalendarService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, userID, workspaceID, "cal-1", CalendarWrite{Name: "Personal", Color: "#12809CFF"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	calendars, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(calendars) != 1 {
		t.Fatalf("expected 1 calendar, got %d", len(calendars))
	}
}

func TestCalendarService_Update(t *testing.T) {
	svc, userID, workspaceID := newTestCalendarService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, userID, workspaceID, "cal-1", CalendarWrite{Name: "Personal", Color: "#12809CFF"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := svc.Update(ctx, userID, "cal-1", CalendarWrite{Name: "Renamed", Color: "#E2483DFF"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "Renamed" || updated.Color != "#E2483DFF" {
		t.Fatalf("unexpected calendar: %+v", updated)
	}
}

func TestCalendarService_Update_RejectsInvalidColor(t *testing.T) {
	svc, userID, workspaceID := newTestCalendarService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, userID, workspaceID, "cal-1", CalendarWrite{Name: "Personal", Color: "#12809CFF"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := svc.Update(ctx, userID, "cal-1", CalendarWrite{Name: "Personal", Color: "not-a-real-color"})
	if !errors.Is(err, ErrInvalidColor) {
		t.Fatalf("expected ErrInvalidColor, got %v", err)
	}
}

func TestCalendarService_Update_NotFound(t *testing.T) {
	svc, userID, _ := newTestCalendarService(t)

	_, err := svc.Update(context.Background(), userID, "nope", CalendarWrite{Name: "Renamed", Color: "#E2483DFF"})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCalendarService_Delete(t *testing.T) {
	svc, userID, workspaceID := newTestCalendarService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, userID, workspaceID, "cal-1", CalendarWrite{Name: "Personal", Color: "#12809CFF"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.Delete(ctx, userID, "cal-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	calendars, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(calendars) != 0 {
		t.Fatalf("expected 0 calendars after delete, got %d", len(calendars))
	}
}

func TestCalendarService_Delete_NotFound(t *testing.T) {
	svc, userID, _ := newTestCalendarService(t)

	err := svc.Delete(context.Background(), userID, "nope")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
