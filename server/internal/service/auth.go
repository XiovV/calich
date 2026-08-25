// Package service holds business logic that sits above the repository layer.
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/XiovV/calich/server/internal/repository"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidSession     = errors.New("invalid session")
	// ErrInvalidPassword is returned by validatePassword for an empty
	// password, and — since #251 — for one that is non-empty but entirely
	// whitespace, which is no harder to guess than empty.
	ErrInvalidPassword = errors.New("password must not be empty or only whitespace")
	// ErrPasswordTooShort is returned by validatePassword for a non-empty
	// password under minPasswordRunes characters (#247).
	ErrPasswordTooShort = errors.New("password must be at least 8 characters")
	// ErrPasswordTooLong is returned by validatePassword for input over
	// maxPasswordBytes bytes — bcrypt.GenerateFromPassword's own limit,
	// checked explicitly (#241) so a too-long password comes back as a
	// normal validation error instead of surfacing as a generic 500.
	// This text is for logs, not users: the handlers deliberately render it
	// without the byte count, which a user can neither count nor act on.
	ErrPasswordTooLong = errors.New("password must be at most 72 bytes")
	// ErrInvalidEmail is returned when an email fails validateEmail — not a
	// well-formed address, or containing a colon or other whitespace (ADR-0047:
	// this is also the CalDAV Basic auth identifier, and Go's
	// net/http.Request.BasicAuth splits credentials on the first colon).
	ErrInvalidEmail = errors.New("email must be a valid address and must not contain whitespace or a colon")
	// ErrEmailTooLong is returned by validateEmail for an address over
	// maxEmailLength octets (#248).
	ErrEmailTooLong = errors.New("email must be at most 254 characters")
	// ErrInvalidDisplayName is returned when a display name fails validateName —
	// empty (or all whitespace) or longer than maxNameLength, and — since
	// #251 — a name with no printable content (e.g. all zero-width spaces)
	// or one carrying a control character (e.g. NUL).
	ErrInvalidDisplayName = errors.New("name must contain a visible character, must not contain control characters, and must be at most 100 characters")
	// ErrEmailTaken mirrors repository.ErrEmailTaken so handlers only import
	// the service package's sentinels.
	ErrEmailTaken = repository.ErrEmailTaken
	// ErrWorkspaceInviteInvalid is returned for a Workspace Invite (ADR-0044)
	// token that doesn't match any outstanding invite, has expired, or has
	// already been consumed — deliberately one error for all three, since
	// telling them apart would tell an attacker holding a dead token which
	// kind of dead it is.
	ErrWorkspaceInviteInvalid = errors.New("invite is invalid or has expired")
	// ErrWorkspaceInviteEmailMismatch is returned by
	// AcceptWorkspaceInviteExisting when the authenticated caller's own
	// account email doesn't match the invite's exactly (ADR-0044) — an
	// invite for one address must not silently add whoever happens to be
	// logged in when they click it.
	ErrWorkspaceInviteEmailMismatch = errors.New("this invite was issued for a different email address")
	// ErrAlreadyWorkspaceMember mirrors repository.ErrAlreadyMember so
	// handlers only import the service package's sentinels.
	ErrAlreadyWorkspaceMember = repository.ErrAlreadyMember
	// ErrInvalidWeekStart is returned by UpdatePreferences for a Week start
	// outside the date-fns weekStartsOn range (ADR-0039).
	ErrInvalidWeekStart = errors.New("week_start must be between 0 and 6")
	// ErrInvalidDefaultView is returned by UpdatePreferences for a Default
	// view outside day/week/month/year (ADR-0039).
	ErrInvalidDefaultView = errors.New("default_view must be one of day, week, month, year")
	// ErrInvalidTimeFormat is returned by UpdatePreferences for a Time format
	// other than 12h/24h (ADR-0039).
	ErrInvalidTimeFormat = errors.New("time_format must be one of 12h, 24h")
	// ErrInvalidWorkingHours is returned by UpdatePreferences for a Working
	// hours pair that isn't both-set-or-both-null, isn't 0..1439 minutes
	// since midnight, or has start >= end (ADR-0039).
	ErrInvalidWorkingHours = errors.New("working_hours_start and working_hours_end must both be set (0-1439 minutes since midnight, start < end) or both be null")
	// ErrEmailRequired is returned by Register when email is empty — unlike
	// UpdateEmail, where it is optional and clearable, Register requires one
	// since it names who the freshly created Workspace's Owner is.
	ErrEmailRequired = errors.New("email is required")
	// ErrSignupsDisabled is returned by Register for any registration
	// attempt beyond the very first account on the instance while
	// ENABLE_SIGNUPS is false (ADR-0044) — the first account always
	// succeeds regardless, since it's how a fresh instance without
	// INITIAL_EMAIL/INITIAL_PASSWORD set gets bootstrapped.
	ErrSignupsDisabled = errors.New("self-registration is disabled on this instance")
)

// validDefaultViews are the Active views a Default view may seed (ADR-0039).
var validDefaultViews = map[string]bool{
	"day":   true,
	"week":  true,
	"month": true,
	"year":  true,
}

// validTimeFormats are the Time format values a Preference may take (ADR-0039).
var validTimeFormats = map[string]bool{
	"12h": true,
	"24h": true,
}

// maxWorkingHoursMinute is the last valid minute-of-day a Working hours
// bound may hold — 23:59, expressed as minutes since midnight (ADR-0039).
const maxWorkingHoursMinute = 1439

type AuthService struct {
	db               *sql.DB
	users            *repository.UserRepository
	sessions         *repository.SessionRepository
	workspaces       *WorkspaceService
	workspaceInvites *repository.WorkspaceInviteRepository
	calendars        *CalendarService
	attendees        *repository.AttendeeRepository
	jwtSecret        []byte
	initialName      string
	initialEmail     string
	initialPassword  string
	enableSignups    bool
}

func NewAuthService(db *sql.DB, users *repository.UserRepository, sessions *repository.SessionRepository, workspaces *WorkspaceService, workspaceInvites *repository.WorkspaceInviteRepository, calendars *CalendarService, attendees *repository.AttendeeRepository, jwtSecret []byte, initialName, initialEmail, initialPassword string, enableSignups bool) *AuthService {
	return &AuthService{
		db:               db,
		users:            users,
		sessions:         sessions,
		workspaces:       workspaces,
		workspaceInvites: workspaceInvites,
		calendars:        calendars,
		attendees:        attendees,
		jwtSecret:        jwtSecret,
		initialName:      initialName,
		initialEmail:     initialEmail,
		initialPassword:  initialPassword,
		enableSignups:    enableSignups,
	}
}

// Bootstrap creates the first user if none exist yet and INITIAL_EMAIL /
// INITIAL_PASSWORD are both set (INITIAL_NAME defaults to "Admin" if unset —
// see config.Load), owning a freshly created Workspace of their own
// (ADR-0044). There is no fixed fallback credential (ADR-0010, superseded):
// an instance with neither env var set simply has no user until someone
// completes Register — the first-run bootstrap form that collects a name,
// email, and password — which is allowed unconditionally for exactly this
// reason. It returns the resulting sole user either way, along with whether
// this call is what created them — callers that only want to act on a
// genuinely fresh install (e.g. seeding default calendars) should gate on
// that flag rather than re-derive freshness from the user's current state,
// which can drift after bootstrap (e.g. the user deletes their own data
// later).
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

	if s.initialEmail == "" || s.initialPassword == "" {
		return repository.User{}, false, nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(s.initialPassword), bcrypt.DefaultCost)
	if err != nil {
		return repository.User{}, false, fmt.Errorf("hash bootstrap password: %w", err)
	}

	// Folded to lowercase (ADR-0058, #196) like every other path that stores
	// an email — INITIAL_EMAIL is operator-typed and just as likely to carry
	// stray capitals as a Register form.
	newUser, err := s.users.Create(ctx, s.initialName, normalizeEmail(s.initialEmail), string(hash), false)
	if err != nil {
		return repository.User{}, false, fmt.Errorf("create bootstrap user: %w", err)
	}

	if _, err := s.workspaces.CreateForOwner(ctx, newUser.ID, workspaceNameFor(newUser.Name)); err != nil {
		return repository.User{}, false, fmt.Errorf("create bootstrap workspace: %w", err)
	}

	return newUser, true, nil
}

// HasAnyAccounts reports whether the instance has at least one account yet —
// unauthenticated-safe, backing the first-run redirect (#169, ADR-0047): a
// fresh self-hosted instance with zero accounts sends an unauthenticated
// visitor straight to Register instead of Sign-in.
func (s *AuthService) HasAnyAccounts(ctx context.Context) (bool, error) {
	count, err := s.users.Count(ctx)
	if err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	return count > 0, nil
}

// SignupsEnabled reports the raw ENABLE_SIGNUPS setting (ADR-0044) — the
// frontend uses it to decide whether to offer self-registration at all.
// It says nothing about the very-first-account exception Register itself
// grants: HasAnyAccounts is what callers combine it with for that.
func (s *AuthService) SignupsEnabled() bool {
	return s.enableSignups
}

// workspaceNameFor is the default name a Workspace gets when Bootstrap or
// Register creates one automatically, rather than leaving it blank — the
// User can rename it later (ADR-0044).
func workspaceNameFor(name string) string {
	return fmt.Sprintf("%s's Workspace", name)
}

// Register self-registers a new User (ADR-0044): it always succeeds for the
// very first account on the instance — the first-run bootstrap form, an
// alternative to env-configured Bootstrap for an operator who'd rather set
// their name (used as the account's username), email, and password through
// the UI — and otherwise only when ENABLE_SIGNUPS is true. Every successful
// call creates a brand-new Workspace owned by the registrant; it never
// joins an existing one.
//
// The "is this the very first account" count check and every write it gates
// run inside one transaction (via WorkspaceService.WithTx) rather than as
// separate statements: two Register calls racing against a fresh instance
// could otherwise both observe zero existing Users and both end up treated
// as the first account, bypassing ENABLE_SIGNUPS and both being granted
// Admin. SQLite allows only one writer at a time, so the second transaction
// re-reads the count only once the first has committed.
func (s *AuthService) Register(ctx context.Context, name, email, password string) (LoginResult, error) {
	name, err := validateName(name)
	if err != nil {
		return LoginResult{}, err
	}

	email, err = validateEmail(email)
	if err != nil {
		return LoginResult{}, err
	}

	if err := validatePassword(password); err != nil {
		return LoginResult{}, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return LoginResult{}, fmt.Errorf("hash password: %w", err)
	}

	var newUser repository.User
	var newWorkspace repository.Workspace
	err = s.workspaces.WithTx(ctx, func(tx *sql.Tx) error {
		txUsers := s.users.WithTx(tx)

		count, err := txUsers.Count(ctx)
		if err != nil {
			return fmt.Errorf("count users: %w", err)
		}
		if count > 0 && !s.enableSignups {
			return ErrSignupsDisabled
		}

		newUser, err = txUsers.Create(ctx, name, email, string(hash), false)
		if err != nil {
			if errors.Is(err, repository.ErrEmailTaken) {
				return ErrEmailTaken
			}
			return fmt.Errorf("create user: %w", err)
		}

		newWorkspace, err = s.workspaces.createForOwnerTx(ctx, tx, newUser.ID, workspaceNameFor(name))
		if err != nil {
			return fmt.Errorf("create workspace: %w", err)
		}

		return nil
	})
	if err != nil {
		return LoginResult{}, err
	}

	// Seeds the three default Calendars into the Workspace Register just
	// created (#172). Run only after the transaction above has committed —
	// EnsureDefaults resolves Workspace membership through the plain (non-tx)
	// repos, so it needs newWorkspace's membership row to already be visible
	// outside the transaction that wrote it. A failure here can't be rolled
	// back by that already-committed transaction, so it's compensated
	// explicitly: deleting newWorkspace (cascading its membership row and any
	// partially-seeded Calendars) and newUser turns "account exists but
	// wasn't seeded" into "registration failed outright," the one outcome
	// #172 doesn't rule out.
	if err := s.calendars.EnsureDefaults(ctx, newUser.ID, newWorkspace.ID); err != nil {
		if cleanupErr := s.cleanupFailedRegistration(ctx, newUser.ID, newWorkspace.ID); cleanupErr != nil {
			return LoginResult{}, fmt.Errorf("seed default calendars: %w (cleanup also failed: %v)", err, cleanupErr)
		}
		return LoginResult{}, fmt.Errorf("seed default calendars: %w", err)
	}

	tokens, err := s.issueSession(ctx, newUser.ID, newUser.TokenVersion)
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{sessionTokens: tokens, MustChangePassword: false}, nil
}

// cleanupFailedRegistration deletes workspaceID and then userID (#172) — the
// compensating action for a Register that got as far as committing the User
// and their owning Workspace but then failed to seed default Calendars.
// Workspace goes first: workspaces.owner_user_id carries no ON DELETE
// behaviour (a Workspace must never be left pointing at a deleted User), so
// deleting userID first would violate that foreign key. Deleting workspaceID
// cascades its membership row and any Calendars EnsureDefaults managed to
// create before failing.
func (s *AuthService) cleanupFailedRegistration(ctx context.Context, userID, workspaceID int64) error {
	if err := s.workspaces.workspaces.Delete(ctx, workspaceID); err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}
	if err := s.users.Delete(ctx, userID); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

// GetUser returns the user a valid access token was issued for.
func (s *AuthService) GetUser(ctx context.Context, userID int64) (repository.User, error) {
	return s.users.GetByID(ctx, userID)
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

// IsDisabled reports whether the given user is Disabled (ADR-0044), gating
// every route but the self-service account-lifecycle ones via
// httpauth.RequireEnabledUser.
func (s *AuthService) IsDisabled(ctx context.Context, userID int64) (bool, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("get user: %w", err)
	}
	return user.IsDisabled, nil
}

// UpdateEmail changes userID's account email — the login identifier
// (ADR-0047) and the Email-Channel Reminder recipient (ADR-0021). Unlike
// before #169, email is mandatory and can no longer be cleared to unset.
func (s *AuthService) UpdateEmail(ctx context.Context, userID int64, email string) (repository.User, error) {
	email, err := validateEmail(email)
	if err != nil {
		return repository.User{}, err
	}

	user, err := s.users.UpdateEmail(ctx, userID, email)
	if err != nil {
		if errors.Is(err, repository.ErrEmailTaken) {
			return repository.User{}, ErrEmailTaken
		}
		return repository.User{}, fmt.Errorf("update email: %w", err)
	}
	return user, nil
}

// UpdateName renames the caller's own display name (#125, ADR-0047),
// validated by the same rule Create and AccountService.SetName use, so
// self-service rename and Admin rename cannot drift. No current password is
// required — the Access token already proves identity, and a name is not a
// secret, matching UpdateEmail.
func (s *AuthService) UpdateName(ctx context.Context, userID int64, name string) (repository.User, error) {
	name, err := validateName(name)
	if err != nil {
		return repository.User{}, err
	}

	user, err := s.users.UpdateName(ctx, userID, name)
	if err != nil {
		return repository.User{}, fmt.Errorf("update name: %w", err)
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

// WorkingHoursUpdate is the Working hours half of PreferencesUpdate
// (ADR-0039): Start and End are nil together to clear the range, or both set
// to apply it — one nil and the other set is rejected by UpdatePreferences,
// and so is a caller omitting one side of the pair entirely.
type WorkingHoursUpdate struct {
	Start *int
	End   *int
}

// PreferencesUpdate is the partial PATCH /api/auth/preferences body
// (ADR-0039): a nil field is left untouched, so a Week start of 0 (Sunday)
// can be told apart from an absent field. WorkingHours follows the same
// rule at the pair level: nil means neither bound was present in the request.
type PreferencesUpdate struct {
	WeekStart    *int
	DefaultView  *string
	TimeFormat   *string
	WorkingHours *WorkingHoursUpdate
}

// UpdatePreferences applies whichever Preferences are present in update,
// leaving the rest untouched (ADR-0039). Every present field is validated
// before any is written, and the writes themselves run inside one
// transaction (ADR-0018, #261), so a request setting several Preferences at
// once either applies all of them or none — a failure partway through never
// leaves an earlier field committed while a later one is not.
func (s *AuthService) UpdatePreferences(ctx context.Context, userID int64, update PreferencesUpdate) (repository.User, error) {
	if update.WeekStart != nil && (*update.WeekStart < 0 || *update.WeekStart > 6) {
		return repository.User{}, ErrInvalidWeekStart
	}
	if update.DefaultView != nil && !validDefaultViews[*update.DefaultView] {
		return repository.User{}, ErrInvalidDefaultView
	}
	if update.TimeFormat != nil && !validTimeFormats[*update.TimeFormat] {
		return repository.User{}, ErrInvalidTimeFormat
	}
	if update.WorkingHours != nil {
		wh := update.WorkingHours
		if (wh.Start == nil) != (wh.End == nil) {
			return repository.User{}, ErrInvalidWorkingHours
		}
		if wh.Start != nil {
			if *wh.Start < 0 || *wh.Start > maxWorkingHoursMinute || *wh.End < 0 || *wh.End > maxWorkingHoursMinute || *wh.Start >= *wh.End {
				return repository.User{}, ErrInvalidWorkingHours
			}
		}
	}

	err := repository.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		txUsers := s.users.WithTx(tx)

		if update.WeekStart != nil {
			if _, err := txUsers.UpdateWeekStart(ctx, userID, *update.WeekStart); err != nil {
				return fmt.Errorf("update week start preference: %w", err)
			}
		}
		if update.DefaultView != nil {
			if _, err := txUsers.UpdateDefaultView(ctx, userID, *update.DefaultView); err != nil {
				return fmt.Errorf("update default view preference: %w", err)
			}
		}
		if update.TimeFormat != nil {
			if _, err := txUsers.UpdateTimeFormat(ctx, userID, *update.TimeFormat); err != nil {
				return fmt.Errorf("update time format preference: %w", err)
			}
		}
		if update.WorkingHours != nil {
			if _, err := txUsers.UpdateWorkingHours(ctx, userID, update.WorkingHours.Start, update.WorkingHours.End); err != nil {
				return fmt.Errorf("update working hours preference: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return repository.User{}, err
	}

	return s.users.GetByID(ctx, userID)
}
