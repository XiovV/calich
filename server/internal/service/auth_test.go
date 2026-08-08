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

	user, created, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !created {
		t.Fatalf("expected bootstrap to report created=true for a fresh install")
	}
	if user.Username != "admin" {
		t.Fatalf("expected bootstrapped user to be admin, got %q", user.Username)
	}
	if !user.MustChangePassword {
		t.Fatalf("expected default bootstrap user to require a password change")
	}
	if !user.IsAdmin {
		t.Fatalf("expected the bootstrapped account to be the first admin (ADR-0037)")
	}

	if _, err := svc.Login(ctx, "admin", "admin"); err != nil {
		t.Fatalf("expected default admin/admin credentials to work, got: %v", err)
	}
}

func TestBootstrap_UsesEnvCredentialsWhenBothSet(t *testing.T) {
	svc := newTestAuthService(t, "alice", "hunter2")
	ctx := context.Background()

	user, created, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !created {
		t.Fatalf("expected bootstrap to report created=true for a fresh install")
	}
	if user.Username != "alice" {
		t.Fatalf("expected bootstrapped user to be alice, got %q", user.Username)
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

	first, created, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	if !created {
		t.Fatalf("expected the first bootstrap to report created=true")
	}

	countBefore, err := svc.users.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}

	second, created, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if created {
		t.Fatalf("expected a second bootstrap against an existing install to report created=false")
	}
	if second.ID != first.ID {
		t.Fatalf("expected the second bootstrap to return the same user, got %d and %d", first.ID, second.ID)
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
	if _, _, err := svc.Bootstrap(ctx); err != nil {
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
	if _, _, err := svc.Bootstrap(ctx); err != nil {
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
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	_, err := svc.Login(ctx, "nobody", "whatever")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogin_RejectsDisabledAccount(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if _, err := svc.users.SetDisabled(ctx, user.ID, true); err != nil {
		t.Fatalf("disable user: %v", err)
	}

	if _, err := svc.Login(ctx, "admin", "admin"); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("expected ErrAccountDisabled, got %v", err)
	}
}

func TestAuthenticate_RejectsWrongSigningSecret(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()

	other := NewAuthService(svc.users, svc.sessions, []byte("a-different-secret"), "", "")
	if _, _, err := other.Bootstrap(ctx); err != nil {
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
	if _, _, err := svc.Bootstrap(ctx); err != nil {
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

func TestMustChangePassword_ReflectsUserFlag(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	mustChange, err := svc.MustChangePassword(ctx, user.ID)
	if err != nil {
		t.Fatalf("must change password: %v", err)
	}
	if !mustChange {
		t.Fatalf("expected default bootstrap user to require a password change")
	}

	if _, err := svc.ChangePassword(ctx, user.ID, "admin", "a-new-password"); err != nil {
		t.Fatalf("change password: %v", err)
	}

	mustChange, err = svc.MustChangePassword(ctx, user.ID)
	if err != nil {
		t.Fatalf("must change password: %v", err)
	}
	if mustChange {
		t.Fatalf("expected must_change_password to be cleared after changing password")
	}
}

func TestChangePassword_SkipsCurrentPasswordCheckWhileMustChangePassword(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	// The bootstrap default is a publicly documented value — while
	// must_change_password is true, the current password isn't checked at all.
	if _, err := svc.ChangePassword(ctx, user.ID, "this-is-not-the-current-password", "a-new-password"); err != nil {
		t.Fatalf("expected the current password check to be skipped, got %v", err)
	}
}

func TestChangePassword_RequiresCurrentPasswordOnceAlreadyChanged(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	if _, err := svc.ChangePassword(ctx, user.ID, "admin", "first-new-password"); err != nil {
		t.Fatalf("first change password: %v", err)
	}

	_, err = svc.ChangePassword(ctx, user.ID, "wrong-current-password", "second-new-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials once must_change_password is false, got %v", err)
	}
}

func TestChangePassword_RejectsEmptyNewPassword(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	_, err = svc.ChangePassword(ctx, user.ID, "admin", "")
	if !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}
}

func TestUpdateEmail_SetsAValidEmail(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	updated, err := svc.UpdateEmail(ctx, user.ID, "admin@example.com")
	if err != nil {
		t.Fatalf("update email: %v", err)
	}
	if updated.Email == nil || *updated.Email != "admin@example.com" {
		t.Fatalf("expected email to be set, got %+v", updated.Email)
	}
}

func TestUpdateEmail_RejectsAnInvalidAddress(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	_, err = svc.UpdateEmail(ctx, user.ID, "not-an-email")
	if !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("expected ErrInvalidEmail, got %v", err)
	}
}

func TestUpdateEmail_EmptyStringClearsIt(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := svc.UpdateEmail(ctx, user.ID, "admin@example.com"); err != nil {
		t.Fatalf("update email: %v", err)
	}

	cleared, err := svc.UpdateEmail(ctx, user.ID, "")
	if err != nil {
		t.Fatalf("clear email: %v", err)
	}
	if cleared.Email != nil {
		t.Fatalf("expected email to be cleared, got %+v", cleared.Email)
	}
}

func TestBootstrap_FirstUserDefaultsToMondayWeekStart(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if user.WeekStart != 1 {
		t.Fatalf("expected week_start to default to 1 (Monday), got %d", user.WeekStart)
	}
}

func TestUpdatePreferences_SetsWeekStart(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	weekStart := 0
	updated, err := svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{WeekStart: &weekStart})
	if err != nil {
		t.Fatalf("update preferences: %v", err)
	}
	if updated.WeekStart != 0 {
		t.Fatalf("expected week_start 0 (Sunday) to be stored, got %d", updated.WeekStart)
	}
}

func TestUpdatePreferences_RejectsWeekStartOutOfRange(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	weekStart := 7
	_, err = svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{WeekStart: &weekStart})
	if !errors.Is(err, ErrInvalidWeekStart) {
		t.Fatalf("expected ErrInvalidWeekStart, got %v", err)
	}
}

func TestUpdatePreferences_NilFieldLeavesWeekStartUntouched(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	updated, err := svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{})
	if err != nil {
		t.Fatalf("update preferences: %v", err)
	}
	if updated.WeekStart != 1 {
		t.Fatalf("expected week_start to remain at its default of 1, got %d", updated.WeekStart)
	}
}

func TestUpdatePreferences_SetsDefaultView(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	defaultView := "month"
	updated, err := svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{DefaultView: &defaultView})
	if err != nil {
		t.Fatalf("update preferences: %v", err)
	}
	if updated.DefaultView != "month" {
		t.Fatalf("expected default_view \"month\" to be stored, got %q", updated.DefaultView)
	}
}

func TestUpdatePreferences_RejectsInvalidDefaultView(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	defaultView := "fortnight"
	_, err = svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{DefaultView: &defaultView})
	if !errors.Is(err, ErrInvalidDefaultView) {
		t.Fatalf("expected ErrInvalidDefaultView, got %v", err)
	}
}

func TestUpdatePreferences_NilFieldLeavesDefaultViewUntouched(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	updated, err := svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{})
	if err != nil {
		t.Fatalf("update preferences: %v", err)
	}
	if updated.DefaultView != "week" {
		t.Fatalf("expected default_view to remain at its default of \"week\", got %q", updated.DefaultView)
	}
}

func TestUpdatePreferences_SetsWeekStartAndDefaultViewTogether(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	weekStart := 0
	defaultView := "day"
	updated, err := svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{WeekStart: &weekStart, DefaultView: &defaultView})
	if err != nil {
		t.Fatalf("update preferences: %v", err)
	}
	if updated.WeekStart != 0 {
		t.Fatalf("expected week_start 0 to be stored, got %d", updated.WeekStart)
	}
	if updated.DefaultView != "day" {
		t.Fatalf("expected default_view \"day\" to be stored, got %q", updated.DefaultView)
	}
}

func TestUpdateUsername_RenamesTheCaller(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	updated, err := svc.UpdateUsername(ctx, user.ID, "  newname  ")
	if err != nil {
		t.Fatalf("update username: %v", err)
	}
	if updated.Username != "newname" {
		t.Fatalf("expected username newname, got %q", updated.Username)
	}
}

func TestUpdateUsername_RejectsAColon(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	if _, err := svc.UpdateUsername(ctx, user.ID, "ad:min"); !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("expected ErrInvalidUsername, got %v", err)
	}
}

func TestUpdateUsername_DuplicateUsername_ReturnsErrUsernameTaken(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := svc.users.Create(ctx, "bob", "hash", false); err != nil {
		t.Fatalf("create bob: %v", err)
	}

	if _, err := svc.UpdateUsername(ctx, user.ID, "bob"); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}
}

func TestChangePassword_NewPasswordWorksForLogin(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	if _, err := svc.ChangePassword(ctx, user.ID, "admin", "a-new-password"); err != nil {
		t.Fatalf("change password: %v", err)
	}

	if _, err := svc.Login(ctx, "admin", "admin"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected old password to no longer work, got %v", err)
	}

	if _, err := svc.Login(ctx, "admin", "a-new-password"); err != nil {
		t.Fatalf("expected new password to work, got %v", err)
	}
}

func TestChangePassword_InvalidatesExistingSessions(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	login, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if _, err := svc.ChangePassword(ctx, mustUserID(t, svc, "admin"), "admin", "a-new-password"); err != nil {
		t.Fatalf("change password: %v", err)
	}

	if _, err := svc.Refresh(ctx, login.RefreshToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expected the refresh token issued before the password change to be invalidated, got %v", err)
	}
}

// TestChangePassword_ReissuesExactlyOneNewSession pins the fix for #123: the
// old Session must be gone (the pre-change refresh token no longer resolves
// to any Session, not just an expired one) and exactly one new Session must
// exist in its place.
func TestChangePassword_ReissuesExactlyOneNewSession(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	userID := mustUserID(t, svc, "admin")

	login, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	result, err := svc.ChangePassword(ctx, userID, "admin", "a-new-password")
	if err != nil {
		t.Fatalf("change password: %v", err)
	}
	if result.AccessToken == "" {
		t.Fatalf("expected a non-empty access token")
	}
	if result.RefreshToken == "" {
		t.Fatalf("expected a non-empty refresh token")
	}
	if result.RefreshToken == login.RefreshToken {
		t.Fatalf("expected a freshly issued refresh token, got the pre-change one back")
	}

	if _, err := svc.sessions.GetByRefreshTokenHash(ctx, hashToken(login.RefreshToken)); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the pre-change session to be gone, got %v", err)
	}

	session, err := svc.sessions.GetByRefreshTokenHash(ctx, hashToken(result.RefreshToken))
	if err != nil {
		t.Fatalf("expected exactly one new session for the returned refresh token, got %v", err)
	}
	if session.UserID != userID {
		t.Fatalf("expected the new session to belong to the user, got user id %d", session.UserID)
	}
}

func mustUserID(t *testing.T, svc *AuthService, username string) int64 {
	t.Helper()
	user, err := svc.users.GetByUsername(context.Background(), username)
	if err != nil {
		t.Fatalf("get user %q: %v", username, err)
	}
	return user.ID
}

func TestRefresh_ReturnsNewAccessTokenForValidRefreshToken(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	login, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	accessToken, err := svc.Refresh(ctx, login.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if accessToken == "" {
		t.Fatalf("expected a non-empty access token")
	}

	userID, err := svc.Authenticate(ctx, accessToken)
	if err != nil {
		t.Fatalf("authenticate refreshed token: %v", err)
	}

	user, err := svc.users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if userID != user.ID {
		t.Fatalf("expected refreshed token subject %d to match user id %d", userID, user.ID)
	}
}

func TestRefresh_RejectsUnknownToken(t *testing.T) {
	svc := newTestAuthService(t, "", "")

	_, err := svc.Refresh(context.Background(), "not-a-real-refresh-token")
	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expected ErrInvalidSession, got %v", err)
	}
}

func TestRefresh_RejectsExpiredSession(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	refreshToken, refreshTokenHash, err := newOpaqueToken()
	if err != nil {
		t.Fatalf("new opaque token: %v", err)
	}
	if _, err := svc.sessions.Create(ctx, user.ID, refreshTokenHash, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("create expired session: %v", err)
	}

	if _, err := svc.Refresh(ctx, refreshToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expected ErrInvalidSession, got %v", err)
	}
}

func TestRefresh_RejectsDisabledAccount(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	login, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	user, err := svc.users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if _, err := svc.users.SetDisabled(ctx, user.ID, true); err != nil {
		t.Fatalf("disable user: %v", err)
	}

	if _, err := svc.Refresh(ctx, login.RefreshToken); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("expected ErrAccountDisabled, got %v", err)
	}
}

func TestLogout_DeletesSessionSoRefreshNoLongerWorks(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	login, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if err := svc.Logout(ctx, login.RefreshToken); err != nil {
		t.Fatalf("logout: %v", err)
	}

	if _, err := svc.Refresh(ctx, login.RefreshToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expected ErrInvalidSession after logout, got %v", err)
	}
}

func TestLogout_NoopForUnknownToken(t *testing.T) {
	svc := newTestAuthService(t, "", "")

	if err := svc.Logout(context.Background(), "not-a-real-refresh-token"); err != nil {
		t.Fatalf("expected logout to be a no-op for an unknown token, got %v", err)
	}
}
