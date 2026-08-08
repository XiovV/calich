package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

func newTestAccountService(t *testing.T) *AccountService {
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
	calendars := NewCalendarService(calendarRepo, shareRepo, users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB))
	appPasswords := NewAppPasswordService(repository.NewAppPasswordRepository(sqlDB), users)

	return NewAccountService(sqlDB, users, sessions, calendarRepo, shareRepo, calendars, appPasswords)
}

func TestAccountService_Create_SeedsDefaultCalendarsAndForcesPasswordChange(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	user, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if user.Username != "alice" {
		t.Fatalf("expected username alice, got %q", user.Username)
	}
	if !user.MustChangePassword {
		t.Fatalf("expected a newly created account to require a password change")
	}
	if user.IsAdmin {
		t.Fatalf("expected a newly created account to not be an admin by default")
	}

	calendars, err := accounts.calendars.List(ctx, user.ID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}
	if len(calendars) == 0 {
		t.Fatalf("expected the new account to start with default calendars")
	}
}

func TestAccountService_Create_EmptyUsername_ReturnsErrInvalidUsername(t *testing.T) {
	accounts := newTestAccountService(t)

	_, err := accounts.Create(context.Background(), "  ", "temp-secret")
	if !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("expected ErrInvalidUsername, got %v", err)
	}
}

func TestAccountService_Create_EmptyPassword_ReturnsErrInvalidPassword(t *testing.T) {
	accounts := newTestAccountService(t)

	_, err := accounts.Create(context.Background(), "alice", "")
	if !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}
}

func TestAccountService_Create_DuplicateUsername_ReturnsErrUsernameTaken(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	if _, err := accounts.Create(ctx, "alice", "temp-secret"); err != nil {
		t.Fatalf("create first account: %v", err)
	}

	if _, err := accounts.Create(ctx, "alice", "another-secret"); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}
}

func TestAccountService_List_ReturnsEveryAccount(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	if _, err := accounts.Create(ctx, "alice", "temp-secret"); err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if _, err := accounts.Create(ctx, "bob", "temp-secret"); err != nil {
		t.Fatalf("create bob: %v", err)
	}

	users, err := accounts.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(users))
	}
}

func TestAccountService_ResetPassword_ForcesPasswordChangeAndInvalidatesSessions(t *testing.T) {
	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	calendarRepo := repository.NewCalendarRepository(sqlDB)
	shareRepo := repository.NewCalendarShareRepository(sqlDB)
	calendars := NewCalendarService(calendarRepo, shareRepo, users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB))
	appPasswords := NewAppPasswordService(repository.NewAppPasswordRepository(sqlDB), users)
	accounts := NewAccountService(sqlDB, users, sessions, calendarRepo, shareRepo, calendars, appPasswords)
	ctx := context.Background()

	user, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	// Simulate alice having already changed her password and logged in with
	// a live session, so ResetPassword has something to invalidate.
	if err := users.UpdatePassword(ctx, user.ID, "some-hash"); err != nil {
		t.Fatalf("update password: %v", err)
	}
	if _, err := sessions.Create(ctx, user.ID, "refresh-token-hash", time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}

	updated, err := accounts.ResetPassword(ctx, user.ID, "new-temp-secret")
	if err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if !updated.MustChangePassword {
		t.Fatalf("expected must_change_password to be set after an admin reset")
	}

	if _, err := sessions.GetByRefreshTokenHash(ctx, "refresh-token-hash"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the existing session to be invalidated, got %v", err)
	}
}

func TestAccountService_ResetPassword_EmptyPassword_ReturnsErrInvalidPassword(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	user, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	if _, err := accounts.ResetPassword(ctx, user.ID, ""); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}
}

func TestAccountService_SetAdmin_GrantsAndRevokes(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	// Two admins so revoking one never hits the last-admin guard.
	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if _, err := accounts.SetAdmin(ctx, alice.ID, true); err != nil {
		t.Fatalf("grant alice admin: %v", err)
	}

	bob, err := accounts.Create(ctx, "bob", "temp-secret")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	granted, err := accounts.SetAdmin(ctx, bob.ID, true)
	if err != nil {
		t.Fatalf("grant bob admin: %v", err)
	}
	if !granted.IsAdmin {
		t.Fatalf("expected bob to be an admin")
	}

	revoked, err := accounts.SetAdmin(ctx, bob.ID, false)
	if err != nil {
		t.Fatalf("revoke bob admin: %v", err)
	}
	if revoked.IsAdmin {
		t.Fatalf("expected bob to no longer be an admin")
	}
}

func TestAccountService_SetAdmin_RefusesToDemoteTheLastRemainingAdmin(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if _, err := accounts.SetAdmin(ctx, alice.ID, true); err != nil {
		t.Fatalf("grant alice admin: %v", err)
	}

	if _, err := accounts.SetAdmin(ctx, alice.ID, false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("expected ErrLastAdmin, got %v", err)
	}
}

func TestAccountService_SetDisabled_DisablesAndReenables(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	disabled, err := accounts.SetDisabled(ctx, alice.ID, true)
	if err != nil {
		t.Fatalf("disable alice: %v", err)
	}
	if !disabled.IsDisabled {
		t.Fatalf("expected alice to be disabled")
	}

	enabled, err := accounts.SetDisabled(ctx, alice.ID, false)
	if err != nil {
		t.Fatalf("re-enable alice: %v", err)
	}
	if enabled.IsDisabled {
		t.Fatalf("expected alice to no longer be disabled")
	}
}

func TestAccountService_SetDisabled_DeletesLiveSessions(t *testing.T) {
	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	calendarRepo := repository.NewCalendarRepository(sqlDB)
	shareRepo := repository.NewCalendarShareRepository(sqlDB)
	calendars := NewCalendarService(calendarRepo, shareRepo, users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB))
	appPasswords := NewAppPasswordService(repository.NewAppPasswordRepository(sqlDB), users)
	accounts := NewAccountService(sqlDB, users, sessions, calendarRepo, shareRepo, calendars, appPasswords)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := sessions.Create(ctx, alice.ID, "refresh-token-hash", time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := accounts.SetDisabled(ctx, alice.ID, true); err != nil {
		t.Fatalf("disable alice: %v", err)
	}

	if _, err := sessions.GetByRefreshTokenHash(ctx, "refresh-token-hash"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the existing session to be invalidated, got %v", err)
	}
}

func TestAccountService_SetDisabled_RefusesToDisableTheLastRemainingAdmin(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if _, err := accounts.SetAdmin(ctx, alice.ID, true); err != nil {
		t.Fatalf("grant alice admin: %v", err)
	}

	if _, err := accounts.SetDisabled(ctx, alice.ID, true); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("expected ErrLastAdmin, got %v", err)
	}
}

func TestAccountService_SetDisabled_AllowsDisablingAnAdminWhenAnotherAdminRemains(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if _, err := accounts.SetAdmin(ctx, alice.ID, true); err != nil {
		t.Fatalf("grant alice admin: %v", err)
	}

	bob, err := accounts.Create(ctx, "bob", "temp-secret")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if _, err := accounts.SetAdmin(ctx, bob.ID, true); err != nil {
		t.Fatalf("grant bob admin: %v", err)
	}

	disabled, err := accounts.SetDisabled(ctx, alice.ID, true)
	if err != nil {
		t.Fatalf("disable alice: %v", err)
	}
	if !disabled.IsDisabled {
		t.Fatalf("expected alice to be disabled")
	}
}

func TestAccountService_SetDisabled_DisablingANonAdminNeverTripsTheLastAdminGuard(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	disabled, err := accounts.SetDisabled(ctx, alice.ID, true)
	if err != nil {
		t.Fatalf("expected no error disabling a non-admin, got %v", err)
	}
	if !disabled.IsDisabled {
		t.Fatalf("expected alice to be disabled")
	}
}

func TestAccountService_SetDisabled_DemotedAdminCanBeReenabledOnceAnotherAdminExists(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	// Re-enabling a disabled admin must never be blocked by the last-admin
	// guard — that guard only protects against losing the last one, and
	// enabling never removes an admin.
	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if _, err := accounts.SetAdmin(ctx, alice.ID, true); err != nil {
		t.Fatalf("grant alice admin: %v", err)
	}

	bob, err := accounts.Create(ctx, "bob", "temp-secret")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if _, err := accounts.SetAdmin(ctx, bob.ID, true); err != nil {
		t.Fatalf("grant bob admin: %v", err)
	}
	if _, err := accounts.SetDisabled(ctx, alice.ID, true); err != nil {
		t.Fatalf("disable alice: %v", err)
	}

	enabled, err := accounts.SetDisabled(ctx, alice.ID, false)
	if err != nil {
		t.Fatalf("re-enable alice: %v", err)
	}
	if enabled.IsDisabled {
		t.Fatalf("expected alice to no longer be disabled")
	}
}

func TestAccountService_SetAdmin_DemotingANonAdminIsANoopNotAnError(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	// alice is the sole account and not an admin — revoking admin from her
	// (a no-op) must not trip the last-admin guard, which only protects an
	// actual admin from being demoted.
	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	revoked, err := accounts.SetAdmin(ctx, alice.ID, false)
	if err != nil {
		t.Fatalf("expected no error revoking admin from a non-admin, got %v", err)
	}
	if revoked.IsAdmin {
		t.Fatalf("expected alice to remain a non-admin")
	}
}

func TestAccountService_Delete_RejectsMissingOrInvalidDisposition(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	if err := accounts.Delete(ctx, alice.ID, "", nil); !errors.Is(err, ErrInvalidDisposition) {
		t.Fatalf("expected ErrInvalidDisposition for an empty disposition, got %v", err)
	}
	if err := accounts.Delete(ctx, alice.ID, "cascade", nil); !errors.Is(err, ErrInvalidDisposition) {
		t.Fatalf("expected ErrInvalidDisposition for an unrecognized disposition, got %v", err)
	}
}

func TestAccountService_Delete_TransferRequiresTransferTo(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	if err := accounts.Delete(ctx, alice.ID, DispositionTransfer, nil); !errors.Is(err, ErrTransferTargetRequired) {
		t.Fatalf("expected ErrTransferTargetRequired, got %v", err)
	}
}

func TestAccountService_Delete_RejectsUnknownTransferTarget(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	ghost := int64(9999)
	if err := accounts.Delete(ctx, alice.ID, DispositionTransfer, &ghost); !errors.Is(err, ErrTransferTargetNotFound) {
		t.Fatalf("expected ErrTransferTargetNotFound, got %v", err)
	}
}

func TestAccountService_Delete_RejectsTransferToSelf(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	self := alice.ID
	if err := accounts.Delete(ctx, alice.ID, DispositionTransfer, &self); !errors.Is(err, ErrCannotTransferToSelf) {
		t.Fatalf("expected ErrCannotTransferToSelf, got %v", err)
	}
}

func TestAccountService_Delete_RefusesToDeleteTheLastRemainingAdmin(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if _, err := accounts.SetAdmin(ctx, alice.ID, true); err != nil {
		t.Fatalf("grant alice admin: %v", err)
	}

	if err := accounts.Delete(ctx, alice.ID, DispositionDelete, nil); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("expected ErrLastAdmin, got %v", err)
	}
}

func TestAccountService_Delete_AllowsDeletingAnAdminWhenAnotherAdminRemains(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if _, err := accounts.SetAdmin(ctx, alice.ID, true); err != nil {
		t.Fatalf("grant alice admin: %v", err)
	}
	bob, err := accounts.Create(ctx, "bob", "temp-secret")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if _, err := accounts.SetAdmin(ctx, bob.ID, true); err != nil {
		t.Fatalf("grant bob admin: %v", err)
	}

	if err := accounts.Delete(ctx, alice.ID, DispositionDelete, nil); err != nil {
		t.Fatalf("delete alice: %v", err)
	}
	if _, err := accounts.users.GetByID(ctx, alice.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected alice to be gone, got %v", err)
	}
}

// TestAccountService_Delete_DispositionDelete_RemovesOwnedCalendarsAndEvents
// covers ADR-0037's "the delete disposition removes the User's Calendars
// and their Events".
func TestAccountService_Delete_DispositionDelete_RemovesOwnedCalendarsAndEvents(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	calendars, err := accounts.calendars.List(ctx, alice.ID)
	if err != nil {
		t.Fatalf("list alice's default calendars: %v", err)
	}
	if len(calendars) == 0 {
		t.Fatalf("expected alice to start with default calendars")
	}

	if err := accounts.Delete(ctx, alice.ID, DispositionDelete, nil); err != nil {
		t.Fatalf("delete alice: %v", err)
	}

	for _, c := range calendars {
		if _, err := accounts.calendarRepo.GetByIDAny(ctx, c.ID); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("expected calendar %s to be deleted along with its owner, got %v", c.ID, err)
		}
	}
}

// TestAccountService_Delete_DispositionTransfer_KeepsEventsAndShares covers
// ADR-0037's "transferred Calendars keep their Events, including Events
// created by other people, and keep their existing Shares".
func TestAccountService_Delete_DispositionTransfer_KeepsEventsAndShares(t *testing.T) {
	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	ctx := context.Background()

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	calendarRepo := repository.NewCalendarRepository(sqlDB)
	shareRepo := repository.NewCalendarShareRepository(sqlDB)
	calendarService := NewCalendarService(calendarRepo, shareRepo, users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB))
	appPasswords := NewAppPasswordService(repository.NewAppPasswordRepository(sqlDB), users)
	accounts := NewAccountService(sqlDB, users, sessions, calendarRepo, shareRepo, calendarService, appPasswords)

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := accounts.Create(ctx, "bob", "temp-secret")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	carol, err := accounts.Create(ctx, "carol", "temp-secret")
	if err != nil {
		t.Fatalf("create carol: %v", err)
	}

	calendar, err := calendarService.Create(ctx, alice.ID, "cal-1", CalendarWrite{Name: "Family", Color: "#112233FF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	if _, err := calendarService.Share(ctx, alice.ID, calendar.ID, "bob", repository.RoleEditor); err != nil {
		t.Fatalf("share calendar with bob: %v", err)
	}

	events := repository.NewEventRepository(sqlDB)
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

	if err := accounts.Delete(ctx, alice.ID, DispositionTransfer, &carol.ID); err != nil {
		t.Fatalf("delete alice with transfer to carol: %v", err)
	}

	transferred, err := calendarRepo.GetByIDAny(ctx, calendar.ID)
	if err != nil {
		t.Fatalf("expected the calendar to survive the transfer: %v", err)
	}
	if transferred.UserID != carol.ID {
		t.Fatalf("expected the calendar to be owned by carol, got owner %d", transferred.UserID)
	}

	survivingEvent, err := events.GetByID(ctx, event.ID)
	if err != nil {
		t.Fatalf("expected bob's event to survive the transfer: %v", err)
	}
	if survivingEvent.Title != "Bob's event" {
		t.Fatalf("expected the event to be unchanged, got %+v", survivingEvent)
	}

	if _, err := shareRepo.Get(ctx, calendar.ID, bob.ID); err != nil {
		t.Fatalf("expected bob's share to survive the transfer: %v", err)
	}

	if _, err := users.GetByID(ctx, alice.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected alice's account to be gone, got %v", err)
	}
}

// TestAccountService_Delete_RemovesSharesGrantedToTheDeletedUser covers
// ADR-0037's "Shares granted to the deleted User are removed" — regardless
// of disposition, since it concerns Shares the deleted User held on
// someone else's Calendar, not their own.
func TestAccountService_Delete_RemovesSharesGrantedToTheDeletedUser(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	owner, err := accounts.Create(ctx, "owner", "temp-secret")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	holder, err := accounts.Create(ctx, "holder", "temp-secret")
	if err != nil {
		t.Fatalf("create holder: %v", err)
	}

	calendar, err := accounts.calendars.Create(ctx, owner.ID, "cal-1", CalendarWrite{Name: "Family", Color: "#112233FF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	if _, err := accounts.calendars.Share(ctx, owner.ID, calendar.ID, "holder", repository.RoleViewer); err != nil {
		t.Fatalf("share calendar with holder: %v", err)
	}

	if err := accounts.Delete(ctx, holder.ID, DispositionDelete, nil); err != nil {
		t.Fatalf("delete holder: %v", err)
	}

	if _, err := accounts.shareRepo.Get(ctx, calendar.ID, holder.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected holder's share to be removed, got %v", err)
	}
	if _, err := accounts.calendarRepo.GetByIDAny(ctx, calendar.ID); err != nil {
		t.Fatalf("expected owner's calendar to survive holder's deletion: %v", err)
	}
}

// TestAccountService_DeleteImpact_ReportsShareCountsAndAffectedUsers covers
// ADR-0037's "the API reports, before deletion, which of the User's
// Calendars have Shares and how many Users would lose Access".
func TestAccountService_DeleteImpact_ReportsShareCountsAndAffectedUsers(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	owner, err := accounts.Create(ctx, "owner", "temp-secret")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if _, err := accounts.Create(ctx, "bob", "temp-secret"); err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if _, err := accounts.Create(ctx, "carol", "temp-secret"); err != nil {
		t.Fatalf("create carol: %v", err)
	}

	shared, err := accounts.calendars.Create(ctx, owner.ID, "cal-shared", CalendarWrite{Name: "Shared", Color: "#112233FF"})
	if err != nil {
		t.Fatalf("create shared calendar: %v", err)
	}
	if _, err := accounts.calendars.Share(ctx, owner.ID, shared.ID, "bob", repository.RoleEditor); err != nil {
		t.Fatalf("share with bob: %v", err)
	}
	if _, err := accounts.calendars.Share(ctx, owner.ID, shared.ID, "carol", repository.RoleViewer); err != nil {
		t.Fatalf("share with carol: %v", err)
	}
	if _, err := accounts.calendars.Create(ctx, owner.ID, "cal-private", CalendarWrite{Name: "Private", Color: "#445566FF"}); err != nil {
		t.Fatalf("create private calendar: %v", err)
	}

	impact, err := accounts.DeleteImpact(ctx, owner.ID)
	if err != nil {
		t.Fatalf("delete impact: %v", err)
	}

	if impact.AffectedUserCount != 2 {
		t.Fatalf("expected 2 affected users, got %d", impact.AffectedUserCount)
	}
	if len(impact.Calendars) != 5 { // owner's 3 defaults + cal-shared + cal-private
		t.Fatalf("expected 5 calendars in the impact report, got %d: %+v", len(impact.Calendars), impact.Calendars)
	}

	var sharedImpact *CalendarImpact
	for i, c := range impact.Calendars {
		if c.ID == shared.ID {
			sharedImpact = &impact.Calendars[i]
		}
	}
	if sharedImpact == nil {
		t.Fatalf("expected the shared calendar to appear in the impact report")
	}
	if sharedImpact.ShareCount != 2 {
		t.Fatalf("expected the shared calendar to report 2 shares, got %d", sharedImpact.ShareCount)
	}
}

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		want     string
		wantErr  bool
	}{
		{name: "trims surrounding whitespace", username: "  alice  ", want: "alice"},
		{name: "empty is rejected", username: "", wantErr: true},
		{name: "all whitespace is rejected", username: "   ", wantErr: true},
		{name: "internal whitespace is rejected", username: "alice bob", wantErr: true},
		{name: "a tab is rejected", username: "alice\tbob", wantErr: true},
		{name: "a colon is rejected", username: "ali:ce", wantErr: true},
		{name: "exactly 64 characters is allowed", username: strings.Repeat("a", 64), want: strings.Repeat("a", 64)},
		{name: "65 characters is rejected", username: strings.Repeat("a", 65), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateUsername(tt.username)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidUsername) {
					t.Fatalf("expected ErrInvalidUsername, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateUsername(%q): %v", tt.username, err)
			}
			if got != tt.want {
				t.Fatalf("validateUsername(%q) = %q, want %q", tt.username, got, tt.want)
			}
		})
	}
}

func TestAccountService_SetUsername_RenamesAccount(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	renamed, err := accounts.SetUsername(ctx, alice.ID, "alicia")
	if err != nil {
		t.Fatalf("set username: %v", err)
	}
	if renamed.Username != "alicia" {
		t.Fatalf("expected username alicia, got %q", renamed.Username)
	}
}

func TestAccountService_SetUsername_InvalidUsername_ReturnsErrInvalidUsername(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	if _, err := accounts.SetUsername(ctx, alice.ID, "ali:ce"); !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("expected ErrInvalidUsername, got %v", err)
	}
}

func TestAccountService_SetUsername_DuplicateUsername_ReturnsErrUsernameTaken(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if _, err := accounts.Create(ctx, "bob", "temp-secret"); err != nil {
		t.Fatalf("create bob: %v", err)
	}

	if _, err := accounts.SetUsername(ctx, alice.ID, "bob"); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}
}

func TestAccountService_SetUsername_UnknownID_ReturnsErrNotFound(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	if _, err := accounts.SetUsername(ctx, 999, "alicia"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAccountService_SetUsername_ExistingAppPasswordAuthenticatesUnderTheNewUsername(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	created, err := accounts.appPasswords.Create(ctx, alice.ID, "phone")
	if err != nil {
		t.Fatalf("create app password: %v", err)
	}

	if _, err := accounts.SetUsername(ctx, alice.ID, "alicia"); err != nil {
		t.Fatalf("set username: %v", err)
	}

	if _, err := accounts.appPasswords.Authenticate(ctx, "alice", created.Secret); err == nil {
		t.Fatalf("expected the old username to no longer authenticate")
	}
	userID, err := accounts.appPasswords.Authenticate(ctx, "alicia", created.Secret)
	if err != nil {
		t.Fatalf("authenticate under new username: %v", err)
	}
	if userID != alice.ID {
		t.Fatalf("expected authenticated user id %d, got %d", alice.ID, userID)
	}
}

func TestAccountService_UsernameImpact_ReportsAppPasswordCount(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	alice, err := accounts.Create(ctx, "alice", "temp-secret")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	count, err := accounts.UsernameImpact(ctx, alice.ID)
	if err != nil {
		t.Fatalf("username impact: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 app passwords for a fresh account, got %d", count)
	}

	if _, err := accounts.appPasswords.Create(ctx, alice.ID, "phone"); err != nil {
		t.Fatalf("create app password: %v", err)
	}
	if _, err := accounts.appPasswords.Create(ctx, alice.ID, "laptop"); err != nil {
		t.Fatalf("create second app password: %v", err)
	}

	count, err = accounts.UsernameImpact(ctx, alice.ID)
	if err != nil {
		t.Fatalf("username impact: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 app passwords, got %d", count)
	}
}

func TestAccountService_UsernameImpact_UnknownID_ReturnsErrNotFound(t *testing.T) {
	accounts := newTestAccountService(t)
	ctx := context.Background()

	if _, err := accounts.UsernameImpact(ctx, 999); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
