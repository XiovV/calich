package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/XiovV/calendar/server/internal/repository"
)

var (
	ErrInvalidAppPasswordLabel       = errors.New("label must not be empty")
	ErrAppPasswordNotFound           = errors.New("app password not found")
	ErrInvalidAppPasswordCredentials = errors.New("invalid app password credentials")
)

type AppPasswordService struct {
	appPasswords *repository.AppPasswordRepository
	users        *repository.UserRepository
}

func NewAppPasswordService(appPasswords *repository.AppPasswordRepository, users *repository.UserRepository) *AppPasswordService {
	return &AppPasswordService{appPasswords: appPasswords, users: users}
}

// CreateResult carries the plaintext secret, which is only ever available at
// creation time — it isn't derivable from the stored hash afterwards, and
// nothing else in this service returns it.
type CreateResult struct {
	AppPassword repository.AppPassword
	Secret      string
}

// Create generates a new random app password for userID under the given
// label, and returns it alongside the plaintext secret shown to the user
// exactly once (ADR-0024).
func (s *AppPasswordService) Create(ctx context.Context, userID int64, label string) (CreateResult, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return CreateResult{}, ErrInvalidAppPasswordLabel
	}

	secret, err := newAppPasswordSecret()
	if err != nil {
		return CreateResult{}, fmt.Errorf("generate app password secret: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return CreateResult{}, fmt.Errorf("hash app password secret: %w", err)
	}

	appPassword, err := s.appPasswords.Create(ctx, userID, label, string(hash))
	if err != nil {
		return CreateResult{}, fmt.Errorf("create app password: %w", err)
	}

	return CreateResult{AppPassword: appPassword, Secret: secret}, nil
}

// List returns userID's app passwords, never including the secret.
func (s *AppPasswordService) List(ctx context.Context, userID int64) ([]repository.AppPassword, error) {
	appPasswords, err := s.appPasswords.ListForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list app passwords: %w", err)
	}
	return appPasswords, nil
}

// Revoke deletes userID's app password with the given id. Revoking one app
// password never affects the others or the web login (ADR-0024).
func (s *AppPasswordService) Revoke(ctx context.Context, userID, id int64) error {
	if err := s.appPasswords.Delete(ctx, userID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrAppPasswordNotFound
		}
		return fmt.Errorf("revoke app password: %w", err)
	}
	return nil
}

// Authenticate validates HTTP Basic credentials for CalDAV (ADR-0024): the
// username is the account's login username, the password must match one of
// that user's App passwords — never the account's own login password, which
// this never checks. On success it stamps the matched App password's
// last_used_at and returns the user id.
func (s *AppPasswordService) Authenticate(ctx context.Context, username, password string) (int64, error) {
	user, err := s.users.GetByUsername(ctx, username)
	if errors.Is(err, repository.ErrNotFound) {
		return 0, ErrInvalidAppPasswordCredentials
	}
	if err != nil {
		return 0, fmt.Errorf("get user: %w", err)
	}

	appPasswords, err := s.appPasswords.ListForUser(ctx, user.ID)
	if err != nil {
		return 0, fmt.Errorf("list app passwords: %w", err)
	}

	for _, appPassword := range appPasswords {
		if bcrypt.CompareHashAndPassword([]byte(appPassword.Hash), []byte(password)) != nil {
			continue
		}

		if err := s.appPasswords.UpdateLastUsedAt(ctx, user.ID, appPassword.ID); err != nil {
			return 0, fmt.Errorf("update app password last used at: %w", err)
		}
		return user.ID, nil
	}

	return 0, ErrInvalidAppPasswordCredentials
}

// newAppPasswordSecret returns a random URL-safe secret shown to the user
// once and never persisted in the clear — only its bcrypt hash is stored.
func newAppPasswordSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate random secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
