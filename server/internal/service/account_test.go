package service

import (
	"context"
	"errors"
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
	calendars := NewCalendarService(repository.NewCalendarRepository(sqlDB))

	return NewAccountService(users, sessions, calendars)
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
	calendars := NewCalendarService(repository.NewCalendarRepository(sqlDB))
	accounts := NewAccountService(users, sessions, calendars)
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
