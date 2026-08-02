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
	"strconv"
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
)

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

	return newUser, true, nil
}

type LoginResult struct {
	AccessToken           string
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
	MustChangePassword    bool
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

	accessToken, err := s.newAccessToken(user.ID)
	if err != nil {
		return LoginResult{}, fmt.Errorf("issue access token: %w", err)
	}

	refreshToken, refreshTokenHash, err := newOpaqueToken()
	if err != nil {
		return LoginResult{}, fmt.Errorf("issue refresh token: %w", err)
	}

	expiresAt := time.Now().Add(refreshTokenTTL)
	if _, err := s.sessions.Create(ctx, user.ID, refreshTokenHash, expiresAt); err != nil {
		return LoginResult{}, fmt.Errorf("create session: %w", err)
	}

	return LoginResult{
		AccessToken:           accessToken,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: expiresAt,
		MustChangePassword:    user.MustChangePassword,
	}, nil
}

// Authenticate validates an access token and returns the user id it was
// issued for. It never touches the database — the token's signature and
// expiry are all that's checked.
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

func (s *AuthService) ChangePassword(ctx context.Context, userID int64, currentPassword, newPassword string) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}

	// A user forced to change their password is always on the fixed, publicly
	// documented bootstrap default (ADR-0010) — verifying it back adds
	// friction without any real security value. Once they're past that (this
	// flag is false), a change requires proving the current password as usual.
	if !user.MustChangePassword {
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
			return ErrInvalidCredentials
		}
	}

	if newPassword == "" {
		return ErrInvalidPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}

	if err := s.users.UpdatePassword(ctx, userID, string(hash)); err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	// Invalidate every outstanding session: a refresh token issued before the
	// password change (e.g. one an attacker had stolen) must stop working.
	if err := s.sessions.DeleteAllForUser(ctx, userID); err != nil {
		return fmt.Errorf("invalidate sessions: %w", err)
	}

	return nil
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
