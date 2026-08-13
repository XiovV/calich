package service

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// accountTestHarness bundles everything a self-service AccountService test
// needs: AuthService to register real Users (each minting their own
// Workspace and default Calendars, exactly like production), CalendarService
// to create/share extra Calendars, and WorkspaceService to inspect/adjust
// Membership directly where a test needs to set up a scenario production
// code wouldn't call AccountService to reach (e.g. adding a second Member to
// a Workspace, which is WorkspaceService.CreateInvite/AuthService.Accept*'s
// job, not AccountService's).
type accountTestHarness struct {
	db         *sql.DB
	accounts   *AccountService
	auth       *AuthService
	calendars  *CalendarService
	workspaces *WorkspaceService
	users      *repository.UserRepository
}

func newAccountTestHarness(t *testing.T) *accountTestHarness {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	calendarRepo := repository.NewCalendarRepository(sqlDB)
	shareRepo := repository.NewCalendarShareRepository(sqlDB)
	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspaceInviteRepo := repository.NewWorkspaceInviteRepository(sqlDB)
	workspaces := NewWorkspaceService(sqlDB, workspaceRepo, workspaceInviteRepo, repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB))
	calendars := NewCalendarService(calendarRepo, shareRepo, users, repository.NewEventReminderRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	auth := NewAuthService(users, sessions, workspaces, workspaceInviteRepo, calendars, repository.NewAttendeeRepository(sqlDB), []byte("test-secret"), "", "", "", true)
	accounts := NewAccountService(sqlDB, users, sessions, calendarRepo, shareRepo, workspaceRepo, workspaces)

	return &accountTestHarness{db: sqlDB, accounts: accounts, auth: auth, calendars: calendars, workspaces: workspaces, users: users}
}

// register self-registers username (ENABLE_SIGNUPS is always true in the
// harness), returning the resulting User and their sole, freshly created
// Workspace.
func (h *accountTestHarness) register(t *testing.T, username string) (repository.User, repository.Workspace) {
	t.Helper()
	ctx := context.Background()

	if _, err := h.auth.Register(ctx, username, username+"@example.com", "hunter2"); err != nil {
		t.Fatalf("register %s: %v", username, err)
	}

	user, err := h.users.GetByEmail(ctx, username+"@example.com")
	if err != nil {
		t.Fatalf("get %s: %v", username, err)
	}
	workspaces, err := h.workspaces.ListForUser(ctx, user.ID)
	if err != nil || len(workspaces) != 1 {
		t.Fatalf("list workspaces for %s: %v (%d workspaces)", username, err, len(workspaces))
	}
	return user, workspaces[0]
}

// createCalendar creates a Calendar for userID inside workspaceID, on top of
// the three default Calendars register already seeded (#172) — a test that
// wants a clean slate with exactly the Calendars it names should call
// deleteAllCalendars first.
func (h *accountTestHarness) createCalendar(t *testing.T, userID, workspaceID int64, id, name string) repository.Calendar {
	t.Helper()
	calendar, err := h.calendars.Create(context.Background(), userID, workspaceID, id, CalendarWrite{Name: name, Color: "#112233FF"})
	if err != nil {
		t.Fatalf("create calendar %s: %v", id, err)
	}
	return calendar
}

// deleteAllCalendars removes every Calendar userID currently owns — used to
// clear register's seeded Personal/Work/Family defaults (#172) before a test
// sets up its own exact Calendar list, exercising the same "delete every
// default" path #172's acceptance criteria requires stay at zero.
func (h *accountTestHarness) deleteAllCalendars(t *testing.T, userID int64) {
	t.Helper()
	existing, err := h.calendars.List(context.Background(), userID)
	if err != nil {
		t.Fatalf("list calendars for %d: %v", userID, err)
	}
	for _, c := range existing {
		if err := h.calendars.Delete(context.Background(), userID, c.ID); err != nil {
			t.Fatalf("delete calendar %s: %v", c.ID, err)
		}
	}
}

// addMember inserts a WorkspaceMember row directly — standing in for
// accepting a Workspace Invite (WorkspaceService/AuthService's job, not
// AccountService's), so a test can put a second User in workspaceID without
// exercising the whole invite flow.
func (h *accountTestHarness) addMember(t *testing.T, workspaceID, userID int64, role string) {
	t.Helper()
	if err := h.workspaces.workspaces.AddMember(context.Background(), workspaceID, userID, role); err != nil {
		t.Fatalf("add member: %v", err)
	}
}

func TestAccountService_SetDisabled_DisablesAndReactivates(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, _ := h.register(t, "alice")

	disabled, err := h.accounts.SetDisabled(ctx, alice.ID, true)
	if err != nil {
		t.Fatalf("disable alice: %v", err)
	}
	if !disabled.IsDisabled {
		t.Fatalf("expected alice to be disabled")
	}

	reactivated, err := h.accounts.SetDisabled(ctx, alice.ID, false)
	if err != nil {
		t.Fatalf("reactivate alice: %v", err)
	}
	if reactivated.IsDisabled {
		t.Fatalf("expected alice to no longer be disabled")
	}
}

func TestAccountService_SetDisabled_DeletesLiveSessions(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, _ := h.register(t, "alice")
	sessions := repository.NewSessionRepository(h.db)
	if _, err := sessions.Create(ctx, alice.ID, "refresh-token-hash", time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := h.accounts.SetDisabled(ctx, alice.ID, true); err != nil {
		t.Fatalf("disable alice: %v", err)
	}

	if _, err := sessions.GetByRefreshTokenHash(ctx, "refresh-token-hash"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the existing session to be invalidated, got %v", err)
	}
}

func TestAccountService_SetDisabled_RefusesTheSoleOwnerOfANonEmptyWorkspace(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, _ := h.register(t, "bob")
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)

	_, err := h.accounts.SetDisabled(ctx, alice.ID, true)
	if !errors.Is(err, ErrSoleWorkspaceOwner) {
		t.Fatalf("expected ErrSoleWorkspaceOwner, got %v", err)
	}
	var soleOwnerErr *SoleWorkspaceOwnerError
	if !errors.As(err, &soleOwnerErr) {
		t.Fatalf("expected *SoleWorkspaceOwnerError, got %T", err)
	}
	if !slices.Contains(soleOwnerErr.WorkspaceNames, aliceWorkspace.Name) {
		t.Fatalf("expected blocking workspace names to include %q, got %v", aliceWorkspace.Name, soleOwnerErr.WorkspaceNames)
	}
}

func TestAccountService_SetDisabled_SucceedsOnceWorkspaceIsReducedToJustTheOwner(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, _ := h.register(t, "bob")
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)

	if err := h.workspaces.RemoveMember(ctx, alice.ID, aliceWorkspace.ID, bob.ID, nil); err != nil {
		t.Fatalf("remove bob: %v", err)
	}

	if _, err := h.accounts.SetDisabled(ctx, alice.ID, true); err != nil {
		t.Fatalf("expected disable to succeed once alice is the workspace's only member, got %v", err)
	}
}

func TestAccountService_SetDisabled_SucceedsOnceOwnershipIsTransferred(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, _ := h.register(t, "bob")
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)

	if _, err := h.db.Exec("UPDATE workspaces SET owner_user_id = ? WHERE id = ?", bob.ID, aliceWorkspace.ID); err != nil {
		t.Fatalf("transfer workspace ownership to bob: %v", err)
	}
	if _, err := h.db.Exec("UPDATE workspace_members SET role = 'owner' WHERE workspace_id = ? AND user_id = ?", aliceWorkspace.ID, bob.ID); err != nil {
		t.Fatalf("promote bob to owner: %v", err)
	}
	if _, err := h.db.Exec("UPDATE workspace_members SET role = 'member' WHERE workspace_id = ? AND user_id = ?", aliceWorkspace.ID, alice.ID); err != nil {
		t.Fatalf("demote alice to member: %v", err)
	}

	if _, err := h.accounts.SetDisabled(ctx, alice.ID, true); err != nil {
		t.Fatalf("expected disable to succeed once alice is no longer the owner, got %v", err)
	}
}

func TestAccountService_DeleteImpact_ReportsPerCalendarWorkspaceAndTransferCandidates(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, _ := h.register(t, "bob")
	carol, _ := h.register(t, "carol")
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)

	shared, err := h.calendars.Create(ctx, alice.ID, aliceWorkspace.ID, "cal-shared", CalendarWrite{Name: "Shared", Color: "#112233FF"})
	if err != nil {
		t.Fatalf("create shared calendar: %v", err)
	}
	if _, _, err := h.calendars.Share(ctx, alice.ID, shared.ID, "bob@example.com", repository.RoleEditor); err != nil {
		t.Fatalf("share with bob: %v", err)
	}

	impact, err := h.accounts.DeleteImpact(ctx, alice.ID)
	if err != nil {
		t.Fatalf("delete impact: %v", err)
	}

	var sharedImpact *CalendarImpact
	for i, c := range impact.Calendars {
		if c.ID == shared.ID {
			sharedImpact = &impact.Calendars[i]
		}
	}
	if sharedImpact == nil {
		t.Fatalf("expected the shared calendar to appear in the impact report: %+v", impact.Calendars)
	}
	if sharedImpact.ShareCount != 1 {
		t.Fatalf("expected 1 share, got %d", sharedImpact.ShareCount)
	}
	if sharedImpact.WorkspaceID != aliceWorkspace.ID || sharedImpact.WorkspaceName != aliceWorkspace.Name {
		t.Fatalf("expected the calendar's own workspace, got %+v", sharedImpact)
	}

	candidateNames := map[string]bool{}
	for _, c := range sharedImpact.TransferCandidates {
		candidateNames[c.Name] = true
	}
	if !candidateNames["bob"] {
		t.Fatalf("expected bob (a workspace member) to be a transfer candidate, got %+v", sharedImpact.TransferCandidates)
	}
	if candidateNames["alice"] {
		t.Fatalf("expected alice not to be her own transfer candidate")
	}
	if candidateNames["carol"] {
		t.Fatalf("expected carol, who isn't a member of alice's workspace, not to be a transfer candidate")
	}
	_ = carol
}

func TestAccountService_Delete_RequiresADispositionForEveryOwnedCalendar(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	h.createCalendar(t, alice.ID, aliceWorkspace.ID, "cal-1", "Personal")

	if err := h.accounts.Delete(ctx, alice.ID, nil); !errors.Is(err, ErrMissingDisposition) {
		t.Fatalf("expected ErrMissingDisposition when no disposition is given for an owned calendar, got %v", err)
	}
}

func TestAccountService_Delete_RejectsDuplicateCalendarInDispositions(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	calendar := h.createCalendar(t, alice.ID, aliceWorkspace.ID, "cal-1", "Personal")

	dispositions := []CalendarDisposition{
		{CalendarID: calendar.ID, Disposition: DispositionDelete},
		{CalendarID: calendar.ID, Disposition: DispositionDelete},
	}
	if err := h.accounts.Delete(ctx, alice.ID, dispositions); !errors.Is(err, ErrDuplicateDisposition) {
		t.Fatalf("expected ErrDuplicateDisposition, got %v", err)
	}
}

func TestAccountService_Delete_RejectsCalendarNotOwnedByCaller(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, _ := h.register(t, "alice")
	bob, bobWorkspace := h.register(t, "bob")
	bobCalendar := h.createCalendar(t, bob.ID, bobWorkspace.ID, "cal-1", "Bob's")

	dispositions := []CalendarDisposition{{CalendarID: bobCalendar.ID, Disposition: DispositionDelete}}
	if err := h.accounts.Delete(ctx, alice.ID, dispositions); !errors.Is(err, ErrCalendarNotOwned) {
		t.Fatalf("expected ErrCalendarNotOwned, got %v", err)
	}
}

func TestAccountService_Delete_InvalidDisposition(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	calendar := h.createCalendar(t, alice.ID, aliceWorkspace.ID, "cal-1", "Personal")

	dispositions := []CalendarDisposition{{CalendarID: calendar.ID, Disposition: "cascade"}}
	if err := h.accounts.Delete(ctx, alice.ID, dispositions); !errors.Is(err, ErrInvalidDisposition) {
		t.Fatalf("expected ErrInvalidDisposition, got %v", err)
	}
}

func TestAccountService_Delete_TransferRequiresTransferTo(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	calendar := h.createCalendar(t, alice.ID, aliceWorkspace.ID, "cal-1", "Personal")

	dispositions := []CalendarDisposition{{CalendarID: calendar.ID, Disposition: DispositionTransfer}}
	if err := h.accounts.Delete(ctx, alice.ID, dispositions); !errors.Is(err, ErrTransferTargetRequired) {
		t.Fatalf("expected ErrTransferTargetRequired, got %v", err)
	}
}

func TestAccountService_Delete_RejectsTransferToSelf(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	calendar := h.createCalendar(t, alice.ID, aliceWorkspace.ID, "cal-1", "Personal")

	self := alice.ID
	dispositions := []CalendarDisposition{{CalendarID: calendar.ID, Disposition: DispositionTransfer, TransferTo: &self}}
	if err := h.accounts.Delete(ctx, alice.ID, dispositions); !errors.Is(err, ErrCannotTransferToSelf) {
		t.Fatalf("expected ErrCannotTransferToSelf, got %v", err)
	}
}

func TestAccountService_Delete_RejectsTransferTargetOutsideTheCalendarsWorkspace(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	carol, _ := h.register(t, "carol") // not a member of alice's workspace
	calendar := h.createCalendar(t, alice.ID, aliceWorkspace.ID, "cal-1", "Personal")

	dispositions := []CalendarDisposition{{CalendarID: calendar.ID, Disposition: DispositionTransfer, TransferTo: &carol.ID}}
	if err := h.accounts.Delete(ctx, alice.ID, dispositions); !errors.Is(err, ErrTransferTargetNotWorkspaceMember) {
		t.Fatalf("expected ErrTransferTargetNotWorkspaceMember, got %v", err)
	}
}

func TestAccountService_Delete_RefusesTheSoleOwnerOfANonEmptyWorkspace(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, _ := h.register(t, "bob")
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)

	err := h.accounts.Delete(ctx, alice.ID, nil)
	if !errors.Is(err, ErrSoleWorkspaceOwner) {
		t.Fatalf("expected ErrSoleWorkspaceOwner, got %v", err)
	}
	var soleOwnerErr *SoleWorkspaceOwnerError
	if !errors.As(err, &soleOwnerErr) {
		t.Fatalf("expected *SoleWorkspaceOwnerError, got %T", err)
	}
	if !slices.Contains(soleOwnerErr.WorkspaceNames, aliceWorkspace.Name) {
		t.Fatalf("expected blocking workspace names to include %q, got %v", aliceWorkspace.Name, soleOwnerErr.WorkspaceNames)
	}
}

// TestAccountService_Delete_DispositionDelete_RemovesOwnedCalendarsWorkspaceAndAccount
// covers self-Delete with the delete disposition end to end: every owned
// Calendar and its Events are gone, the User's own solo Workspace is
// retired, and the account itself is gone.
func TestAccountService_Delete_DispositionDelete_RemovesOwnedCalendarsWorkspaceAndAccount(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	h.deleteAllCalendars(t, alice.ID)
	calendarA := h.createCalendar(t, alice.ID, aliceWorkspace.ID, "cal-1", "Personal")
	calendarB := h.createCalendar(t, alice.ID, aliceWorkspace.ID, "cal-2", "Work")

	dispositions := []CalendarDisposition{
		{CalendarID: calendarA.ID, Disposition: DispositionDelete},
		{CalendarID: calendarB.ID, Disposition: DispositionDelete},
	}

	if err := h.accounts.Delete(ctx, alice.ID, dispositions); err != nil {
		t.Fatalf("delete alice: %v", err)
	}

	calendarRepo := repository.NewCalendarRepository(h.db)
	for _, id := range []string{calendarA.ID, calendarB.ID} {
		if _, err := calendarRepo.GetByIDAny(ctx, id); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("expected calendar %s to be deleted, got %v", id, err)
		}
	}

	workspaceRepo := repository.NewWorkspaceRepository(h.db)
	if _, err := workspaceRepo.GetByID(ctx, aliceWorkspace.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected alice's solo workspace to be retired, got %v", err)
	}

	if _, err := h.users.GetByID(ctx, alice.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected alice's account to be gone, got %v", err)
	}
}

// TestAccountService_Delete_DispositionTransfer_KeepsEventsAndShares covers
// self-Delete with the transfer disposition: a transferred Calendar keeps
// its Events (including ones written by other people) and existing Shares.
func TestAccountService_Delete_DispositionTransfer_KeepsEventsAndShares(t *testing.T) {
	h := newAccountTestHarness(t)
	ctx := context.Background()

	alice, aliceWorkspace := h.register(t, "alice")
	bob, _ := h.register(t, "bob")
	h.addMember(t, aliceWorkspace.ID, bob.ID, repository.WorkspaceRoleMember)
	h.deleteAllCalendars(t, alice.ID)

	calendar, err := h.calendars.Create(ctx, alice.ID, aliceWorkspace.ID, "cal-1", CalendarWrite{Name: "Family", Color: "#112233FF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	if _, _, err := h.calendars.Share(ctx, alice.ID, calendar.ID, "bob@example.com", repository.RoleEditor); err != nil {
		t.Fatalf("share calendar with bob: %v", err)
	}

	events := repository.NewEventRepository(h.db)
	bobID := bob.ID
	event, err := events.Create(ctx, "evt-1", &bobID, repository.EventFields{
		CalendarID: calendar.ID,
		Title:      "Bob's event",
		Start:      time.Now(),
		End:        time.Now().Add(time.Hour),
	}, 1)
	if err != nil {
		t.Fatalf("create event authored by bob: %v", err)
	}

	// Transfer Workspace Ownership to bob so alice is no longer its Owner —
	// the sole-owner guard only cares about Ownership, and bob stays a
	// Member throughout, so the calendar transfer below still lands on a
	// valid target and the workspace (with its calendars/shares/events)
	// stays alive. Mirrors ADR-0044's story: a sole Owner must transfer
	// Ownership (or empty the workspace) before they can delete themselves.
	if _, err := h.db.Exec("UPDATE workspaces SET owner_user_id = ? WHERE id = ?", bob.ID, aliceWorkspace.ID); err != nil {
		t.Fatalf("transfer workspace ownership to bob: %v", err)
	}
	if _, err := h.db.Exec("UPDATE workspace_members SET role = 'owner' WHERE workspace_id = ? AND user_id = ?", aliceWorkspace.ID, bob.ID); err != nil {
		t.Fatalf("promote bob to owner: %v", err)
	}
	if _, err := h.db.Exec("UPDATE workspace_members SET role = 'member' WHERE workspace_id = ? AND user_id = ?", aliceWorkspace.ID, alice.ID); err != nil {
		t.Fatalf("demote alice to member: %v", err)
	}

	if err := h.accounts.Delete(ctx, alice.ID, []CalendarDisposition{
		{CalendarID: calendar.ID, Disposition: DispositionTransfer, TransferTo: &bob.ID},
	}); err != nil {
		t.Fatalf("delete alice with transfer to bob: %v", err)
	}

	calendarRepo := repository.NewCalendarRepository(h.db)
	transferred, err := calendarRepo.GetByIDAny(ctx, calendar.ID)
	if err != nil {
		t.Fatalf("expected the calendar to survive the transfer: %v", err)
	}
	if transferred.UserID != bob.ID {
		t.Fatalf("expected the calendar to be owned by bob, got owner %d", transferred.UserID)
	}

	survivingEvent, err := events.GetByID(ctx, event.ID)
	if err != nil {
		t.Fatalf("expected bob's event to survive the transfer: %v", err)
	}
	if survivingEvent.Title != "Bob's event" {
		t.Fatalf("expected the event to be unchanged, got %+v", survivingEvent)
	}

	shareRepo := repository.NewCalendarShareRepository(h.db)
	if _, err := shareRepo.Get(ctx, calendar.ID, bob.ID); err != nil {
		t.Fatalf("expected bob's share to survive the transfer: %v", err)
	}

	if _, err := h.users.GetByID(ctx, alice.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected alice's account to be gone, got %v", err)
	}
}

// TestValidateName covers ADR-0047's display-name rules (#125): unlike the
// old username, a Name may contain internal whitespace and colons — "Jane
// Smith" must be accepted.
func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "trims surrounding whitespace", input: "  alice  ", want: "alice"},
		{name: "empty is rejected", input: "", wantErr: true},
		{name: "all whitespace is rejected", input: "   ", wantErr: true},
		{name: "internal whitespace is accepted", input: "Jane Smith", want: "Jane Smith"},
		{name: "a colon is accepted", input: "ali:ce", want: "ali:ce"},
		{name: "over the length limit is rejected", input: strings.Repeat("a", maxNameLength+1), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateName(tt.input)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidDisplayName) {
					t.Fatalf("expected ErrInvalidDisplayName, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateName(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("validateName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestValidateEmail covers ADR-0047's identifier rules: the colon/whitespace
// rejection that used to live on username moves here, since Email is now
// what CalDAV Basic auth authenticates against.
func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "trims surrounding whitespace", input: "  alice@example.com  ", want: "alice@example.com"},
		{name: "empty is rejected", input: "", wantErr: ErrEmailRequired},
		{name: "all whitespace is rejected", input: "   ", wantErr: ErrEmailRequired},
		{name: "internal whitespace is rejected", input: "alice bob@example.com", wantErr: ErrInvalidEmail},
		{name: "a colon is rejected", input: "ali:ce@example.com", wantErr: ErrInvalidEmail},
		{name: "not a well-formed address is rejected", input: "not-an-email", wantErr: ErrInvalidEmail},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateEmail(tt.input)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateEmail(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("validateEmail(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
