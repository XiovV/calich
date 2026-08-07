package service

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

func newTestAppPasswordService(t *testing.T) (svc *AppPasswordService, userID int64) {
	t.Helper()

	svc, userID, _ = newTestAppPasswordServiceWithUsername(t)
	return svc, userID
}

func newTestAppPasswordServiceWithUsername(t *testing.T) (svc *AppPasswordService, userID int64, username string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	user, err := users.Create(context.Background(), "user-a", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	return NewAppPasswordService(repository.NewAppPasswordRepository(sqlDB), users), user.ID, user.Username
}

func TestAppPasswordService_Create(t *testing.T) {
	svc, userID := newTestAppPasswordService(t)
	ctx := context.Background()

	result, err := svc.Create(ctx, userID, "iPhone")
	if err != nil {
		t.Fatalf("create app password: %v", err)
	}

	if result.Secret == "" {
		t.Fatalf("expected a non-empty plaintext secret")
	}
	if result.AppPassword.Label != "iPhone" {
		t.Fatalf("expected label %q, got %q", "iPhone", result.AppPassword.Label)
	}
	if result.AppPassword.Hash == result.Secret {
		t.Fatalf("expected the stored hash to differ from the plaintext secret")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(result.AppPassword.Hash), []byte(result.Secret)); err != nil {
		t.Fatalf("expected the stored hash to verify against the plaintext secret: %v", err)
	}
}

func TestAppPasswordService_Create_RejectsEmptyLabel(t *testing.T) {
	svc, userID := newTestAppPasswordService(t)

	_, err := svc.Create(context.Background(), userID, "   ")
	if !errors.Is(err, ErrInvalidAppPasswordLabel) {
		t.Fatalf("expected ErrInvalidAppPasswordLabel, got %v", err)
	}
}

func TestAppPasswordService_List_NeverIncludesSecretOrHashIsUsableOnlyInternally(t *testing.T) {
	svc, userID := newTestAppPasswordService(t)
	ctx := context.Background()

	first, err := svc.Create(ctx, userID, "iPhone")
	if err != nil {
		t.Fatalf("create app password: %v", err)
	}
	second, err := svc.Create(ctx, userID, "iPad")
	if err != nil {
		t.Fatalf("create app password: %v", err)
	}

	list, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("list app passwords: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 app passwords, got %d", len(list))
	}
	for _, p := range list {
		if p.Hash == first.Secret || p.Hash == second.Secret {
			t.Fatalf("expected list to never expose the plaintext secret")
		}
	}
}

func TestAppPasswordService_Revoke(t *testing.T) {
	svc, userID := newTestAppPasswordService(t)
	ctx := context.Background()

	kept, err := svc.Create(ctx, userID, "iPhone")
	if err != nil {
		t.Fatalf("create app password: %v", err)
	}
	toRevoke, err := svc.Create(ctx, userID, "iPad")
	if err != nil {
		t.Fatalf("create app password: %v", err)
	}

	if err := svc.Revoke(ctx, userID, toRevoke.AppPassword.ID); err != nil {
		t.Fatalf("revoke app password: %v", err)
	}

	list, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("list app passwords: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 remaining app password, got %d", len(list))
	}
	if list[0].ID != kept.AppPassword.ID {
		t.Fatalf("expected the untouched app password to remain, revoke affected the wrong one")
	}
}

func TestAppPasswordService_Revoke_NotFound(t *testing.T) {
	svc, userID := newTestAppPasswordService(t)

	err := svc.Revoke(context.Background(), userID, 999)
	if !errors.Is(err, ErrAppPasswordNotFound) {
		t.Fatalf("expected ErrAppPasswordNotFound, got %v", err)
	}
}

func TestAppPasswordService_Authenticate_Success(t *testing.T) {
	svc, userID, username := newTestAppPasswordServiceWithUsername(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, userID, "iPhone")
	if err != nil {
		t.Fatalf("create app password: %v", err)
	}

	gotUserID, err := svc.Authenticate(ctx, username, created.Secret)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if gotUserID != userID {
		t.Fatalf("expected user id %d, got %d", userID, gotUserID)
	}

	list, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("list app passwords: %v", err)
	}
	if len(list) != 1 || list[0].LastUsedAt == nil {
		t.Fatalf("expected the matched app password's last_used_at to be stamped, got %+v", list)
	}
}

func TestAppPasswordService_Authenticate_RejectsWrongSecret(t *testing.T) {
	svc, userID, username := newTestAppPasswordServiceWithUsername(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, userID, "iPhone"); err != nil {
		t.Fatalf("create app password: %v", err)
	}

	_, err := svc.Authenticate(ctx, username, "not-the-right-secret")
	if !errors.Is(err, ErrInvalidAppPasswordCredentials) {
		t.Fatalf("expected ErrInvalidAppPasswordCredentials, got %v", err)
	}
}

func TestAppPasswordService_Authenticate_RejectsUnknownUsername(t *testing.T) {
	svc, _, _ := newTestAppPasswordServiceWithUsername(t)

	_, err := svc.Authenticate(context.Background(), "no-such-user", "whatever")
	if !errors.Is(err, ErrInvalidAppPasswordCredentials) {
		t.Fatalf("expected ErrInvalidAppPasswordCredentials, got %v", err)
	}
}

func TestAppPasswordService_Authenticate_RejectsDisabledAccount(t *testing.T) {
	svc, userID, username := newTestAppPasswordServiceWithUsername(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, userID, "iPhone")
	if err != nil {
		t.Fatalf("create app password: %v", err)
	}
	if _, err := svc.users.SetDisabled(ctx, userID, true); err != nil {
		t.Fatalf("disable account: %v", err)
	}

	_, err = svc.Authenticate(ctx, username, created.Secret)
	if !errors.Is(err, ErrInvalidAppPasswordCredentials) {
		t.Fatalf("expected ErrInvalidAppPasswordCredentials, got %v", err)
	}
}

func TestAppPasswordService_Authenticate_RejectsRevokedAppPassword(t *testing.T) {
	svc, userID, username := newTestAppPasswordServiceWithUsername(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, userID, "iPhone")
	if err != nil {
		t.Fatalf("create app password: %v", err)
	}
	if err := svc.Revoke(ctx, userID, created.AppPassword.ID); err != nil {
		t.Fatalf("revoke app password: %v", err)
	}

	_, err = svc.Authenticate(ctx, username, created.Secret)
	if !errors.Is(err, ErrInvalidAppPasswordCredentials) {
		t.Fatalf("expected ErrInvalidAppPasswordCredentials for a revoked app password, got %v", err)
	}
}

func TestAppPasswordService_Authenticate_RejectsTheAccountLoginPassword(t *testing.T) {
	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	loginPassword := "the-account-login-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(loginPassword), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash login password: %v", err)
	}

	users := repository.NewUserRepository(sqlDB)
	user, err := users.Create(context.Background(), "user-a", string(hash), false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	svc := NewAppPasswordService(repository.NewAppPasswordRepository(sqlDB), users)
	if _, err := svc.Create(context.Background(), user.ID, "iPhone"); err != nil {
		t.Fatalf("create app password: %v", err)
	}

	// The web login password must never work as a CalDAV app password
	// credential (ADR-0024) — Authenticate only checks app_passwords hashes.
	_, err = svc.Authenticate(context.Background(), user.Username, loginPassword)
	if !errors.Is(err, ErrInvalidAppPasswordCredentials) {
		t.Fatalf("expected the web login password to be rejected, got %v", err)
	}
}
