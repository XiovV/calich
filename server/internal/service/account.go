package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	"github.com/XiovV/calendar/server/internal/repository"
)

// Delete dispositions for a deleted User's owned Calendars (ADR-0037) —
// there is no default, since guessing wrong either silently destroys a
// shared Calendar or hands its data to someone with no business holding it.
const (
	DispositionTransfer = "transfer"
	DispositionDelete   = "delete"
)

// maxUsernameLength bounds validateUsername (#125). Chosen as a generous
// round number — nothing downstream (the users.username column, CalDAV
// principal paths keyed on id rather than username) imposes a tighter limit.
const maxUsernameLength = 64

var (
	// ErrInvalidUsername is returned when a username fails validateUsername
	// — empty (or all whitespace), containing a colon or other whitespace,
	// or longer than maxUsernameLength.
	ErrInvalidUsername = errors.New("username must not be empty, must not contain whitespace or a colon, and must be at most 64 characters")
	// ErrUsernameTaken mirrors repository.ErrUsernameTaken so handlers only
	// import the service package's sentinels, matching every other service.
	ErrUsernameTaken = repository.ErrUsernameTaken
	// ErrLastAdmin is returned when an operation would leave the instance
	// with no Admin at all (ADR-0037).
	ErrLastAdmin = errors.New("cannot remove the last remaining admin")
	// ErrInvalidDisposition is returned when Delete is called with neither
	// DispositionTransfer nor DispositionDelete — Delete has no default
	// (ADR-0037).
	ErrInvalidDisposition = errors.New(`owned_calendars must be "transfer" or "delete"`)
	// ErrTransferTargetRequired is returned when Delete is called with
	// DispositionTransfer but no transferTo.
	ErrTransferTargetRequired = errors.New("transfer_to is required when owned_calendars is \"transfer\"")
	// ErrTransferTargetNotFound is returned when transferTo doesn't name an
	// existing User.
	ErrTransferTargetNotFound = errors.New("transfer target not found")
	// ErrCannotTransferToSelf is returned when transferTo names the User
	// being deleted — their Calendars can't be reassigned to themselves,
	// since they're about to stop existing.
	ErrCannotTransferToSelf = errors.New("cannot transfer calendars to the user being deleted")
)

// AccountService is account administration (ADR-0037): creating accounts,
// listing them, resetting a password, granting or revoking Admin, renaming,
// disabling, and deleting. It is deliberately separate from data access — an
// Admin's authority here never extends to another User's Calendars or
// Events, which stay behind the Access resolver (ADR-0034) like everyone
// else's.
type AccountService struct {
	db           *sql.DB
	users        *repository.UserRepository
	sessions     *repository.SessionRepository
	calendarRepo *repository.CalendarRepository
	shareRepo    *repository.CalendarShareRepository
	calendars    *CalendarService
	appPasswords *AppPasswordService
}

func NewAccountService(db *sql.DB, users *repository.UserRepository, sessions *repository.SessionRepository, calendarRepo *repository.CalendarRepository, shareRepo *repository.CalendarShareRepository, calendars *CalendarService, appPasswords *AppPasswordService) *AccountService {
	return &AccountService{db: db, users: users, sessions: sessions, calendarRepo: calendarRepo, shareRepo: shareRepo, calendars: calendars, appPasswords: appPasswords}
}

// validateUsername trims username and checks it against the one set of rules
// shared by Create and both rename paths — self (AuthService.UpdateUsername)
// and Admin (AccountService.SetUsername) — so account creation and account
// rename cannot drift (#125). A colon is rejected because Go's
// net/http.Request.BasicAuth splits credentials on the first colon: a
// username containing one could create an account that can never
// authenticate over CalDAV (AppPasswordService.Authenticate).
func validateUsername(username string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > maxUsernameLength {
		return "", ErrInvalidUsername
	}
	if strings.ContainsAny(username, ":") || strings.ContainsFunc(username, unicode.IsSpace) {
		return "", ErrInvalidUsername
	}
	return username, nil
}

// Create makes a new account with username and a temporary password
// (ADR-0037): the account is forced through the same must_change_password
// gate that closes the bootstrap window (ADR-0010), and starts with the
// same default Calendars a bootstrapped account gets rather than an empty
// sidebar.
func (s *AccountService) Create(ctx context.Context, username, tempPassword string) (repository.User, error) {
	username, err := validateUsername(username)
	if err != nil {
		return repository.User{}, err
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

// SetUsername renames userID's account (#125), validated the same way
// Create validates one. The App passwords userID holds stay valid — CalDAV
// auth is keyed by username (AppPasswordService.Authenticate), so every
// device already configured with the old one gets 401 until it's
// reconfigured with the new one. That is what UsernameImpact exists to warn
// an Admin about before calling this; SetUsername itself does not guard
// against it.
func (s *AccountService) SetUsername(ctx context.Context, userID int64, username string) (repository.User, error) {
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

// UsernameImpact reports how many App passwords userID holds — the
// Admin-facing preview a rename UI shows before calling SetUsername, so the
// Admin knows how many of that person's synced devices are about to start
// failing to sync until reconfigured. Callers should only surface this as a
// confirmation when the count is at least one; a fresh account with none
// warrants no such interruption.
func (s *AccountService) UsernameImpact(ctx context.Context, userID int64) (int, error) {
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return 0, fmt.Errorf("get user: %w", err)
	}

	count, err := s.appPasswords.CountForUser(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("count app passwords: %w", err)
	}
	return count, nil
}

// CalendarImpact is one owned Calendar's exposure to a Delete — how many
// Users hold a Share on it, and would therefore lose Access the moment its
// Owner's account (and, under DispositionDelete, the Calendar itself) is
// gone.
type CalendarImpact struct {
	ID         string
	Name       string
	ShareCount int
}

// DeleteImpact is what DeleteImpact reports before an Admin commits to
// deleting userID's account (ADR-0037): every Calendar they own, and how
// many distinct Users, across all of them, hold a Share and would lose
// Access.
type DeleteImpact struct {
	Calendars         []CalendarImpact
	AffectedUserCount int
}

// DeleteImpact reports what deleting userID's account would affect, without
// writing anything — the Admin-facing preview ADR-0037 asks for so "which
// Calendars have Shares and how many Users would lose Access" is known
// before the irreversible DispositionDelete is chosen.
func (s *AccountService) DeleteImpact(ctx context.Context, userID int64) (DeleteImpact, error) {
	calendars, err := s.calendarRepo.ListByUser(ctx, userID)
	if err != nil {
		return DeleteImpact{}, fmt.Errorf("list owned calendars: %w", err)
	}

	impact := DeleteImpact{Calendars: make([]CalendarImpact, 0, len(calendars))}
	affected := map[int64]struct{}{}
	for _, c := range calendars {
		shares, err := s.shareRepo.ListByCalendarWithUsername(ctx, c.ID)
		if err != nil {
			return DeleteImpact{}, fmt.Errorf("list shares for calendar %s: %w", c.ID, err)
		}
		impact.Calendars = append(impact.Calendars, CalendarImpact{ID: c.ID, Name: c.Name, ShareCount: len(shares)})
		for _, share := range shares {
			affected[share.UserID] = struct{}{}
		}
	}
	impact.AffectedUserCount = len(affected)

	return impact, nil
}

// Delete removes userID's account outright, requiring an explicit
// disposition for the Calendars they own (ADR-0037) — there is no default,
// since today's cascade would otherwise take a shared Calendar, including
// Events other people wrote, out from under everyone with a Share, silently,
// at the exact moment nobody is paying attention:
//
//   - DispositionTransfer reassigns every Calendar userID owned to
//     transferTo, keeping their Events (including ones other people wrote)
//     and existing Shares exactly as they were.
//   - DispositionDelete removes userID's Calendars, and with them their
//     Events, via the existing cascade.
//
// Either way, Shares granted *to* userID are removed (cascade on
// calendar_shares.user_id), and deleting the last remaining Admin is
// refused, exactly like disabling one.
func (s *AccountService) Delete(ctx context.Context, userID int64, disposition string, transferTo *int64) error {
	if disposition != DispositionTransfer && disposition != DispositionDelete {
		return ErrInvalidDisposition
	}

	if disposition == DispositionTransfer {
		if transferTo == nil {
			return ErrTransferTargetRequired
		}
		if *transferTo == userID {
			return ErrCannotTransferToSelf
		}
		if _, err := s.users.GetByID(ctx, *transferTo); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return ErrTransferTargetNotFound
			}
			return fmt.Errorf("get transfer target: %w", err)
		}
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}

	if user.IsAdmin {
		count, err := s.users.CountEnabledAdmins(ctx)
		if err != nil {
			return fmt.Errorf("count enabled admins: %w", err)
		}
		if count <= 1 {
			return ErrLastAdmin
		}
	}

	return repository.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if disposition == DispositionTransfer {
			if err := s.calendarRepo.WithTx(tx).TransferOwnership(ctx, userID, *transferTo); err != nil {
				return fmt.Errorf("transfer calendars: %w", err)
			}
		}

		if err := s.users.WithTx(tx).Delete(ctx, userID); err != nil {
			return fmt.Errorf("delete user: %w", err)
		}

		return nil
	})
}
