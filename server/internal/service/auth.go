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

var ErrInvalidCredentials = errors.New("invalid credentials")

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
func (s *AuthService) Bootstrap(ctx context.Context) error {
	count, err := s.users.Count(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}

	username, password, mustChangePassword := defaultBootstrapUsername, defaultBootstrapPassword, true
	if s.initialUsername != "" && s.initialPassword != "" {
		username, password, mustChangePassword = s.initialUsername, s.initialPassword, false
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash bootstrap password: %w", err)
	}

	if _, err := s.users.Create(ctx, username, string(hash), mustChangePassword); err != nil {
		return fmt.Errorf("create bootstrap user: %w", err)
	}

	return nil
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
	sum := sha256.Sum256([]byte(token))
	hash = hex.EncodeToString(sum[:])

	return token, hash, nil
}
