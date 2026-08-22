package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/XiovV/calich/server/internal/apptest"
	"github.com/XiovV/calich/server/internal/repository"
)

func newTestAuthService(t *testing.T, initialUsername, initialPassword string) *AuthService {
	t.Helper()
	return newTestAuthServiceWithSignups(t, initialUsername, initialPassword, false)
}

// newTestAuthServiceWithSignups keeps its historical (initialUsername,
// initialPassword) parameter names so the dozens of call sites below don't
// need touching for #169/ADR-0047 — initialUsername now seeds both the
// bootstrap account's Name and, deriving a deterministic address from it,
// its Email.
func newTestAuthServiceWithSignups(t *testing.T, initialUsername, initialPassword string, enableSignups bool) *AuthService {
	t.Helper()

	initialEmail := ""
	if initialUsername != "" {
		initialEmail = initialUsername + "@example.com"
	}

	cfg := apptest.Config(t)
	cfg.InitialName = initialUsername
	cfg.InitialEmail = initialEmail
	cfg.InitialPassword = initialPassword
	cfg.EnableSignups = enableSignups

	// The signing secret is pinned rather than minted, so the expired-token
	// test below can sign one AuthService will accept as authentic.
	return newTestGraphWithConfig(t, cfg, WithJWTSecret([]byte("test-secret"))).Auth
}

// mustSeedUserRequiringPasswordChange inserts a User directly via the
// repository, bypassing both Bootstrap and Register — neither of which ever
// produces a User with must_change_password set anymore (ADR-0044 retires
// the fixed bootstrap default that used to). Used only by tests exercising
// AuthService's must-change-password mechanics themselves.
func mustSeedUserRequiringPasswordChange(t *testing.T, svc *AuthService, ctx context.Context, username, password string) repository.User {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	user, err := svc.users.Create(ctx, username, username+"@example.com", string(hash), true)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return user
}

// TestBootstrap_NoopWhenNoEnvVars asserts ADR-0044's replacement for the
// retired fixed admin/admin default (ADR-0010): with neither
// INITIAL_USERNAME nor INITIAL_PASSWORD set, Bootstrap leaves a fresh
// install with no User at all, rather than falling back to a known
// credential — the instance waits for Register (the first-run bootstrap
// form) instead.
func TestBootstrap_NoopWhenNoEnvVars(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()

	user, created, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if created {
		t.Fatalf("expected bootstrap to report created=false with no env vars set")
	}
	if user != (repository.User{}) {
		t.Fatalf("expected a zero-value user, got %+v", user)
	}

	count, err := svc.users.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no user to have been created, got count %d", count)
	}
}

// TestBootstrap_CreatesWorkspaceOwnedByTheBootstrappedUser covers #153's
// acceptance criterion that Bootstrap's env-credentialed path creates a
// Workspace owned by the resulting User (ADR-0044).
func TestBootstrap_CreatesWorkspaceOwnedByTheBootstrappedUser(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()

	user, created, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !created {
		t.Fatalf("expected bootstrap to report created=true for a fresh install")
	}

	workspaces, err := svc.workspaces.ListForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list workspaces: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("expected the bootstrapped user to belong to exactly one workspace, got %d", len(workspaces))
	}
	if workspaces[0].OwnerUserID != user.ID {
		t.Fatalf("expected the bootstrapped user to own their workspace, owner is %d", workspaces[0].OwnerUserID)
	}
}

func TestBootstrap_UsesEnvCredentialsWhenBothSet(t *testing.T) {
	svc := newTestAuthService(t, "alice", "hunter22")
	ctx := context.Background()

	user, created, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !created {
		t.Fatalf("expected bootstrap to report created=true for a fresh install")
	}
	if user.Name != "alice" {
		t.Fatalf("expected bootstrapped user to be alice, got %q", user.Name)
	}
	if user.MustChangePassword {
		t.Fatalf("expected env-configured bootstrap user to skip forced password change")
	}

	result, err := svc.Login(ctx, "alice@example.com", "hunter22")
	if err != nil {
		t.Fatalf("expected env credentials to work, got: %v", err)
	}
	if result.MustChangePassword {
		t.Fatalf("expected login result to report must_change_password=false")
	}
}

// TestBootstrap_FoldsInitialEmailCase covers #196 (ADR-0058): INITIAL_EMAIL
// is operator-typed like any other email input, so a mixed-case value must
// be folded to lowercase on write, the same as Register and UpdateEmail.
func TestBootstrap_FoldsInitialEmailCase(t *testing.T) {
	cfg := apptest.Config(t)
	cfg.InitialName = "Admin"
	cfg.InitialEmail = "Admin@Example.com"
	cfg.InitialPassword = "hunter22"
	svc := newTestGraphWithConfig(t, cfg).Auth

	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if user.Email != "admin@example.com" {
		t.Fatalf("expected bootstrapped email to be folded to lowercase, got %q", user.Email)
	}

	if _, err := svc.Login(ctx, "admin@example.com", "hunter22"); err != nil {
		t.Fatalf("expected login with the folded email to work, got: %v", err)
	}
}

func TestBootstrap_NoopWhenUsersExist(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
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

// TestRegister_ConcurrentFirstRegistrations_OnlyOneSucceeds guards against a
// TOCTOU race: two Register calls hitting a fresh instance at the same time
// must not both observe zero existing Users and both end up treated as the
// first account (bypassing ENABLE_SIGNUPS) — see the transaction Register
// wraps its count check in.
func TestRegister_ConcurrentFirstRegistrations_OnlyOneSucceeds(t *testing.T) {
	svc := newTestAuthServiceWithSignups(t, "", "", false)
	ctx := context.Background()

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i, username := range []string{"alice", "bob"} {
		wg.Add(1)
		go func(i int, username string) {
			defer wg.Done()
			_, err := svc.Register(ctx, username, username+"@example.com", "hunter22")
			results[i] = err
		}(i, username)
	}
	wg.Wait()

	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrSignupsDisabled) {
			t.Fatalf("expected either success or ErrSignupsDisabled, got %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one of the two concurrent registrations to succeed as the first account, got %d", successes)
	}

	count, err := svc.users.Count(ctx)
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one account to have been created, got %d", count)
	}
}

// TestRegister_FirstAccountSucceedsEvenWithSignupsDisabled covers #153's
// acceptance criterion that ENABLE_SIGNUPS=false rejects registration after
// the first account, but never the very first one — Register is a web-based
// alternative to env-configured Bootstrap for exactly this reason.
func TestRegister_FirstAccountSucceedsEvenWithSignupsDisabled(t *testing.T) {
	svc := newTestAuthServiceWithSignups(t, "", "", false)
	ctx := context.Background()

	result, err := svc.Register(ctx, "alice", "alice@example.com", "hunter22")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if result.AccessToken == "" {
		t.Fatalf("expected a non-empty access token")
	}

	user, err := svc.users.GetByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	workspaces, err := svc.workspaces.ListForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list workspaces: %v", err)
	}
	if len(workspaces) != 1 || workspaces[0].OwnerUserID != user.ID {
		t.Fatalf("expected the registrant to own exactly one fresh workspace, got %+v", workspaces)
	}
}

// TestSignupsEnabled_ReflectsConfig covers #235: the frontend's setup-status
// call needs the raw ENABLE_SIGNUPS value, independent of HasAnyAccounts, to
// decide whether to offer registration once an account already exists.
func TestSignupsEnabled_ReflectsConfig(t *testing.T) {
	if newTestAuthServiceWithSignups(t, "", "", false).SignupsEnabled() {
		t.Fatalf("expected SignupsEnabled to be false")
	}
	if !newTestAuthServiceWithSignups(t, "", "", true).SignupsEnabled() {
		t.Fatalf("expected SignupsEnabled to be true")
	}
}

// TestRegister_SignupsDisabled_BlocksASecondRegistration covers #153's
// acceptance criterion that ENABLE_SIGNUPS=false rejects any registration
// attempt beyond the first account.
func TestRegister_SignupsDisabled_BlocksASecondRegistration(t *testing.T) {
	svc := newTestAuthServiceWithSignups(t, "", "", false)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "alice", "alice@example.com", "hunter22"); err != nil {
		t.Fatalf("register alice: %v", err)
	}

	if _, err := svc.Register(ctx, "bob", "bob@example.com", "hunter22"); !errors.Is(err, ErrSignupsDisabled) {
		t.Fatalf("expected ErrSignupsDisabled, got %v", err)
	}
}

// TestRegister_SignupsEnabled_AlwaysCreatesAFreshWorkspace covers #153's
// acceptance criterion that ENABLE_SIGNUPS=true self-registration always
// creates a brand-new Workspace owned by the registrant, never joining an
// existing one.
func TestRegister_SignupsEnabled_AlwaysCreatesAFreshWorkspace(t *testing.T) {
	svc := newTestAuthServiceWithSignups(t, "", "", true)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "alice", "alice@example.com", "hunter22"); err != nil {
		t.Fatalf("register alice: %v", err)
	}
	if _, err := svc.Register(ctx, "bob", "bob@example.com", "hunter22"); err != nil {
		t.Fatalf("register bob: %v", err)
	}

	alice, err := svc.users.GetByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("get alice: %v", err)
	}
	bob, err := svc.users.GetByEmail(ctx, "bob@example.com")
	if err != nil {
		t.Fatalf("get bob: %v", err)
	}

	aliceWorkspaces, err := svc.workspaces.ListForUser(ctx, alice.ID)
	if err != nil {
		t.Fatalf("list alice's workspaces: %v", err)
	}
	bobWorkspaces, err := svc.workspaces.ListForUser(ctx, bob.ID)
	if err != nil {
		t.Fatalf("list bob's workspaces: %v", err)
	}
	if len(aliceWorkspaces) != 1 || len(bobWorkspaces) != 1 {
		t.Fatalf("expected each registrant to own exactly one workspace, got alice=%d bob=%d", len(aliceWorkspaces), len(bobWorkspaces))
	}
	if aliceWorkspaces[0].ID == bobWorkspaces[0].ID {
		t.Fatalf("expected bob to get a brand-new workspace rather than joining alice's")
	}
	if bobWorkspaces[0].OwnerUserID != bob.ID {
		t.Fatalf("expected bob to own his own workspace, owner is %d", bobWorkspaces[0].OwnerUserID)
	}
}

// TestRegister_SeedsDefaultCalendars covers #172: self-signup registration
// must leave the new User with the three default Calendars, seeded into the
// fresh Workspace Register creates for them.
func TestRegister_SeedsDefaultCalendars(t *testing.T) {
	svc := newTestAuthServiceWithSignups(t, "", "", true)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "alice", "alice@example.com", "hunter22"); err != nil {
		t.Fatalf("register alice: %v", err)
	}

	alice, err := svc.users.GetByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("get alice: %v", err)
	}

	calendars, err := svc.calendars.List(ctx, alice.ID)
	if err != nil {
		t.Fatalf("list alice's calendars: %v", err)
	}

	want := map[string]bool{"Personal": true, "Work": true, "Family": true}
	if len(calendars) != len(want) {
		t.Fatalf("expected %d default calendars, got %d", len(want), len(calendars))
	}
	for _, c := range calendars {
		if !want[c.Name] {
			t.Fatalf("unexpected default calendar %q", c.Name)
		}
		delete(want, c.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing default calendars: %v", want)
	}
}

func TestRegister_RejectsEmptyEmail(t *testing.T) {
	svc := newTestAuthServiceWithSignups(t, "", "", true)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "alice", "", "hunter22"); !errors.Is(err, ErrEmailRequired) {
		t.Fatalf("expected ErrEmailRequired, got %v", err)
	}
}

func TestRegister_RejectsInvalidEmail(t *testing.T) {
	svc := newTestAuthServiceWithSignups(t, "", "", true)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "alice", "not-an-email", "hunter22"); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("expected ErrInvalidEmail, got %v", err)
	}
}

// TestValidatePassword_AcceptsExactlyMaxBytes pins the boundary validatePassword
// draws at maxPasswordBytes (#241): a password of exactly that many bytes is
// what bcrypt.GenerateFromPassword itself still accepts, so validatePassword
// rejecting it too (an off-by-one on > vs >=) would be a regression none of
// the "over the limit" tests alone would catch.
func TestValidatePassword_AcceptsExactlyMaxBytes(t *testing.T) {
	if err := validatePassword(strings.Repeat("a", maxPasswordBytes)); err != nil {
		t.Fatalf("expected a password of exactly %d bytes to be accepted, got %v", maxPasswordBytes, err)
	}
}

func TestRegister_RejectsEmptyPassword(t *testing.T) {
	svc := newTestAuthServiceWithSignups(t, "", "", true)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "alice", "alice@example.com", ""); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}
}

// TestValidatePassword_AcceptsExactlyMinRunes pins the boundary
// validatePassword draws at minPasswordRunes (#247): a password of exactly
// that many characters must be accepted, so an off-by-one on < vs <= would
// be a regression none of the "under the floor" tests alone would catch.
func TestValidatePassword_AcceptsExactlyMinRunes(t *testing.T) {
	if err := validatePassword(strings.Repeat("a", minPasswordRunes)); err != nil {
		t.Fatalf("expected a password of exactly %d characters to be accepted, got %v", minPasswordRunes, err)
	}
}

// TestRegister_RejectsPasswordUnderMinLength covers #247: before this floor
// existed, "x" was a valid password on every one of the three paths that
// share validatePassword.
func TestRegister_RejectsPasswordUnderMinLength(t *testing.T) {
	svc := newTestAuthServiceWithSignups(t, "", "", true)
	ctx := context.Background()

	shortPassword := strings.Repeat("a", minPasswordRunes-1)
	if _, err := svc.Register(ctx, "alice", "alice@example.com", shortPassword); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("expected ErrPasswordTooShort, got %v", err)
	}
}

// TestRegister_RejectsPasswordOverMaxBytes covers #241: bcrypt itself limits
// GenerateFromPassword to 72 bytes, so this must come back as ErrPasswordTooLong
// rather than a wrapped, unrecognized error.
func TestRegister_RejectsPasswordOverMaxBytes(t *testing.T) {
	svc := newTestAuthServiceWithSignups(t, "", "", true)
	ctx := context.Background()

	longPassword := strings.Repeat("a", maxPasswordBytes+1)
	if _, err := svc.Register(ctx, "alice", "alice@example.com", longPassword); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("expected ErrPasswordTooLong, got %v", err)
	}
}

// TestRegister_RejectsPasswordOverMaxBytes_MultibyteRunes covers #241's
// "not an exotic input" case: the limit is bytes, not characters, so a
// passphrase of multi-byte runes (e.g. emoji) can exceed it well under 72
// visible characters.
func TestRegister_RejectsPasswordOverMaxBytes_MultibyteRunes(t *testing.T) {
	svc := newTestAuthServiceWithSignups(t, "", "", true)
	ctx := context.Background()

	// 19 emoji, 4 bytes each in UTF-8 = 76 bytes, well under 72 characters.
	longPassword := strings.Repeat("\U0001F600", 19)
	if _, err := svc.Register(ctx, "alice", "alice@example.com", longPassword); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("expected ErrPasswordTooLong, got %v", err)
	}
}

// TestRegister_RejectsEmailOverMaxLength covers #248: before maxEmailLength
// existed, validateEmail bounded nothing but well-formedness, so an address
// thousands of characters long was accepted and stored as the account's
// CalDAV Basic auth username and login identifier.
func TestRegister_RejectsEmailOverMaxLength(t *testing.T) {
	svc := newTestAuthServiceWithSignups(t, "", "", true)
	ctx := context.Background()

	longEmail := strings.Repeat("a", maxEmailLength-len("@example.com")+1) + "@example.com"
	if _, err := svc.Register(ctx, "alice", longEmail, "password123"); !errors.Is(err, ErrEmailTooLong) {
		t.Fatalf("expected ErrEmailTooLong, got %v", err)
	}
}

func TestRegister_DuplicateEmail_ReturnsErrEmailTaken(t *testing.T) {
	svc := newTestAuthServiceWithSignups(t, "", "", true)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "alice", "alice@example.com", "hunter22"); err != nil {
		t.Fatalf("register alice: %v", err)
	}
	if _, err := svc.Register(ctx, "alice-two", "alice@example.com", "hunter22"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

// TestRegister_DuplicateNameIsAllowed covers ADR-0047: Name is a display
// label, not an identifier, so two accounts may share one — only Email is
// unique.
func TestRegister_DuplicateNameIsAllowed(t *testing.T) {
	svc := newTestAuthServiceWithSignups(t, "", "", true)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "alice", "alice@example.com", "hunter22"); err != nil {
		t.Fatalf("register first alice: %v", err)
	}
	if _, err := svc.Register(ctx, "alice", "alice2@example.com", "hunter22"); err != nil {
		t.Fatalf("expected two accounts to share a name, got: %v", err)
	}
}

// TestRegister_DuplicateEmail_DifferentCase_ReturnsErrEmailTaken covers #196
// (ADR-0058): one address is one account regardless of case, so registering
// "Damir@x.com" and then "damir@x.com" is a duplicate, not two accounts.
func TestRegister_DuplicateEmail_DifferentCase_ReturnsErrEmailTaken(t *testing.T) {
	svc := newTestAuthServiceWithSignups(t, "", "", true)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "alice", "Alice@Example.com", "hunter22"); err != nil {
		t.Fatalf("register alice: %v", err)
	}
	if _, err := svc.Register(ctx, "alice-two", "alice@example.com", "hunter22"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

// TestRegister_FoldsEmailCase_LoginWithDifferentCaseAuthenticatesSameUser
// covers #196's primary acceptance criterion: registering with a mixed-case
// address and signing in with a different case must authenticate the same
// User, and the stored Email must come back folded to lowercase.
func TestRegister_FoldsEmailCase_LoginWithDifferentCaseAuthenticatesSameUser(t *testing.T) {
	svc := newTestAuthServiceWithSignups(t, "", "", true)
	ctx := context.Background()

	registered, err := svc.Register(ctx, "damir", "Damir@x.com", "hunter22")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	registeredUserID, err := svc.Authenticate(ctx, registered.AccessToken)
	if err != nil {
		t.Fatalf("authenticate registration access token: %v", err)
	}

	stored, err := svc.users.GetByEmail(ctx, "damir@x.com")
	if err != nil {
		t.Fatalf("get by email: %v", err)
	}
	if stored.Email != "damir@x.com" {
		t.Fatalf("expected stored email to be folded to lowercase, got %q", stored.Email)
	}

	loggedIn, err := svc.Login(ctx, "damir@x.com", "hunter22")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	loggedInUserID, err := svc.Authenticate(ctx, loggedIn.AccessToken)
	if err != nil {
		t.Fatalf("authenticate login access token: %v", err)
	}

	if loggedInUserID != registeredUserID {
		t.Fatalf("expected login with a different case to authenticate the same user %d, got %d", registeredUserID, loggedInUserID)
	}
}

func TestLogin_Success(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	result, err := svc.Login(ctx, "admin@example.com", "admin")
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
	if result.MustChangePassword {
		t.Fatalf("expected must_change_password to be false for an env-configured bootstrap user")
	}

	userID, err := svc.Authenticate(ctx, result.AccessToken)
	if err != nil {
		t.Fatalf("authenticate issued access token: %v", err)
	}

	user, err := svc.users.GetByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if userID != user.ID {
		t.Fatalf("expected access token subject %d to match user id %d", userID, user.ID)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	_, err := svc.Login(ctx, "admin@example.com", "wrong-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogin_UnknownEmail(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	_, err := svc.Login(ctx, "nobody@example.com", "whatever")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

// TestLogin_DisabledAccount_StillIssuesASessionButFlagsIt covers ADR-0044:
// with no instance-wide Admin left to re-activate someone else's account, a
// Disabled User must still be able to log back in to reach the one action
// available to them — reactivating. Login reports IsDisabled instead of
// refusing outright; httpauth.RequireEnabledUser is what closes off every
// other route.
func TestLogin_DisabledAccount_StillIssuesASessionButFlagsIt(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if _, err := svc.users.SetDisabled(ctx, user.ID, true); err != nil {
		t.Fatalf("disable user: %v", err)
	}

	result, err := svc.Login(ctx, "admin@example.com", "admin")
	if err != nil {
		t.Fatalf("expected login to succeed for a disabled account, got %v", err)
	}
	if !result.IsDisabled {
		t.Fatalf("expected the login result to report IsDisabled")
	}
	if result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatalf("expected a full session to still be issued")
	}
}

func TestAuthenticate_RejectsWrongSigningSecret(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()

	// A second instance, identical to svc except for the secret it signs
	// with — its own graph, since nothing about this crosses the database.
	other := newTestGraph(t, WithJWTSecret([]byte("a-different-secret"))).Auth
	if _, _, err := other.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	result, err := other.Login(ctx, "admin@example.com", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if _, err := svc.Authenticate(ctx, result.AccessToken); err == nil {
		t.Fatalf("expected an error authenticating a token signed with a different secret")
	}
}

func TestAuthenticate_RejectsExpiredToken(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByEmail(ctx, "admin@example.com")
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
	svc := newTestAuthService(t, "admin", "admin")

	if _, err := svc.Authenticate(context.Background(), "not-a-real-token"); err == nil {
		t.Fatalf("expected an error authenticating a malformed token")
	}
}

// TestAuthenticate_RejectsAccessTokenIssuedBeforePasswordChange pins the fix
// for #242: ChangePassword already revoked the refresh token, but a
// pre-change access token kept authenticating for its full 15-minute TTL
// because Authenticate never checked the database at all. It now compares
// the token's "tv" (token_version) claim against the account's current
// token_version (ADR-0071).
func TestAuthenticate_RejectsAccessTokenIssuedBeforePasswordChange(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	login, err := svc.Login(ctx, "admin@example.com", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if _, err := svc.Authenticate(ctx, login.AccessToken); err != nil {
		t.Fatalf("expected the freshly issued access token to authenticate, got %v", err)
	}

	if _, err := svc.ChangePassword(ctx, mustUserID(t, svc, "admin"), "admin", "a-new-password"); err != nil {
		t.Fatalf("change password: %v", err)
	}

	if _, err := svc.Authenticate(ctx, login.AccessToken); err == nil {
		t.Fatalf("expected the pre-change access token to be rejected after a password change")
	}
}

// TestAuthenticate_AcceptsAccessTokenIssuedByPasswordChange guards the
// boundary of the #242 fix: the fresh access token ChangePassword itself
// returns already carries the bumped token_version, so it must still
// authenticate even though it was minted in the very call that invalidated
// every token before it.
func TestAuthenticate_AcceptsAccessTokenIssuedByPasswordChange(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	result, err := svc.ChangePassword(ctx, mustUserID(t, svc, "admin"), "admin", "a-new-password")
	if err != nil {
		t.Fatalf("change password: %v", err)
	}

	if _, err := svc.Authenticate(ctx, result.AccessToken); err != nil {
		t.Fatalf("expected the access token ChangePassword just issued to authenticate, got %v", err)
	}
}

func TestMustChangePassword_ReflectsUserFlag(t *testing.T) {
	svc := newTestAuthService(t, "", "")
	ctx := context.Background()
	user := mustSeedUserRequiringPasswordChange(t, svc, ctx, "admin", "admin")

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
	user := mustSeedUserRequiringPasswordChange(t, svc, ctx, "admin", "admin")

	// While must_change_password is true (e.g. a temporary password an Admin
	// issued, ADR-0037), the current password isn't checked at all.
	if _, err := svc.ChangePassword(ctx, user.ID, "this-is-not-the-current-password", "a-new-password"); err != nil {
		t.Fatalf("expected the current password check to be skipped, got %v", err)
	}
}

func TestChangePassword_RequiresCurrentPasswordOnceAlreadyChanged(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByEmail(ctx, "admin@example.com")
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
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	_, err = svc.ChangePassword(ctx, user.ID, "admin", "")
	if !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}
}

// TestChangePassword_RejectsNewPasswordOverMaxBytes covers #241: a new
// password over bcrypt's 72-byte limit must come back as ErrPasswordTooLong
// rather than the opaque 500 a wrapped bcrypt.ErrPasswordTooLong used to
// produce.
func TestChangePassword_RejectsNewPasswordOverMaxBytes(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	longPassword := strings.Repeat("a", maxPasswordBytes+1)
	_, err = svc.ChangePassword(ctx, user.ID, "admin", longPassword)
	if !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("expected ErrPasswordTooLong, got %v", err)
	}
}

func TestUpdateEmail_SetsAValidEmail(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	updated, err := svc.UpdateEmail(ctx, user.ID, "new-admin@example.com")
	if err != nil {
		t.Fatalf("update email: %v", err)
	}
	if updated.Email != "new-admin@example.com" {
		t.Fatalf("expected email to be set, got %+v", updated.Email)
	}
}

func TestUpdateEmail_RejectsAnInvalidAddress(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
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

// TestUpdateEmail_RejectsEmptyString covers ADR-0047: email is mandatory
// now, so — unlike before #169 — an empty string no longer clears it.
func TestUpdateEmail_RejectsEmptyString(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	if _, err := svc.UpdateEmail(ctx, user.ID, ""); !errors.Is(err, ErrEmailRequired) {
		t.Fatalf("expected ErrEmailRequired, got %v", err)
	}
}

func TestUpdateEmail_DuplicateReturnsErrEmailTaken(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := svc.users.Create(ctx, "bob", "bob@example.com", "hash", false); err != nil {
		t.Fatalf("create bob: %v", err)
	}

	if _, err := svc.UpdateEmail(ctx, user.ID, "bob@example.com"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

func TestBootstrap_FirstUserDefaultsToMondayWeekStart(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
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
	svc := newTestAuthService(t, "admin", "admin")
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
	svc := newTestAuthService(t, "admin", "admin")
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
	svc := newTestAuthService(t, "admin", "admin")
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
	svc := newTestAuthService(t, "admin", "admin")
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
	svc := newTestAuthService(t, "admin", "admin")
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
	svc := newTestAuthService(t, "admin", "admin")
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

func TestBootstrap_FirstUserDefaultsTo24hTimeFormat(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if user.TimeFormat != "24h" {
		t.Fatalf("expected time_format to default to \"24h\", got %q", user.TimeFormat)
	}
}

func TestUpdatePreferences_SetsTimeFormat(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	timeFormat := "12h"
	updated, err := svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{TimeFormat: &timeFormat})
	if err != nil {
		t.Fatalf("update preferences: %v", err)
	}
	if updated.TimeFormat != "12h" {
		t.Fatalf("expected time_format \"12h\" to be stored, got %q", updated.TimeFormat)
	}
}

func TestUpdatePreferences_RejectsInvalidTimeFormat(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	timeFormat := "36h"
	_, err = svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{TimeFormat: &timeFormat})
	if !errors.Is(err, ErrInvalidTimeFormat) {
		t.Fatalf("expected ErrInvalidTimeFormat, got %v", err)
	}
}

func TestUpdatePreferences_NilFieldLeavesTimeFormatUntouched(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	updated, err := svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{})
	if err != nil {
		t.Fatalf("update preferences: %v", err)
	}
	if updated.TimeFormat != "24h" {
		t.Fatalf("expected time_format to remain at its default of \"24h\", got %q", updated.TimeFormat)
	}
}

func TestUpdatePreferences_SetsWeekStartAndDefaultViewTogether(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
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

func TestUpdatePreferences_SetsWorkingHours(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	start, end := 9, 17
	updated, err := svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{
		WorkingHours: &WorkingHoursUpdate{Start: &start, End: &end},
	})
	if err != nil {
		t.Fatalf("update preferences: %v", err)
	}
	if updated.WorkingHoursStart == nil || *updated.WorkingHoursStart != 9 {
		t.Fatalf("expected working_hours_start 9 to be stored, got %+v", updated.WorkingHoursStart)
	}
	if updated.WorkingHoursEnd == nil || *updated.WorkingHoursEnd != 17 {
		t.Fatalf("expected working_hours_end 17 to be stored, got %+v", updated.WorkingHoursEnd)
	}
}

func TestUpdatePreferences_SetsWorkingHoursToMinuteOfDayPrecision(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	start, end := 510, 1020 // 08:30-17:00
	updated, err := svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{
		WorkingHours: &WorkingHoursUpdate{Start: &start, End: &end},
	})
	if err != nil {
		t.Fatalf("update preferences: %v", err)
	}
	if updated.WorkingHoursStart == nil || *updated.WorkingHoursStart != 510 {
		t.Fatalf("expected working_hours_start 510 to be stored, got %+v", updated.WorkingHoursStart)
	}
	if updated.WorkingHoursEnd == nil || *updated.WorkingHoursEnd != 1020 {
		t.Fatalf("expected working_hours_end 1020 to be stored, got %+v", updated.WorkingHoursEnd)
	}
}

func TestUpdatePreferences_RejectsWorkingHoursOutOfMinuteOfDayRange(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	start, end := 0, 1440
	_, err = svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{
		WorkingHours: &WorkingHoursUpdate{Start: &start, End: &end},
	})
	if !errors.Is(err, ErrInvalidWorkingHours) {
		t.Fatalf("expected ErrInvalidWorkingHours, got %v", err)
	}
}

func TestUpdatePreferences_ClearsWorkingHoursWithBothNil(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	start, end := 9, 17
	if _, err := svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{
		WorkingHours: &WorkingHoursUpdate{Start: &start, End: &end},
	}); err != nil {
		t.Fatalf("set working hours: %v", err)
	}

	cleared, err := svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{
		WorkingHours: &WorkingHoursUpdate{},
	})
	if err != nil {
		t.Fatalf("clear working hours: %v", err)
	}
	if cleared.WorkingHoursStart != nil || cleared.WorkingHoursEnd != nil {
		t.Fatalf("expected working hours to be cleared, got start=%+v end=%+v", cleared.WorkingHoursStart, cleared.WorkingHoursEnd)
	}
}

func TestUpdatePreferences_RejectsWorkingHoursStartNotBeforeEnd(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	start, end := 17, 9
	_, err = svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{
		WorkingHours: &WorkingHoursUpdate{Start: &start, End: &end},
	})
	if !errors.Is(err, ErrInvalidWorkingHours) {
		t.Fatalf("expected ErrInvalidWorkingHours, got %v", err)
	}
}

func TestUpdatePreferences_RejectsWorkingHoursOneBoundNil(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	start := 9
	_, err = svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{
		WorkingHours: &WorkingHoursUpdate{Start: &start, End: nil},
	})
	if !errors.Is(err, ErrInvalidWorkingHours) {
		t.Fatalf("expected ErrInvalidWorkingHours, got %v", err)
	}

	updated, err := svc.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if updated.WorkingHoursStart != nil || updated.WorkingHoursEnd != nil {
		t.Fatalf("expected an invalid working hours pair to store nothing, got start=%+v end=%+v", updated.WorkingHoursStart, updated.WorkingHoursEnd)
	}
}

func TestUpdatePreferences_NilWorkingHoursLeavesItUntouched(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	start, end := 9, 17
	if _, err := svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{
		WorkingHours: &WorkingHoursUpdate{Start: &start, End: &end},
	}); err != nil {
		t.Fatalf("set working hours: %v", err)
	}

	weekStart := 0
	updated, err := svc.UpdatePreferences(ctx, user.ID, PreferencesUpdate{WeekStart: &weekStart})
	if err != nil {
		t.Fatalf("update preferences: %v", err)
	}
	if updated.WorkingHoursStart == nil || *updated.WorkingHoursStart != 9 {
		t.Fatalf("expected working_hours_start to remain 9, got %+v", updated.WorkingHoursStart)
	}
	if updated.WorkingHoursEnd == nil || *updated.WorkingHoursEnd != 17 {
		t.Fatalf("expected working_hours_end to remain 17, got %+v", updated.WorkingHoursEnd)
	}
}

func TestUpdateName_RenamesTheCaller(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	updated, err := svc.UpdateName(ctx, user.ID, "  New Name  ")
	if err != nil {
		t.Fatalf("update name: %v", err)
	}
	if updated.Name != "New Name" {
		t.Fatalf("expected name 'New Name', got %q", updated.Name)
	}
}

// TestUpdateName_AcceptsSpaces covers ADR-0047: unlike the old username
// rename, a display Name accepts internal whitespace and a colon.
func TestUpdateName_AcceptsSpaces(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	if _, err := svc.UpdateName(ctx, user.ID, "ad:min smith"); err != nil {
		t.Fatalf("expected a name with a colon and a space to be accepted, got %v", err)
	}
}

// TestUpdateName_DuplicateNameIsAllowed covers ADR-0047: Name isn't unique,
// unlike the old username rename it replaces.
func TestUpdateName_DuplicateNameIsAllowed(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	user, _, err := svc.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := svc.users.Create(ctx, "bob", "bob@example.com", "hash", false); err != nil {
		t.Fatalf("create bob: %v", err)
	}

	if _, err := svc.UpdateName(ctx, user.ID, "bob"); err != nil {
		t.Fatalf("expected sharing bob's name to be allowed, got %v", err)
	}
}

func TestChangePassword_NewPasswordWorksForLogin(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	if _, err := svc.ChangePassword(ctx, user.ID, "admin", "a-new-password"); err != nil {
		t.Fatalf("change password: %v", err)
	}

	if _, err := svc.Login(ctx, "admin@example.com", "admin"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected old password to no longer work, got %v", err)
	}

	if _, err := svc.Login(ctx, "admin@example.com", "a-new-password"); err != nil {
		t.Fatalf("expected new password to work, got %v", err)
	}
}

func TestChangePassword_InvalidatesExistingSessions(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	login, err := svc.Login(ctx, "admin@example.com", "admin")
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
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	userID := mustUserID(t, svc, "admin")

	login, err := svc.Login(ctx, "admin@example.com", "admin")
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
	user, err := svc.users.GetByEmail(context.Background(), username+"@example.com")
	if err != nil {
		t.Fatalf("get user %q: %v", username, err)
	}
	return user.ID
}

func TestRefresh_ReturnsNewAccessTokenForValidRefreshToken(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	login, err := svc.Login(ctx, "admin@example.com", "admin")
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

	user, err := svc.users.GetByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if userID != user.ID {
		t.Fatalf("expected refreshed token subject %d to match user id %d", userID, user.ID)
	}
}

func TestRefresh_RejectsUnknownToken(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")

	_, err := svc.Refresh(context.Background(), "not-a-real-refresh-token")
	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expected ErrInvalidSession, got %v", err)
	}
}

func TestRefresh_RejectsExpiredSession(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	user, err := svc.users.GetByEmail(ctx, "admin@example.com")
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

// TestRefresh_StillWorksForADisabledAccount covers ADR-0044: a page reload
// while Disabled must not strand the User short of the one action available
// to them — reactivating. httpauth.RequireEnabledUser, not Refresh, is what
// closes off every other route.
func TestRefresh_StillWorksForADisabledAccount(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	login, err := svc.Login(ctx, "admin@example.com", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	user, err := svc.users.GetByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if _, err := svc.users.SetDisabled(ctx, user.ID, true); err != nil {
		t.Fatalf("disable user: %v", err)
	}

	if _, err := svc.Refresh(ctx, login.RefreshToken); err != nil {
		t.Fatalf("expected refresh to still succeed for a disabled account, got %v", err)
	}
}

func TestLogout_DeletesSessionSoRefreshNoLongerWorks(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")
	ctx := context.Background()
	if _, _, err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	login, err := svc.Login(ctx, "admin@example.com", "admin")
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
	svc := newTestAuthService(t, "admin", "admin")

	if err := svc.Logout(context.Background(), "not-a-real-refresh-token"); err != nil {
		t.Fatalf("expected logout to be a no-op for an unknown token, got %v", err)
	}
}
