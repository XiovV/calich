package service

import (
	"context"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

func newTestCalendarServiceForUser(t *testing.T) (svc *CalendarService, userID, workspaceID int64) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	user, err := users.Create(context.Background(), "admin", "hash", true)
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

	return NewCalendarService(repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo), user.ID, workspace.ID
}

func TestEnsureDefaultCalendars_SeedsPersonalWorkFamily(t *testing.T) {
	calendars, userID, workspaceID := newTestCalendarServiceForUser(t)
	ctx := context.Background()

	if err := calendars.EnsureDefaults(ctx, userID, workspaceID); err != nil {
		t.Fatalf("ensure default calendars: %v", err)
	}

	got, err := calendars.List(ctx, userID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 default calendars, got %d", len(got))
	}

	want := map[string]string{"Personal": "#12809CFF", "Work": "#E2483DFF", "Family": "#6B9071FF"}
	for _, c := range got {
		color, ok := want[c.Name]
		if !ok {
			t.Fatalf("unexpected default calendar %q", c.Name)
		}
		if c.Color != color {
			t.Fatalf("expected %q to have color %q, got %q", c.Name, color, c.Color)
		}
		delete(want, c.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing default calendars: %v", want)
	}
}

func TestEnsureDefaultCalendars_NoOpWhenUserAlreadyHasCalendars(t *testing.T) {
	calendars, userID, workspaceID := newTestCalendarServiceForUser(t)
	ctx := context.Background()

	if _, err := calendars.Create(ctx, userID, workspaceID, "existing-cal", CalendarWrite{Name: "Existing", Color: "#6B7280FF"}); err != nil {
		t.Fatalf("create existing calendar: %v", err)
	}

	if err := calendars.EnsureDefaults(ctx, userID, workspaceID); err != nil {
		t.Fatalf("ensure default calendars: %v", err)
	}

	got, err := calendars.List(ctx, userID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected the pre-existing calendar to be untouched, got %d calendars", len(got))
	}
	if got[0].Name != "Existing" {
		t.Fatalf("expected the pre-existing calendar to survive, got %+v", got[0])
	}
}

func TestEnsureDefaultCalendars_RepeatedCallsAreNoOps(t *testing.T) {
	calendars, userID, workspaceID := newTestCalendarServiceForUser(t)
	ctx := context.Background()

	if err := calendars.EnsureDefaults(ctx, userID, workspaceID); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if err := calendars.EnsureDefaults(ctx, userID, workspaceID); err != nil {
		t.Fatalf("second ensure: %v", err)
	}

	got, err := calendars.List(ctx, userID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected re-running to be a no-op, got %d calendars", len(got))
	}
}

// TestBootstrapCreatedFlag_GatesSeedingSoDeletedCalendarsStayDeleted mirrors
// main.go's real orchestration — only call EnsureDefaultCalendars when
// Bootstrap reports created=true — to prove that deleting every calendar and
// then restarting the process doesn't resurrect the defaults. Gating on
// "does the user currently have zero calendars" instead of this flag would
// re-seed here, which is exactly the behavior the ticket rules out.
func TestBootstrapCreatedFlag_GatesSeedingSoDeletedCalendarsStayDeleted(t *testing.T) {
	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	workspaces := NewWorkspaceService(sqlDB, repository.NewWorkspaceRepository(sqlDB), repository.NewWorkspaceInviteRepository(sqlDB))
	authSvc := NewAuthService(users, repository.NewSessionRepository(sqlDB), workspaces, repository.NewWorkspaceInviteRepository(sqlDB), []byte("test-secret"), "admin", "admin", false)
	calendarSvc := NewCalendarService(repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), repository.NewWorkspaceRepository(sqlDB))
	ctx := context.Background()

	user, created, err := authSvc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	if !created {
		t.Fatalf("expected the first bootstrap to report created=true")
	}

	userWorkspaces, err := workspaces.ListForUser(ctx, user.ID)
	if err != nil || len(userWorkspaces) == 0 {
		t.Fatalf("list user workspaces: %v", err)
	}
	workspaceID := userWorkspaces[0].ID

	if created {
		if err := calendarSvc.EnsureDefaults(ctx, user.ID, workspaceID); err != nil {
			t.Fatalf("ensure default calendars: %v", err)
		}
	}

	seeded, err := calendarSvc.List(ctx, user.ID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}
	for _, c := range seeded {
		if err := calendarSvc.Delete(ctx, user.ID, c.ID); err != nil {
			t.Fatalf("delete calendar %q: %v", c.Name, err)
		}
	}

	// Simulate a server restart: Bootstrap runs again against the now-existing user.
	_, created, err = authSvc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if created {
		t.Fatalf("expected the second bootstrap to report created=false")
	}
	if created {
		if err := calendarSvc.EnsureDefaults(ctx, user.ID, workspaceID); err != nil {
			t.Fatalf("ensure default calendars: %v", err)
		}
	}

	remaining, err := calendarSvc.List(ctx, user.ID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected deleted calendars to stay deleted across a restart, got %d calendars", len(remaining))
	}
}
