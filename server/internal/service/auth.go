// Package service holds business logic that sits above the repository layer.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/XiovV/calendar/server/internal/repository"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 30 * 24 * time.Hour
)

// maxUsernameLength bounds validateUsername (#125). Chosen as a generous
// round number — nothing downstream (the users.username column, CalDAV
// principal paths keyed on id rather than username) imposes a tighter limit.
const maxUsernameLength = 64

// inviteTokenTTL is how long a Workspace Invite token is valid for
// (ADR-0044) — shorter than the 30-day refresh token (ADR-0009) because this
// token doesn't extend a Session, it creates one: anyone holding a live link
// can become a Member, or a brand-new User, outright.
const inviteTokenTTL = 7 * 24 * time.Hour

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidSession     = errors.New("invalid session")
	ErrInvalidPassword    = errors.New("password must not be empty")
	ErrInvalidEmail       = errors.New("email is not a valid address")
	// ErrInvalidUsername is returned when a username fails validateUsername
	// — empty (or all whitespace), containing a colon or other whitespace,
	// or longer than maxUsernameLength.
	ErrInvalidUsername = errors.New("username must not be empty, must not contain whitespace or a colon, and must be at most 64 characters")
	// ErrUsernameTaken mirrors repository.ErrUsernameTaken so handlers only
	// import the service package's sentinels.
	ErrUsernameTaken = repository.ErrUsernameTaken
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
	// INITIAL_USERNAME/INITIAL_PASSWORD set gets bootstrapped.
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

// validateUsername trims username and checks it against the one set of rules
// shared by every path that picks or renames one — Register, AuthService.
// UpdateUsername, and AcceptWorkspaceInviteNewAccount — so none of them can
// drift (#125). A colon is rejected because Go's net/http.Request.BasicAuth
// splits credentials on the first colon: a username containing one could
// create an account that can never authenticate over CalDAV
// (AppPasswordService.Authenticate).
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

type AuthService struct {
	users            *repository.UserRepository
	sessions         *repository.SessionRepository
	workspaces       *WorkspaceService
	workspaceInvites *repository.WorkspaceInviteRepository
	jwtSecret        []byte
	initialUsername  string
	initialPassword  string
	enableSignups    bool
}

func NewAuthService(users *repository.UserRepository, sessions *repository.SessionRepository, workspaces *WorkspaceService, workspaceInvites *repository.WorkspaceInviteRepository, jwtSecret []byte, initialUsername, initialPassword string, enableSignups bool) *AuthService {
	return &AuthService{
		users:            users,
		sessions:         sessions,
		workspaces:       workspaces,
		workspaceInvites: workspaceInvites,
		jwtSecret:        jwtSecret,
		initialUsername:  initialUsername,
		initialPassword:  initialPassword,
		enableSignups:    enableSignups,
	}
}

// Bootstrap creates the first user if none exist yet and INITIAL_USERNAME /
// INITIAL_PASSWORD are both set, owning a freshly created Workspace of their
// own (ADR-0044). There is no fixed fallback credential (ADR-0010,
// superseded): an instance with neither env var set simply has no user until
// someone completes Register — the first-run bootstrap form that collects a
// name, email, and password — which is allowed unconditionally for exactly
// this reason. It returns the resulting sole user either way, along with
// whether this call is what created them — callers that only want to act on
// a genuinely fresh install (e.g. seeding default calendars) should gate on
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

	if s.initialUsername == "" || s.initialPassword == "" {
		return repository.User{}, false, nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(s.initialPassword), bcrypt.DefaultCost)
	if err != nil {
		return repository.User{}, false, fmt.Errorf("hash bootstrap password: %w", err)
	}

	newUser, err := s.users.Create(ctx, s.initialUsername, string(hash), false)
	if err != nil {
		return repository.User{}, false, fmt.Errorf("create bootstrap user: %w", err)
	}

	if _, err := s.workspaces.CreateForOwner(ctx, newUser.ID, workspaceNameFor(newUser.Username)); err != nil {
		return repository.User{}, false, fmt.Errorf("create bootstrap workspace: %w", err)
	}

	return newUser, true, nil
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
	name, err := validateUsername(name)
	if err != nil {
		return LoginResult{}, err
	}

	email = strings.TrimSpace(email)
	if email == "" {
		return LoginResult{}, ErrEmailRequired
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return LoginResult{}, ErrInvalidEmail
	}

	if password == "" {
		return LoginResult{}, ErrInvalidPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return LoginResult{}, fmt.Errorf("hash password: %w", err)
	}

	var newUser repository.User
	err = s.workspaces.WithTx(ctx, func(tx *sql.Tx) error {
		txUsers := s.users.WithTx(tx)

		count, err := txUsers.Count(ctx)
		if err != nil {
			return fmt.Errorf("count users: %w", err)
		}
		if count > 0 && !s.enableSignups {
			return ErrSignupsDisabled
		}

		newUser, err = txUsers.Create(ctx, name, string(hash), false)
		if err != nil {
			if errors.Is(err, repository.ErrUsernameTaken) {
				return ErrUsernameTaken
			}
			return fmt.Errorf("create user: %w", err)
		}

		if _, err := txUsers.UpdateEmail(ctx, newUser.ID, email); err != nil {
			return fmt.Errorf("set email: %w", err)
		}

		if _, err := s.workspaces.createForOwnerTx(ctx, tx, newUser.ID, workspaceNameFor(name)); err != nil {
			return fmt.Errorf("create workspace: %w", err)
		}

		return nil
	})
	if err != nil {
		return LoginResult{}, err
	}

	tokens, err := s.issueSession(ctx, newUser.ID)
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{sessionTokens: tokens, MustChangePassword: false}, nil
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
	// IsDisabled is whether the credentials that just authenticated name a
	// Disabled account (ADR-0044). Login still issues a Session either way —
	// there is no instance-wide Admin left to re-activate someone else's
	// account, so a Disabled User must be able to log back in to reach the
	// one action available to them. The frontend gates everything else off
	// this flag exactly as it already does for MustChangePassword, and the
	// server backs that gate with httpauth.RequireEnabledUser.
	IsDisabled bool
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

	tokens, err := s.issueSession(ctx, user.ID)
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{
		sessionTokens:      tokens,
		MustChangePassword: user.MustChangePassword,
		IsDisabled:         user.IsDisabled,
	}, nil
}

// liveWorkspaceInvite resolves token to the WorkspaceInvite it names, the
// check every Workspace-invite operation shares: the token must match an
// outstanding invite and it must not have expired (ADR-0044).
func (s *AuthService) liveWorkspaceInvite(ctx context.Context, token string) (repository.WorkspaceInvite, error) {
	invite, err := s.workspaceInvites.GetByTokenHash(ctx, hashToken(token))
	if errors.Is(err, repository.ErrNotFound) {
		return repository.WorkspaceInvite{}, ErrWorkspaceInviteInvalid
	}
	if err != nil {
		return repository.WorkspaceInvite{}, fmt.Errorf("get workspace invite by token: %w", err)
	}

	if time.Now().After(invite.InviteExpiresAt) {
		return repository.WorkspaceInvite{}, ErrWorkspaceInviteInvalid
	}

	return invite, nil
}

// WorkspaceInvitePreview is what the accept-workspace-invite page shows
// before the invitee does anything (ADR-0044): which Workspace they're
// joining, the email the invite names, and whether that email already
// belongs to an account — deciding whether the page should collect a name
// and password (new account) or just ask the invitee to log in (existing
// account).
type WorkspaceInvitePreview struct {
	WorkspaceName string
	Email         string
	UserExists    bool
}

// PreviewWorkspaceInvite resolves a live Workspace Invite token without
// consuming it (ADR-0044), mirroring PreviewInvite for the account-level
// Invite this replaces.
func (s *AuthService) PreviewWorkspaceInvite(ctx context.Context, token string) (WorkspaceInvitePreview, error) {
	invite, err := s.liveWorkspaceInvite(ctx, token)
	if err != nil {
		return WorkspaceInvitePreview{}, err
	}

	workspace, err := s.workspaces.GetByID(ctx, invite.WorkspaceID)
	if err != nil {
		return WorkspaceInvitePreview{}, fmt.Errorf("get workspace: %w", err)
	}

	_, err = s.users.GetByEmail(ctx, invite.Email)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return WorkspaceInvitePreview{}, fmt.Errorf("check existing user: %w", err)
	}

	return WorkspaceInvitePreview{
		WorkspaceName: workspace.Name,
		Email:         invite.Email,
		UserExists:    err == nil,
	}, nil
}

// liveWorkspaceInviteTx re-resolves invite.ID inside a transaction and
// confirms it's still the same live invite token names — the re-check both
// accept paths need after opening their transaction, since liveWorkspaceInvite
// only proved the invite was live at the time it was first read, not at the
// moment the transaction actually commits.
func liveWorkspaceInviteTx(ctx context.Context, txInvites *repository.WorkspaceInviteRepository, invite repository.WorkspaceInvite, token string) (repository.WorkspaceInvite, error) {
	liveInvite, err := txInvites.GetByID(ctx, invite.ID)
	if errors.Is(err, repository.ErrNotFound) {
		return repository.WorkspaceInvite{}, ErrWorkspaceInviteInvalid
	}
	if err != nil {
		return repository.WorkspaceInvite{}, fmt.Errorf("get workspace invite: %w", err)
	}
	if liveInvite.InviteTokenHash != hashToken(token) || time.Now().After(liveInvite.InviteExpiresAt) {
		return repository.WorkspaceInvite{}, ErrWorkspaceInviteInvalid
	}
	return liveInvite, nil
}

// AcceptWorkspaceInviteNewAccount accepts a live Workspace Invite whose email
// belongs to nobody yet (ADR-0044): it creates a User (name, the invite's
// email, password), adds them as a Member of the inviting Workspace, and
// consumes the invite — all in one transaction — then logs them straight in,
// matching AcceptInvite's shape for the account-level Invite this replaces.
// Fails with ErrWorkspaceInviteInvalid if, by the time the transaction runs,
// a User has already claimed the invite's email — the same kind of race
// Register's first-account count check closes.
func (s *AuthService) AcceptWorkspaceInviteNewAccount(ctx context.Context, token, name, password string) (LoginResult, error) {
	name, err := validateUsername(name)
	if err != nil {
		return LoginResult{}, err
	}
	if password == "" {
		return LoginResult{}, ErrInvalidPassword
	}

	invite, err := s.liveWorkspaceInvite(ctx, token)
	if err != nil {
		return LoginResult{}, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return LoginResult{}, fmt.Errorf("hash password: %w", err)
	}

	var newUser repository.User
	err = s.workspaces.WithTx(ctx, func(tx *sql.Tx) error {
		txUsers := s.users.WithTx(tx)
		txInvites := s.workspaceInvites.WithTx(tx)

		liveInvite, err := liveWorkspaceInviteTx(ctx, txInvites, invite, token)
		if err != nil {
			return err
		}

		if _, err := txUsers.GetByEmail(ctx, liveInvite.Email); err == nil {
			return ErrWorkspaceInviteInvalid
		} else if !errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("check existing user: %w", err)
		}

		newUser, err = txUsers.Create(ctx, name, string(hash), false)
		if err != nil {
			if errors.Is(err, repository.ErrUsernameTaken) {
				return ErrUsernameTaken
			}
			return fmt.Errorf("create user: %w", err)
		}

		if _, err := txUsers.UpdateEmail(ctx, newUser.ID, liveInvite.Email); err != nil {
			return fmt.Errorf("set email: %w", err)
		}

		if err := s.workspaces.AddMemberInTx(ctx, tx, liveInvite.WorkspaceID, newUser.ID, repository.WorkspaceRoleMember); err != nil {
			return fmt.Errorf("add workspace member: %w", err)
		}

		if err := txInvites.Delete(ctx, liveInvite.ID); err != nil {
			return fmt.Errorf("consume workspace invite: %w", err)
		}

		return nil
	})
	if err != nil {
		return LoginResult{}, err
	}

	tokens, err := s.issueSession(ctx, newUser.ID)
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{sessionTokens: tokens, MustChangePassword: false}, nil
}

// AcceptWorkspaceInviteExisting accepts a live Workspace Invite on behalf of
// the already-authenticated userID (ADR-0044): it adds a WorkspaceMember row
// for the inviting Workspace and consumes the invite — no new User row, no
// password step, and every other Workspace userID already belongs to is left
// untouched. Refused with ErrWorkspaceInviteEmailMismatch unless userID's own
// account email matches the invite's exactly, so accepting an invite meant
// for someone else's address isn't possible just by being logged in as
// anybody, and with ErrAlreadyWorkspaceMember if userID already belongs to
// the Workspace.
func (s *AuthService) AcceptWorkspaceInviteExisting(ctx context.Context, userID int64, token string) (repository.Workspace, error) {
	invite, err := s.liveWorkspaceInvite(ctx, token)
	if err != nil {
		return repository.Workspace{}, err
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return repository.Workspace{}, fmt.Errorf("get user: %w", err)
	}
	if user.Email == nil || *user.Email != invite.Email {
		return repository.Workspace{}, ErrWorkspaceInviteEmailMismatch
	}

	err = s.workspaces.WithTx(ctx, func(tx *sql.Tx) error {
		txInvites := s.workspaceInvites.WithTx(tx)

		liveInvite, err := liveWorkspaceInviteTx(ctx, txInvites, invite, token)
		if err != nil {
			return err
		}

		if err := s.workspaces.AddMemberInTx(ctx, tx, liveInvite.WorkspaceID, userID, repository.WorkspaceRoleMember); err != nil {
			if errors.Is(err, repository.ErrAlreadyMember) {
				return ErrAlreadyWorkspaceMember
			}
			return fmt.Errorf("add workspace member: %w", err)
		}

		if err := txInvites.Delete(ctx, liveInvite.ID); err != nil {
			return fmt.Errorf("consume workspace invite: %w", err)
		}

		return nil
	})
	if err != nil {
		return repository.Workspace{}, err
	}

	return s.workspaces.GetByID(ctx, invite.WorkspaceID)
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
// before any is written, so a request setting several Preferences at once
// either applies all of them or none.
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
	if update.TimeFormat != nil {
		if _, err := s.users.UpdateTimeFormat(ctx, userID, *update.TimeFormat); err != nil {
			return repository.User{}, fmt.Errorf("update time format preference: %w", err)
		}
	}
	if update.WorkingHours != nil {
		if _, err := s.users.UpdateWorkingHours(ctx, userID, update.WorkingHours.Start, update.WorkingHours.End); err != nil {
			return repository.User{}, fmt.Errorf("update working hours preference: %w", err)
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

	// Unlike Login, Refresh doesn't need to look the user up at all — a
	// Disabled account still gets a fresh access token here (ADR-0044), the
	// same way it still gets a Session from Login, so a page reload doesn't
	// strand a Disabled User short of the one action available to them.
	// httpauth.RequireEnabledUser is what actually closes off every other
	// route.
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
