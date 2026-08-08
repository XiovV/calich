// Package service holds business logic that sits above the repository layer.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/XiovV/calendar/server/internal/repository"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 30 * 24 * time.Hour

	defaultBootstrapUsername = "admin"
	defaultBootstrapPassword = "admin"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidSession     = errors.New("invalid session")
	ErrInvalidPassword    = errors.New("password must not be empty")
	ErrInvalidEmail       = errors.New("email is not a valid address")
	// ErrAccountDisabled is returned by Login and Refresh for a Disabled
	// User (ADR-0037) — two of the three points Disable must be enforced at,
	// the third being CalDAV Basic auth (AppPasswordService.Authenticate).
	ErrAccountDisabled = errors.New("account is disabled")
	// ErrInvalidWeekStart is returned by UpdatePreferences for a Week start
	// outside the date-fns weekStartsOn range (ADR-0039).
	ErrInvalidWeekStart = errors.New("week_start must be between 0 and 6")
	// ErrInvalidDefaultView is returned by UpdatePreferences for a Default
	// view outside day/week/month/year (ADR-0039).
	ErrInvalidDefaultView = errors.New("default_view must be one of day, week, month, year")
)

// validDefaultViews are the Active views a Default view may seed (ADR-0039).
var validDefaultViews = map[string]bool{
	"day":   true,
	"week":  true,
	"month": true,
	"year":  true,
}

type AuthService struct {
	users           *repository.UserRepository
	sessions        *repository.SessionRepository
	jwtSecret       []byte
	initialUsername string
	initialPassword string
}

func NewAuthService(users *repository.UserRepository, sessions *repository.SessionRepository, jwtSecret []byte, initialUsername, initialPassword string) *AuthService {
	return &AuthService{
		users:           users,
		sessions:        sessions,
		jwtSecret:       jwtSecret,
		initialUsername: initialUsername,
		initialPassword: initialPassword,
	}
}

// Bootstrap creates the first user if none exist yet: the env-configured
// initial credentials if both are set, otherwise a fixed admin/admin user
// that must change its password before anything else can be done (ADR-0010).
// It returns the resulting sole user either way, along with whether this
// call is what created them — callers that only want to act on a genuinely
// fresh install (e.g. seeding default calendars) should gate on that flag
// rather than re-derive freshness from the user's current state, which can
// drift after bootstrap (e.g. the user deletes their own data later).
func (s *AuthService) Bootstrap(ctx context.Context) (user repository.User, created bool, err error) {
	count, err := s.users.Count(ctx)
	if err != nil {
		return repository.User{}, false, fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		existing, err := s.users.First(ctx)
		if err != nil {
			return repository.User{}, false, fmt.Errorf("get existing user: %w", err)
		}
		return existing, false, nil
	}

	username, password, mustChangePassword := defaultBootstrapUsername, defaultBootstrapPassword, true
	if s.initialUsername != "" && s.initialPassword != "" {
		username, password, mustChangePassword = s.initialUsername, s.initialPassword, false
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return repository.User{}, false, fmt.Errorf("hash bootstrap password: %w", err)
	}

	newUser, err := s.users.Create(ctx, username, string(hash), mustChangePassword)
	if err != nil {
		return repository.User{}, false, fmt.Errorf("create bootstrap user: %w", err)
	}

	// The bootstrapped account is the first Admin (ADR-0037), so the
	// instance is never unadministrable.
	newUser, err = s.users.SetAdmin(ctx, newUser.ID, true)
	if err != nil {
		return repository.User{}, false, fmt.Errorf("grant bootstrap user admin: %w", err)
	}

	return newUser, true, nil
}

// sessionTokens is the access/refresh token pair issued whenever a Session is
// created — on Login and on a successful ChangePassword (#123), which
// re-issues rather than just invalidating.
type sessionTokens struct {
	AccessToken           string
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
}

// issueSession mints an access token, creates a new Session for a fresh
// opaque refresh token, and returns both.
func (s *AuthService) issueSession(ctx context.Context, userID int64) (sessionTokens, error) {
	accessToken, err := s.newAccessToken(userID)
	if err != nil {
		return sessionTokens{}, fmt.Errorf("issue access token: %w", err)
	}

	refreshToken, refreshTokenHash, err := newOpaqueToken()
	if err != nil {
		return sessionTokens{}, fmt.Errorf("issue refresh token: %w", err)
	}

	expiresAt := time.Now().Add(refreshTokenTTL)
	if _, err := s.sessions.Create(ctx, userID, refreshTokenHash, expiresAt); err != nil {
		return sessionTokens{}, fmt.Errorf("create session: %w", err)
	}

	return sessionTokens{
		AccessToken:           accessToken,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: expiresAt,
	}, nil
}

type LoginResult struct {
	sessionTokens
	MustChangePassword bool
}

// GetUser returns the user a valid access token was issued for.
func (s *AuthService) GetUser(ctx context.Context, userID int64) (repository.User, error) {
	return s.users.GetByID(ctx, userID)
}

func (s *AuthService) Login(ctx context.Context, username, password string) (LoginResult, error) {
	user, err := s.users.GetByUsername(ctx, username)
	if errors.Is(err, repository.ErrNotFound) {
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, fmt.Errorf("get user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}

	if user.IsDisabled {
		return LoginResult{}, ErrAccountDisabled
	}

	tokens, err := s.issueSession(ctx, user.ID)
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{
		sessionTokens:      tokens,
		MustChangePassword: user.MustChangePassword,
	}, nil
}

// Authenticate validates an access token and returns the user id it was
// issued for. It never touches the database — the token's signature and
// expiry are all that's checked.
//
// This is a knowingly accepted gap in Disable (ADR-0037): a Disabled User
// keeps a working access token for up to its accessTokenTTL (15 minutes),
// since closing it would mean a database read on every authenticated
// request. Login, Refresh, and CalDAV Basic auth are the three points that
// do check — see AuthService.Login, AuthService.Refresh, and
// AppPasswordService.Authenticate.
func (s *AuthService) Authenticate(ctx context.Context, accessToken string) (int64, error) {
	claims := &jwt.RegisteredClaims{}

	_, err := jwt.ParseWithClaims(accessToken, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return 0, fmt.Errorf("parse access token: %w", err)
	}

	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse access token subject: %w", err)
	}

	return userID, nil
}

func (s *AuthService) newAccessToken(userID int64) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   formatUserID(userID),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(accessTokenTTL)),
	}

	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
}

func formatUserID(userID int64) string {
	return strconv.FormatInt(userID, 10)
}

// newOpaqueToken returns a random URL-safe token alongside the SHA-256 hash
// that should be persisted in its place — the raw token is never stored.
func newOpaqueToken() (token string, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate random token: %w", err)
	}

	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// MustChangePassword reports whether the given user still needs to change
// their password before using anything but the change-password endpoint.
func (s *AuthService) MustChangePassword(ctx context.Context, userID int64) (bool, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("get user: %w", err)
	}
	return user.MustChangePassword, nil
}

// IsAdmin reports whether the given user holds Admin (ADR-0037), gating
// every account-management endpoint via httpauth.RequireAdmin.
func (s *AuthService) IsAdmin(ctx context.Context, userID int64) (bool, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("get user: %w", err)
	}
	return user.IsAdmin, nil
}

// ChangePasswordResult is the freshly issued Session's tokens (#123) — the
// same shape Login returns, minus MustChangePassword, since a successful
// change always clears that flag.
type ChangePasswordResult = sessionTokens

// ChangePassword deletes every Session the User has and issues a fresh one,
// rather than sparing the caller's — the security property is that *every*
// refresh token issued before the change stops working, including one an
// attacker stole from this device. Re-issuing rather than sparing keeps that
// property while leaving the caller signed in (#123).
func (s *AuthService) ChangePassword(ctx context.Context, userID int64, currentPassword, newPassword string) (ChangePasswordResult, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return ChangePasswordResult{}, fmt.Errorf("get user: %w", err)
	}

	// A user forced to change their password is always on the fixed, publicly
	// documented bootstrap default (ADR-0010) — verifying it back adds
	// friction without any real security value. Once they're past that (this
	// flag is false), a change requires proving the current password as usual.
	if !user.MustChangePassword {
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
			return ChangePasswordResult{}, ErrInvalidCredentials
		}
	}

	if newPassword == "" {
		return ChangePasswordResult{}, ErrInvalidPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return ChangePasswordResult{}, fmt.Errorf("hash new password: %w", err)
	}

	if err := s.users.UpdatePassword(ctx, userID, string(hash)); err != nil {
		return ChangePasswordResult{}, fmt.Errorf("update password: %w", err)
	}

	if err := s.sessions.DeleteAllForUser(ctx, userID); err != nil {
		return ChangePasswordResult{}, fmt.Errorf("invalidate sessions: %w", err)
	}

	return s.issueSession(ctx, userID)
}

// UpdateEmail sets userID's account email — the Email-Channel Reminder
// recipient (ADR-0021). An empty string clears it back to unset.
func (s *AuthService) UpdateEmail(ctx context.Context, userID int64, email string) (repository.User, error) {
	email = strings.TrimSpace(email)
	if email != "" {
		if _, err := mail.ParseAddress(email); err != nil {
			return repository.User{}, ErrInvalidEmail
		}
	}

	user, err := s.users.UpdateEmail(ctx, userID, email)
	if err != nil {
		return repository.User{}, fmt.Errorf("update email: %w", err)
	}
	return user, nil
}

// UpdateUsername renames the caller's own account (#125), validated by the
// same rule Create and AccountService.SetUsername use, so self-service
// rename and Admin rename cannot drift. No current password is required —
// the Access token already proves identity, and a username is not a secret,
// matching UpdateEmail.
func (s *AuthService) UpdateUsername(ctx context.Context, userID int64, username string) (repository.User, error) {
	username, err := validateUsername(username)
	if err != nil {
		return repository.User{}, err
	}

	user, err := s.users.UpdateUsername(ctx, userID, username)
	if err != nil {
		if errors.Is(err, repository.ErrUsernameTaken) {
			return repository.User{}, ErrUsernameTaken
		}
		return repository.User{}, fmt.Errorf("update username: %w", err)
	}
	return user, nil
}

// UpdateSyncedDeviceReminders sets userID's "let my synced devices show
// reminder pop-ups (disable in-app reminder notifications)" preference
// (ADR-0027).
func (s *AuthService) UpdateSyncedDeviceReminders(ctx context.Context, userID int64, enabled bool) (repository.User, error) {
	user, err := s.users.UpdateSyncedDeviceReminders(ctx, userID, enabled)
	if err != nil {
		return repository.User{}, fmt.Errorf("update synced device reminders preference: %w", err)
	}
	return user, nil
}

// PreferencesUpdate is the partial PATCH /api/auth/preferences body
// (ADR-0039): a nil field is left untouched, so a Week start of 0 (Sunday)
// can be told apart from an absent field.
type PreferencesUpdate struct {
	WeekStart   *int
	DefaultView *string
}

// UpdatePreferences applies whichever Preferences are present in update,
// leaving the rest untouched (ADR-0039). Every present field is validated
// before any is written, so a request setting several Preferences at once
// either applies all of them or none.
func (s *AuthService) UpdatePreferences(ctx context.Context, userID int64, update PreferencesUpdate) (repository.User, error) {
	if update.WeekStart != nil && (*update.WeekStart < 0 || *update.WeekStart > 6) {
		return repository.User{}, ErrInvalidWeekStart
	}
	if update.DefaultView != nil && !validDefaultViews[*update.DefaultView] {
		return repository.User{}, ErrInvalidDefaultView
	}

	if update.WeekStart != nil {
		if _, err := s.users.UpdateWeekStart(ctx, userID, *update.WeekStart); err != nil {
			return repository.User{}, fmt.Errorf("update week start preference: %w", err)
		}
	}
	if update.DefaultView != nil {
		if _, err := s.users.UpdateDefaultView(ctx, userID, *update.DefaultView); err != nil {
			return repository.User{}, fmt.Errorf("update default view preference: %w", err)
		}
	}

	return s.users.GetByID(ctx, userID)
}

// Refresh mints a new access token for the session identified by the given
// (raw, unhashed) refresh token, without rotating the refresh token itself.
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (string, error) {
	session, err := s.sessions.GetByRefreshTokenHash(ctx, hashToken(refreshToken))
	if errors.Is(err, repository.ErrNotFound) {
		return "", ErrInvalidSession
	}
	if err != nil {
		return "", fmt.Errorf("get session: %w", err)
	}

	if time.Now().After(session.RefreshTokenExpiresAt) {
		return "", ErrInvalidSession
	}

	user, err := s.users.GetByID(ctx, session.UserID)
	if err != nil {
		return "", fmt.Errorf("get user: %w", err)
	}
	if user.IsDisabled {
		return "", ErrAccountDisabled
	}

	accessToken, err := s.newAccessToken(session.UserID)
	if err != nil {
		return "", fmt.Errorf("issue access token: %w", err)
	}

	return accessToken, nil
}

// Logout invalidates the session for the given (raw, unhashed) refresh token.
// It is a no-op — not an error — if the token doesn't match any session.
func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	session, err := s.sessions.GetByRefreshTokenHash(ctx, hashToken(refreshToken))
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	if err := s.sessions.Delete(ctx, session.ID); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	return nil
}
