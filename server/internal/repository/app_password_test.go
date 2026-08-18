package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calich/server/internal/db"
)

func newTestAppPasswordRepository(t *testing.T) (*AppPasswordRepository, *UserRepository) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	return NewAppPasswordRepository(sqlDB), NewUserRepository(sqlDB)
}

func TestAppPasswordRepository_Create(t *testing.T) {
	appPasswords, users := newTestAppPasswordRepository(t)
	ctx := context.Background()

	user, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	created, err := appPasswords.Create(ctx, user.ID, "iPhone", "app-password-hash")
	if err != nil {
		t.Fatalf("create app password: %v", err)
	}

	if created.ID == 0 {
		t.Fatalf("expected non-zero app password id")
	}
	if created.UserID != user.ID {
		t.Fatalf("expected user id %d, got %d", user.ID, created.UserID)
	}
	if created.Label != "iPhone" {
		t.Fatalf("expected label %q, got %q", "iPhone", created.Label)
	}
	if created.Hash != "app-password-hash" {
		t.Fatalf("expected hash %q, got %q", "app-password-hash", created.Hash)
	}
	if created.LastUsedAt != nil {
		t.Fatalf("expected a freshly created app password to have no last_used_at, got %v", created.LastUsedAt)
	}
}

func TestAppPasswordRepository_ListForUser(t *testing.T) {
	appPasswords, users := newTestAppPasswordRepository(t)
	ctx := context.Background()

	userA, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	userB, err := users.Create(ctx, "someone-else", "someone-else@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, err := appPasswords.Create(ctx, userA.ID, "iPhone", "hash-1"); err != nil {
		t.Fatalf("create app password: %v", err)
	}
	if _, err := appPasswords.Create(ctx, userA.ID, "iPad", "hash-2"); err != nil {
		t.Fatalf("create app password: %v", err)
	}
	if _, err := appPasswords.Create(ctx, userB.ID, "Other user's device", "hash-3"); err != nil {
		t.Fatalf("create app password: %v", err)
	}

	list, err := appPasswords.ListForUser(ctx, userA.ID)
	if err != nil {
		t.Fatalf("list app passwords: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 app passwords for user A, got %d", len(list))
	}
	for _, p := range list {
		if p.UserID != userA.ID {
			t.Fatalf("expected all listed app passwords to belong to user A, got user id %d", p.UserID)
		}
	}
}

func TestAppPasswordRepository_ListForUser_Empty(t *testing.T) {
	appPasswords, users := newTestAppPasswordRepository(t)
	ctx := context.Background()

	user, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	list, err := appPasswords.ListForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list app passwords: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no app passwords, got %d", len(list))
	}
}

func TestAppPasswordRepository_Delete(t *testing.T) {
	appPasswords, users := newTestAppPasswordRepository(t)
	ctx := context.Background()

	user, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	created, err := appPasswords.Create(ctx, user.ID, "iPhone", "hash-1")
	if err != nil {
		t.Fatalf("create app password: %v", err)
	}

	if err := appPasswords.Delete(ctx, user.ID, created.ID); err != nil {
		t.Fatalf("delete app password: %v", err)
	}

	list, err := appPasswords.ListForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list app passwords: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no app passwords after delete, got %d", len(list))
	}
}

func TestAppPasswordRepository_Delete_NotFound(t *testing.T) {
	appPasswords, users := newTestAppPasswordRepository(t)
	ctx := context.Background()

	user, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := appPasswords.Delete(ctx, user.ID, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAppPasswordRepository_UpdateLastUsedAt(t *testing.T) {
	appPasswords, users := newTestAppPasswordRepository(t)
	ctx := context.Background()

	user, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	created, err := appPasswords.Create(ctx, user.ID, "iPhone", "hash-1")
	if err != nil {
		t.Fatalf("create app password: %v", err)
	}

	if err := appPasswords.UpdateLastUsedAt(ctx, user.ID, created.ID); err != nil {
		t.Fatalf("update last used at: %v", err)
	}

	list, err := appPasswords.ListForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list app passwords: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 app password, got %d", len(list))
	}
	if list[0].LastUsedAt == nil {
		t.Fatalf("expected last_used_at to be set after UpdateLastUsedAt")
	}
}

func TestAppPasswordRepository_UpdateLastUsedAt_ScopedToOwner(t *testing.T) {
	appPasswords, users := newTestAppPasswordRepository(t)
	ctx := context.Background()

	userA, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	userB, err := users.Create(ctx, "someone-else", "someone-else@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	created, err := appPasswords.Create(ctx, userA.ID, "iPhone", "hash-1")
	if err != nil {
		t.Fatalf("create app password: %v", err)
	}

	// Passing the wrong owner must not stamp another user's app password.
	if err := appPasswords.UpdateLastUsedAt(ctx, userB.ID, created.ID); err != nil {
		t.Fatalf("update last used at: %v", err)
	}

	list, err := appPasswords.ListForUser(ctx, userA.ID)
	if err != nil {
		t.Fatalf("list app passwords: %v", err)
	}
	if len(list) != 1 || list[0].LastUsedAt != nil {
		t.Fatalf("expected user A's app password to be untouched by user B's update, got %+v", list)
	}
}

func TestAppPasswordRepository_Delete_ScopedToOwner(t *testing.T) {
	appPasswords, users := newTestAppPasswordRepository(t)
	ctx := context.Background()

	userA, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	userB, err := users.Create(ctx, "someone-else", "someone-else@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	created, err := appPasswords.Create(ctx, userA.ID, "iPhone", "hash-1")
	if err != nil {
		t.Fatalf("create app password: %v", err)
	}

	if err := appPasswords.Delete(ctx, userB.ID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting another user's app password, got %v", err)
	}

	list, err := appPasswords.ListForUser(ctx, userA.ID)
	if err != nil {
		t.Fatalf("list app passwords: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected user A's app password to survive the other user's delete attempt, got %d remaining", len(list))
	}
}
