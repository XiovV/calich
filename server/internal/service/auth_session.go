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

	"github.com/XiovV/calich/server/internal/repository"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 30 * 24 * time.Hour
)

// sessionTokens is the access/refresh token pair issued whenever a Session is
// created — on Login and on a successful ChangePassword (#123), which
// re-issues rather than just invalidating.
type sessionTokens struct {
	AccessToken           string
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
}

// issueSession mints an access token, creates a new Session for a fresh
// opaque refresh token, and returns both. tokenVersion is embedded in the
// access token's "tv" claim (#242, ADR-0071) — the caller passes in the
// value from a User row it already has (Create, GetByEmail, or
// UpdatePassword's return), rather than issueSession re-fetching it, so
// issuing a session never costs more than one extra read beyond whatever the
// caller already did.
func (s *AuthService) issueSession(ctx context.Context, userID, tokenVersion int64) (sessionTokens, error) {
	accessToken, err := s.newAccessToken(userID, tokenVersion)
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
	// IsDisabled is whether the credentials that just authenticated name a
	// Disabled account (ADR-0044). Login still issues a Session either way —
	// there is no instance-wide Admin left to re-activate someone else's
	// account, so a Disabled User must be able to log back in to reach the
	// one action available to them. The frontend gates everything else off
	// this flag exactly as it already does for MustChangePassword, and the
	// server backs that gate with httpauth.RequireEnabledUser.
	IsDisabled bool
}

func (s *AuthService) Login(ctx context.Context, email, password string) (LoginResult, error) {
	user, err := s.users.GetByEmail(ctx, email)
	if errors.Is(err, repository.ErrNotFound) {
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, fmt.Errorf("get user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}

	tokens, err := s.issueSession(ctx, user.ID, user.TokenVersion)
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{
		sessionTokens:      tokens,
		MustChangePassword: user.MustChangePassword,
		IsDisabled:         user.IsDisabled,
	}, nil
}

// accessTokenClaims is the JWT payload AuthService mints and validates for
// bearer access tokens. TokenVersion pins the token to the account's
// token_version at mint time (#242, ADR-0071); AuthService.Authenticate
// rejects a token whose TokenVersion doesn't match the account's current
// one.
type accessTokenClaims struct {
	jwt.RegisteredClaims
	TokenVersion int64 `json:"tv"`
}

// Authenticate validates an access token, returning the user id it was
// issued for. Beyond the token's own signature and expiry, it makes one
// check against the database: the token's "tv" claim must match the
// account's current token_version (#242, ADR-0071) — otherwise a token
// minted before a password change would keep working for up to its full
// accessTokenTTL (15 minutes) even though ChangePassword already revoked
// every refresh token, which is precisely the window an attacker who
// prompted the change by stealing the old password would still have.
//
// This closes that gap only for a password change. Disable (ADR-0037) still
// knowingly accepts it: a Disabled User keeps a working access token for up
// to accessTokenTTL, since httpauth.RequireEnabledUser already closes every
// route but the self-service ones behind its own database read on the same
// request. Login, Refresh, and CalDAV Basic auth are the other points that
// check the database directly — see AuthService.Login, AuthService.Refresh,
// and AppPasswordService.Authenticate.
func (s *AuthService) Authenticate(ctx context.Context, accessToken string) (int64, error) {
	claims := &accessTokenClaims{}

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

	tokenVersion, err := s.users.GetTokenVersion(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("get token version: %w", err)
	}
	if claims.TokenVersion != tokenVersion {
		return 0, ErrInvalidSession
	}

	return userID, nil
}

func (s *AuthService) newAccessToken(userID, tokenVersion int64) (string, error) {
	now := time.Now()
	claims := accessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   formatUserID(userID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(accessTokenTTL)),
		},
		TokenVersion: tokenVersion,
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

	if err := validatePassword(newPassword); err != nil {
		return ChangePasswordResult{}, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return ChangePasswordResult{}, fmt.Errorf("hash new password: %w", err)
	}

	updatedUser, err := s.users.UpdatePassword(ctx, userID, string(hash))
	if err != nil {
		return ChangePasswordResult{}, fmt.Errorf("update password: %w", err)
	}

	if err := s.sessions.DeleteAllForUser(ctx, userID); err != nil {
		return ChangePasswordResult{}, fmt.Errorf("invalidate sessions: %w", err)
	}

	return s.issueSession(ctx, userID, updatedUser.TokenVersion)
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

	// Unlike Login, Refresh doesn't check is_disabled at all — a Disabled
	// account still gets a fresh access token here (ADR-0044), the same way
	// it still gets a Session from Login, so a page reload doesn't strand a
	// Disabled User short of the one action available to them.
	// httpauth.RequireEnabledUser is what actually closes off every other
	// route. It does still need token_version (#242, ADR-0071): a live
	// Session here proves no password change has happened since it was
	// created (ChangePassword deletes every Session), but the new access
	// token still has to carry that unchanged value to authenticate.
	tokenVersion, err := s.users.GetTokenVersion(ctx, session.UserID)
	if err != nil {
		return "", fmt.Errorf("get token version: %w", err)
	}

	accessToken, err := s.newAccessToken(session.UserID, tokenVersion)
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
