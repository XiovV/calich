package service

import (
	"net/mail"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxNameLength bounds validateName (#125, ADR-0047). Bumped from the old
// single-token username's 64 to a more generous round number now that a
// display name is expected to hold a full "First Last" — nothing downstream
// (the users.name column, CalDAV principal paths keyed on id rather than
// name) imposes a tighter limit.
const maxNameLength = 100

// maxEmailLength bounds validateEmail (#248) — RFC 5321 §4.5.3.1 caps a
// forward path at 254 octets, and unlike a display name this value is never
// just text: it's the CalDAV Basic auth username (ADR-0024) sent on every
// sync request, the Email-Channel Reminder recipient (ADR-0021), and the
// login identifier compared on every Login and AppPasswordService.Authenticate.
const maxEmailLength = 254

// maxPasswordBytes is bcrypt's own limit (golang.org/x/crypto/bcrypt,
// bcrypt.go, #241) — GenerateFromPassword returns the opaque
// ErrPasswordTooLong for anything over it, and every caller here wraps that
// as a generic error, so it's checked explicitly up front instead. It's
// bytes, not characters: a password heavy on multi-byte runes (emoji,
// accented letters) can hit this well under 72 visible characters.
const maxPasswordBytes = 72

// minPasswordRunes is the floor validatePassword enforces (#247): before
// this, the only check on any of the three password-setting paths —
// Register, AcceptWorkspaceInviteNewAccount, ChangePassword — was
// non-emptiness, so "x" was a valid password. A self-hoster picking their
// own password is one threat model, but a Workspace Invite lets other
// people pick theirs on the same instance, and CalDAV Basic auth exposes it
// to online guessing. Counted in runes like maxNameLength/validateName,
// not bytes like maxPasswordBytes: that limit is bcrypt's own byte-accurate
// hashing constraint, but a floor meant to resist guessing should track
// visible characters instead.
const minPasswordRunes = 8

// isVisibleRune is validateName and validatePassword's shared definition of
// "actually there" (#251): printable, and not whitespace. Printable alone
// isn't enough — unicode.IsPrint treats the ASCII space as printable, so a
// name or password of nothing but spaces would still pass a bare IsPrint
// check, which is exactly the gap #247's minPasswordRunes floor left open
// for validatePassword ("        " is eight printable runes) and that a
// ZWSP-flanked space (U+200B, ' ', U+200B — none of it trimmed, since ZWSP
// isn't unicode.IsSpace) would have left open for validateName too.
func isVisibleRune(r rune) bool {
	return unicode.IsPrint(r) && !unicode.IsSpace(r)
}

// validateName trims name and checks it against the one set of rules shared
// by every path that picks or renames one — Register, AuthService.UpdateName,
// and AcceptWorkspaceInviteNewAccount — so none of them can drift (#125,
// ADR-0047). Unlike the identifier (see validateEmail), a display name may
// contain spaces: "Jane Smith" is a valid name.
//
// Two further checks guard against degenerate input TrimSpace doesn't catch
// (#251). strings.TrimSpace only strips unicode.IsSpace runes, and U+200B
// ZERO WIDTH SPACE (category Cf, "format") isn't one — a name of nothing but
// ZWSPs (or ZWSPs padding a lone space) would pass an emptiness check on the
// trimmed string alone and render as a blank name visible to other Workspace
// members; requiring at least one isVisibleRune closes that gap without a
// separate name == "" check — an empty string contains none, by definition.
// Control characters — notably NUL — get their own outright rejection rather
// than being silently discounted: letting one through here means it reaches
// SQLite, whose C string handling truncates the name at the NUL with no
// error and no sign anything was dropped.
func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > maxNameLength {
		return "", ErrInvalidDisplayName
	}
	sawVisible := false
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", ErrInvalidDisplayName
		}
		if isVisibleRune(r) {
			sawVisible = true
		}
	}
	if !sawVisible {
		return "", ErrInvalidDisplayName
	}
	return name, nil
}

// normalizeEmail trims and lowercases an email address — the fold shared by
// every path that stores or resolves one, however loosely or strictly it
// otherwise validates the value: validateEmail, Bootstrap, and
// WorkspaceService.CreateInvite (ADR-0058, #196). Folding is what keeps a
// stored email comparable by plain Go string equality (e.g.
// AuthService.AcceptWorkspaceInviteExisting) — users.email's COLLATE NOCASE
// only helps SQL-level lookups, not that.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// emailPattern is the WHATWG "valid email address" grammar that browsers use
// for native type="email" validation
// (https://html.spec.whatwg.org/multipage/input.html#valid-e-mail-address).
// validateEmail must accept exactly what the login form's native validation
// accepts (#243) — mail.ParseAddress alone accepts strictly more than that
// grammar, and the gap keeps reopening this same web-login lockout: a
// non-ASCII local part (mail.ParseAddress allows it; the WHATWG local part
// is ASCII-only) or an RFC 5321 domain-literal like a@[192.168.1.1]
// (mail.ParseAddress allows it; WHATWG requires dot-separated labels) both
// round-trip the equality check below yet get rejected by the browser,
// stranding the account outside the web UI while the API keeps
// authenticating it.
var emailPattern = regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")

// validateEmail normalizes email and checks it against the one set of rules
// shared by every path that picks or changes the account's login identifier
// — Register and AuthService.UpdateEmail (ADR-0047), plus WorkspaceService.CreateInvite
// and event.go's inviteEmail, which pick an email on someone else's behalf.
// maxEmailLength bounds it the same way maxNameLength bounds validateName
// (#248). A colon or other whitespace is rejected because Go's net/http.Request.BasicAuth splits
// credentials on the first colon: an email containing one could create an
// account that can never authenticate over CalDAV
// (AppPasswordService.Authenticate). Ordinary email addresses never contain
// either, so this isn't a new restriction in practice.
//
// mail.ParseAddress parses the full RFC 5322 address grammar, including the
// name-addr form ("Evil<a@b.com>", "<a@b.com>", "a@b.com (comment)") — it's
// an address parser, not a validator of the bare addr-spec this field wants.
// Requiring the parsed Address to have no Name and its Address to equal the
// input string rejects those forms while still accepting ordinary,
// unquoted-local-part addresses (#243). The equality check also rejects a
// quoted local-part ("alice"@example.com): ParseAddress unquotes it to a
// different canonical string, so storing the raw quoted form would reopen
// the same uniqueness bypass under a different disguise — two logins that
// parse to one mailbox but compare unequal as stored strings.
//
// That equality check is necessary but not sufficient: it pins the value to
// a bare addr-spec without constraining it to the subset the login form can
// actually submit. emailPattern is that missing constraint (#243).
func validateEmail(email string) (string, error) {
	email = normalizeEmail(email)
	if email == "" {
		return "", ErrEmailRequired
	}
	if len(email) > maxEmailLength {
		return "", ErrEmailTooLong
	}
	if strings.ContainsAny(email, ":") || strings.ContainsFunc(email, unicode.IsSpace) {
		return "", ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Name != "" || addr.Address != email {
		return "", ErrInvalidEmail
	}
	if !emailPattern.MatchString(email) {
		return "", ErrInvalidEmail
	}
	return email, nil
}

// validatePassword checks a plaintext password against the one set of rules
// shared by every path that hashes one with bcrypt — Register, ChangePassword,
// and AcceptWorkspaceInviteNewAccount (#241, #247) — so none of them can
// drift out of sync with bcrypt.GenerateFromPassword's own maxPasswordBytes
// limit or with each other's minimum.
//
// The floor is counted in runes, not visible content, because spaces are
// legitimate inside a passphrase — so this deliberately doesn't trim. But a
// password of nothing but spaces (or NUL bytes, or ZWSPs) is no harder to
// guess than the "x" this floor was added to reject (#247), so it needs its
// own check: at least one isVisibleRune, on top of meeting minPasswordRunes
// (#251). Control characters are rejected outright even alongside real
// content, matching validateName — bcrypt has no NUL-truncation bug to
// motivate it here, but a control character set via a direct API call
// can't be typed back into a browser's password field, locking the account
// out of the web login the same way a colon in an email locks it out of
// CalDAV Basic auth (see validateEmail).
func validatePassword(password string) error {
	if password == "" {
		return ErrInvalidPassword
	}
	if utf8.RuneCountInString(password) < minPasswordRunes {
		return ErrPasswordTooShort
	}
	if len(password) > maxPasswordBytes {
		return ErrPasswordTooLong
	}
	if strings.ContainsFunc(password, unicode.IsControl) {
		return ErrInvalidPassword
	}
	if !strings.ContainsFunc(password, isVisibleRune) {
		return ErrInvalidPassword
	}
	return nil
}
