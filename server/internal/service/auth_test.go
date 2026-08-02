package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

func newTestAuthService(t *testing.T, initialUsername, initialPassword string) *AuthService {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)

	return NewAuthService(users, sessions, []byte("test-secret"), initialUsername, initialPassword)
}

func TestBootstrap_CreatesDefaultAdminWhenNoUsersAndNoEnvVars(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()

	if err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get bootstrapped user: %v", err)
	}
	if !user.MustChangePassword {
		t.Fatalf("expected default bootstrap user to require a password change")
	}

	if _, err := svc.Login(ctx, "admin", "admin"); err != nil {
		t.Fatalf("expected default admin/admin credentials to work, got: %v", err)
	}
}

func TestBootstrap_UsesEnvCredentialsWhenBothSet(t *testing.T) {
	svc := newTestAuthService(t, "alice", "hunter2")
	ctx := context.Background()

	if err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("get bootstrapped user: %v", err)
	}
	if user.MustChangePassword {
		t.Fatalf("expected env-configured bootstrap user to skip forced password change")
	}

	result, err := svc.Login(ctx, "alice", "hunter2")
	if err != nil {
		t.Fatalf("expected env credentials to work, got: %v", err)
	}
	if result.MustChangePassword {
		t.Fatalf("expected login result to report must_change_password=false")
	}
}

func TestBootstrap_NoopWhenUsersExist(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()

	if err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}

	countBefore, err := svc.users.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}

	if err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}

	countAfter, err := svc.users.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}

	if countBefore != countAfter {
		t.Fatalf("expected bootstrap to be a no-op when users already exist, count went from %d to %d", countBefore, countAfter)
	}
}

func TestLogin_Success(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	if err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	result, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if result.AccessToken == "" {
		t.Fatalf("expected a non-empty access token")
	}
	if result.RefreshToken == "" {
		t.Fatalf("expected a non-empty refresh token")
	}
	if !result.RefreshTokenExpiresAt.After(time.Now()) {
		t.Fatalf("expected refresh token expiry to be in the future")
	}
	if !result.MustChangePassword {
		t.Fatalf("expected must_change_password to be true for the default bootstrap user")
	}

	userID, err := svc.Authenticate(ctx, result.AccessToken)
	if err != nil {
		t.Fatalf("authenticate issued access token: %v", err)
	}

	user, err := svc.users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if userID != user.ID {
		t.Fatalf("expected access token subject %d to match user id %d", userID, user.ID)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	if err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	_, err := svc.Login(ctx, "admin", "wrong-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogin_UnknownUsername(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	if err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	_, err := svc.Login(ctx, "nobody", "whatever")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestAuthenticate_RejectsWrongSigningSecret(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()

	other := NewAuthService(svc.users, svc.sessions, []byte("a-different-secret"), "", "")
	if err := other.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	result, err := other.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if _, err := svc.Authenticate(ctx, result.AccessToken); err == nil {
		t.Fatalf("expected an error authenticating a token signed with a different secret")
	}
}

func TestAuthenticate_RejectsExpiredToken(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	if err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	expired := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   formatUserID(user.ID),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Hour)),
	})
	signed, err := expired.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}

	if _, err := svc.Authenticate(ctx, signed); err == nil {
		t.Fatalf("expected an error authenticating an expired token")
	}
}

func TestAuthenticate_RejectsGarbage(t *testing.T) {
	svc := newTestAuthService(t, "", "")

	if _, err := svc.Authenticate(context.Background(), "not-a-real-token"); err == nil {
		t.Fatalf("expected an error authenticating a malformed token")
	}
}
