package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/XiovV/calendar/server/internal/repository"
)

var (
	// ErrInvalidUsername is returned when a username fails basic validation
	// — empty, or all whitespace.
	ErrInvalidUsername = errors.New("username must not be empty")
	// ErrUsernameTaken mirrors repository.ErrUsernameTaken so handlers only
	// import the service package's sentinels, matching every other service.
	ErrUsernameTaken = repository.ErrUsernameTaken
	// ErrLastAdmin is returned when an operation would leave the instance
	// with no Admin at all (ADR-0037).
	ErrLastAdmin = errors.New("cannot remove the last remaining admin")
)

// AccountService is account administration (ADR-0037): creating accounts,
// listing them, resetting a password, and granting or revoking Admin. It is
// deliberately separate from data access — an Admin's authority here never
// extends to another User's Calendars or Events, which stay behind the
// Access resolver (ADR-0034) like everyone else's.
type AccountService struct {
	users     *repository.UserRepository
	sessions  *repository.SessionRepository
	calendars *CalendarService
}

func NewAccountService(users *repository.UserRepository, sessions *repository.SessionRepository, calendars *CalendarService) *AccountService {
	return &AccountService{users: users, sessions: sessions, calendars: calendars}
}

// Create makes a new account with username and a temporary password
// (ADR-0037): the account is forced through the same must_change_password
// gate that closes the bootstrap window (ADR-0010), and starts with the
// same default Calendars a bootstrapped account gets rather than an empty
// sidebar.
func (s *AccountService) Create(ctx context.Context, username, tempPassword string) (repository.User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return repository.User{}, ErrInvalidUsername
	}
	if tempPassword == "" {
		return repository.User{}, ErrInvalidPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(tempPassword), bcrypt.DefaultCost)
	if err != nil {
		return repository.User{}, fmt.Errorf("hash temporary password: %w", err)
	}

	user, err := s.users.Create(ctx, username, string(hash), true)
	if err != nil {
		if errors.Is(err, repository.ErrUsernameTaken) {
			return repository.User{}, ErrUsernameTaken
		}
		return repository.User{}, fmt.Errorf("create user: %w", err)
	}

	if err := s.calendars.EnsureDefaults(ctx, user.ID); err != nil {
		return repository.User{}, fmt.Errorf("seed default calendars: %w", err)
	}

	return user, nil
}

// List returns every account on the instance.
func (s *AccountService) List(ctx context.Context) ([]repository.User, error) {
	users, err := s.users.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return users, nil
}

// ResetPassword sets userID's password to a new temporary one, without
// requiring their current password, and forces the must_change_password
// gate again so the value the Admin typed does not survive their next
// login. Every existing Session is invalidated, mirroring what a
// self-service password change already does.
func (s *AccountService) ResetPassword(ctx context.Context, userID int64, tempPassword string) (repository.User, error) {
	if tempPassword == "" {
		return repository.User{}, ErrInvalidPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(tempPassword), bcrypt.DefaultCost)
	if err != nil {
		return repository.User{}, fmt.Errorf("hash temporary password: %w", err)
	}

	if err := s.users.ResetPassword(ctx, userID, string(hash)); err != nil {
		return repository.User{}, fmt.Errorf("reset password: %w", err)
	}

	if err := s.sessions.DeleteAllForUser(ctx, userID); err != nil {
		return repository.User{}, fmt.Errorf("invalidate sessions: %w", err)
	}

	return s.users.GetByID(ctx, userID)
}

// SetAdmin grants or revokes Admin for userID. Revoking the last remaining
// Admin is refused (ADR-0037) — otherwise the instance becomes
// unadministrable, with nobody left who can create accounts or promote
// anyone back.
func (s *AccountService) SetAdmin(ctx context.Context, userID int64, isAdmin bool) (repository.User, error) {
	if !isAdmin {
		user, err := s.users.GetByID(ctx, userID)
		if err != nil {
			return repository.User{}, fmt.Errorf("get user: %w", err)
		}

		if user.IsAdmin {
			count, err := s.users.CountAdmins(ctx)
			if err != nil {
				return repository.User{}, fmt.Errorf("count admins: %w", err)
			}
			if count <= 1 {
				return repository.User{}, ErrLastAdmin
			}
		}
	}

	user, err := s.users.SetAdmin(ctx, userID, isAdmin)
	if err != nil {
		return repository.User{}, fmt.Errorf("set admin: %w", err)
	}
	return user, nil
}

// SetDisabled disables or re-enables userID's account (ADR-0037). Disabling
// the last remaining Admin is refused, exactly like revoking their Admin —
// otherwise the instance becomes unadministrable. Disabling deletes every
// live Session so the change takes effect immediately rather than waiting
// out an existing session; everything the User owns — Calendars, Events,
// Shares — is left untouched, since Disable is a property of the account,
// never of the data.
func (s *AccountService) SetDisabled(ctx context.Context, userID int64, isDisabled bool) (repository.User, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return repository.User{}, fmt.Errorf("get user: %w", err)
	}

	if isDisabled && user.IsAdmin {
		count, err := s.users.CountEnabledAdmins(ctx)
		if err != nil {
			return repository.User{}, fmt.Errorf("count enabled admins: %w", err)
		}
		if count <= 1 {
			return repository.User{}, ErrLastAdmin
		}
	}

	user, err = s.users.SetDisabled(ctx, userID, isDisabled)
	if err != nil {
		return repository.User{}, fmt.Errorf("set disabled: %w", err)
	}

	if isDisabled {
		if err := s.sessions.DeleteAllForUser(ctx, userID); err != nil {
			return repository.User{}, fmt.Errorf("invalidate sessions: %w", err)
		}
	}

	return user, nil
}
