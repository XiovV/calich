package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
)

func newTestUserRepository(t *testing.T) *UserRepository {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	return NewUserRepository(sqlDB)
}

func TestUserRepository_CreateAndGetByUsername(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "admin", "hashed-password", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if created.ID == 0 {
		t.Fatalf("expected a non-zero id, got %d", created.ID)
	}
	if !created.MustChangePassword {
		t.Fatalf("expected must_change_password to be true")
	}

	fetched, err := repo.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get by username: %v", err)
	}

	if fetched != created {
		t.Fatalf("expected fetched user %+v to equal created user %+v", fetched, created)
	}
}

func TestUserRepository_GetByUsername_NotFound(t *testing.T) {
	repo := newTestUserRepository(t)

	_, err := repo.GetByUsername(context.Background(), "nobody")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUserRepository_Create_DuplicateUsername(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, "admin", "hash1", true); err != nil {
		t.Fatalf("create first user: %v", err)
	}

	if _, err := repo.Create(ctx, "admin", "hash2", true); err == nil {
		t.Fatalf("expected an error creating a duplicate username, got nil")
	}
}

func TestUserRepository_UpdatePassword(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "admin", "old-hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := repo.UpdatePassword(ctx, created.ID, "new-hash"); err != nil {
		t.Fatalf("update password: %v", err)
	}

	updated, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}

	if updated.PasswordHash != "new-hash" {
		t.Fatalf("expected password hash to be updated, got %q", updated.PasswordHash)
	}
	if updated.MustChangePassword {
		t.Fatalf("expected must_change_password to be cleared after updating password")
	}
}

func TestUserRepository_UpdateEmail(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "admin", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if created.Email != nil {
		t.Fatalf("expected a freshly created user to have no email, got %+v", created.Email)
	}

	updated, err := repo.UpdateEmail(ctx, created.ID, "admin@example.com")
	if err != nil {
		t.Fatalf("update email: %v", err)
	}
	if updated.Email == nil || *updated.Email != "admin@example.com" {
		t.Fatalf("expected email to be updated, got %+v", updated.Email)
	}

	cleared, err := repo.UpdateEmail(ctx, created.ID, "")
	if err != nil {
		t.Fatalf("clear email: %v", err)
	}
	if cleared.Email != nil {
		t.Fatalf("expected email to be cleared, got %+v", cleared.Email)
	}
}

func TestUserRepository_Count(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	count, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 users, got %d", count)
	}

	if _, err := repo.Create(ctx, "admin", "hash", true); err != nil {
		t.Fatalf("create user: %v", err)
	}

	count, err = repo.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 user, got %d", count)
	}
}

func TestUserRepository_First(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "admin", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	first, err := repo.First(ctx)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first != created {
		t.Fatalf("expected first user %+v to equal created user %+v", first, created)
	}
}

func TestUserRepository_First_NotFound(t *testing.T) {
	repo := newTestUserRepository(t)

	_, err := repo.First(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUserRepository_SyncedDeviceRemindersEnabled_DefaultsOff(t *testing.T) {
	repo := newTestUserRepository(t)

	created, err := repo.Create(context.Background(), "admin", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if created.SyncedDeviceRemindersEnabled {
		t.Fatalf("expected synced device reminders to default off")
	}
}

func TestUserRepository_Create_DefaultsToNonAdmin(t *testing.T) {
	repo := newTestUserRepository(t)

	created, err := repo.Create(context.Background(), "admin", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if created.IsAdmin {
		t.Fatalf("expected a freshly created user to not be an admin")
	}
}

func TestUserRepository_Create_DuplicateUsername_ReturnsErrUsernameTaken(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, "admin", "hash1", true); err != nil {
		t.Fatalf("create first user: %v", err)
	}

	if _, err := repo.Create(ctx, "admin", "hash2", true); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}
}

func TestUserRepository_SetAdmin(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "admin", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	granted, err := repo.SetAdmin(ctx, created.ID, true)
	if err != nil {
		t.Fatalf("set admin: %v", err)
	}
	if !granted.IsAdmin {
		t.Fatalf("expected user to be an admin")
	}

	revoked, err := repo.SetAdmin(ctx, created.ID, false)
	if err != nil {
		t.Fatalf("revoke admin: %v", err)
	}
	if revoked.IsAdmin {
		t.Fatalf("expected user to no longer be an admin")
	}
}

func TestUserRepository_CountAdmins(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	alice, err := repo.Create(ctx, "alice", "hash", true)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if _, err := repo.Create(ctx, "bob", "hash", true); err != nil {
		t.Fatalf("create bob: %v", err)
	}

	count, err := repo.CountAdmins(ctx)
	if err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 admins, got %d", count)
	}

	if _, err := repo.SetAdmin(ctx, alice.ID, true); err != nil {
		t.Fatalf("set admin: %v", err)
	}

	count, err = repo.CountAdmins(ctx)
	if err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 admin, got %d", count)
	}
}

func TestUserRepository_SetDisabled(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "alice", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if created.IsDisabled {
		t.Fatalf("expected a freshly created user to not be disabled")
	}

	disabled, err := repo.SetDisabled(ctx, created.ID, true)
	if err != nil {
		t.Fatalf("set disabled: %v", err)
	}
	if !disabled.IsDisabled {
		t.Fatalf("expected user to be disabled")
	}

	enabled, err := repo.SetDisabled(ctx, created.ID, false)
	if err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if enabled.IsDisabled {
		t.Fatalf("expected user to no longer be disabled")
	}
}

func TestUserRepository_CountEnabledAdmins(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	alice, err := repo.Create(ctx, "alice", "hash", true)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := repo.Create(ctx, "bob", "hash", true)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if _, err := repo.SetAdmin(ctx, alice.ID, true); err != nil {
		t.Fatalf("grant alice admin: %v", err)
	}
	if _, err := repo.SetAdmin(ctx, bob.ID, true); err != nil {
		t.Fatalf("grant bob admin: %v", err)
	}

	count, err := repo.CountEnabledAdmins(ctx)
	if err != nil {
		t.Fatalf("count enabled admins: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 enabled admins, got %d", count)
	}

	// A disabled admin no longer counts toward "still administrable".
	if _, err := repo.SetDisabled(ctx, alice.ID, true); err != nil {
		t.Fatalf("disable alice: %v", err)
	}

	count, err = repo.CountEnabledAdmins(ctx)
	if err != nil {
		t.Fatalf("count enabled admins: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 enabled admin after disabling alice, got %d", count)
	}
}

func TestUserRepository_ResetPassword_SetsMustChangePassword(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "admin", "old-hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := repo.ResetPassword(ctx, created.ID, "new-hash"); err != nil {
		t.Fatalf("reset password: %v", err)
	}

	updated, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if updated.PasswordHash != "new-hash" {
		t.Fatalf("expected password hash to be updated, got %q", updated.PasswordHash)
	}
	if !updated.MustChangePassword {
		t.Fatalf("expected must_change_password to be set after an admin reset")
	}
}

func TestUserRepository_List(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	alice, err := repo.Create(ctx, "alice", "hash", true)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := repo.Create(ctx, "bob", "hash", true)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	users, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
	if users[0] != alice {
		t.Fatalf("expected alice first (creation order), got %+v", users[0])
	}
	if users[1] != bob {
		t.Fatalf("expected bob second (creation order), got %+v", users[1])
	}
}

func TestUserRepository_List_Empty(t *testing.T) {
	repo := newTestUserRepository(t)

	users, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected no users, got %d", len(users))
	}
}

func TestUserRepository_UpdateSyncedDeviceReminders(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "admin", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	updated, err := repo.UpdateSyncedDeviceReminders(ctx, created.ID, true)
	if err != nil {
		t.Fatalf("update synced device reminders: %v", err)
	}
	if !updated.SyncedDeviceRemindersEnabled {
		t.Fatalf("expected synced device reminders to be enabled")
	}

	reverted, err := repo.UpdateSyncedDeviceReminders(ctx, created.ID, false)
	if err != nil {
		t.Fatalf("update synced device reminders: %v", err)
	}
	if reverted.SyncedDeviceRemindersEnabled {
		t.Fatalf("expected synced device reminders to be disabled again")
	}
}
