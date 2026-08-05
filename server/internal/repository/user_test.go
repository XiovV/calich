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
