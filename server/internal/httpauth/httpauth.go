// Package httpauth wires access-token authentication into net/http:
// extracting the bearer token, validating it, and carrying the resulting
// user id through the request context.
package httpauth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/XiovV/calich/server/internal/clientip"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/repository"
)

type contextKey int

const userIDKey contextKey = iota

func withUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// UserIDFromContext returns the authenticated user id set by RequireAuth.
func UserIDFromContext(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDKey).(int64)
	return id, ok
}

// Authenticator validates an access token and returns the user id it was
// issued for.
type Authenticator interface {
	Authenticate(ctx context.Context, accessToken string) (int64, error)
}

// RequireAuth rejects requests without a valid "Authorization: Bearer <token>"
// header, and otherwise makes the authenticated user id available via
// UserIDFromContext.
func RequireAuth(auth Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "missing or malformed authorization header")
				return
			}

			userID, err := auth.Authenticate(r.Context(), token)
			if err != nil {
				httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or expired access token")
				return
			}

			next.ServeHTTP(w, r.WithContext(withUserID(r.Context(), userID)))
		})
	}
}

// ActiveUserChecker reports whether a user still needs to change their
// password before doing anything else.
type ActiveUserChecker interface {
	MustChangePassword(ctx context.Context, userID int64) (bool, error)
}

// RequireActiveUser blocks requests from a user who still has
// must_change_password set, with a 403 password_change_required response.
// It must run after RequireAuth, which populates the user id in context.
func RequireActiveUser(checker ActiveUserChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := UserIDFromContext(r.Context())
			if !ok {
				httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
				return
			}

			mustChangePassword, err := checker.MustChangePassword(r.Context(), userID)
			if err != nil {
				httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to check user status")
				return
			}
			if mustChangePassword {
				httpresponse.Error(w, http.StatusForbidden, "password_change_required", "password must be changed before continuing")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// DisabledChecker reports whether a user is Disabled (ADR-0044).
type DisabledChecker interface {
	IsDisabled(ctx context.Context, userID int64) (bool, error)
}

// RequireEnabledUser blocks requests from a Disabled user with a 403
// account_disabled response — every route sits behind it except the
// self-service account-lifecycle ones (/api/account/...), which must stay
// reachable so a Disabled User can still reach the one action available to
// them: re-activating. There is no instance-wide Admin left to do it for
// them (ADR-0044). It must run after RequireAuth, which populates the user
// id in context.
func RequireEnabledUser(checker DisabledChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := UserIDFromContext(r.Context())
			if !ok {
				httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
				return
			}

			isDisabled, err := checker.IsDisabled(r.Context(), userID)
			if err != nil {
				httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to check user status")
				return
			}
			if isDisabled {
				httpresponse.Error(w, http.StatusForbidden, "account_disabled", "this account is disabled")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// CalDAVAuthenticator validates HTTP Basic credentials against a per-user
// hashed App password (ADR-0024) and returns the user id they resolve to.
type CalDAVAuthenticator interface {
	Authenticate(ctx context.Context, username, password string) (int64, error)
}

// CalDAVRateLimiter throttles CalDAV Basic auth (#240, ADR-0070) — the same
// "auth" scope Login shares, satisfied by *service.AuthRateLimiter. Declared
// here rather than imported from the service package so this package's only
// new dependency is the leaf repository package, for the ErrRateLimited
// sentinel CheckAuth's error is checked against below.
type CalDAVRateLimiter interface {
	CheckAuth(ctx context.Context, email, ip string) error
	RecordAuthFailure(ctx context.Context, email, ip string) error
	ClearAuthFailures(ctx context.Context, email string) error
}

// RequireCalDAVAuth is the CalDAV counterpart to RequireAuth (ADR-0023,
// ADR-0024): it validates "Authorization: Basic" credentials against a
// hashed App password rather than a bearer access token, and otherwise makes
// the authenticated user id available via the same UserIDFromContext.
//
// limiter's CheckAuth runs before auth.Authenticate — a cheap SQL count
// ahead of AppPasswordService.Authenticate's bcrypt loop, so a flood past
// the ceiling never pays that cost (#240, ADR-0070). A failure charges both
// the Basic-auth username's and the caller's IP's own buckets identically
// whether or not that username names a real account; a success clears the
// username's bucket alone.
func RequireCalDAVAuth(auth CalDAVAuthenticator, limiter CalDAVRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			username, password, ok := r.BasicAuth()
			if !ok {
				requireBasicAuth(w)
				return
			}

			ip := clientip.From(r)

			if err := limiter.CheckAuth(r.Context(), username, ip); err != nil {
				if errors.Is(err, repository.ErrRateLimited) {
					tooManyRequests(w)
					return
				}
				// Anything else is CheckAuth's own failure to answer the
				// question (a DB error), not a verdict about the
				// credentials — collapsing it into requireBasicAuth's 401
				// would tell a CalDAV client to resend credentials when
				// retrying with any credentials can't help.
				httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to authenticate")
				return
			}

			userID, err := auth.Authenticate(r.Context(), username, password)
			if err != nil {
				if recErr := limiter.RecordAuthFailure(r.Context(), username, ip); recErr != nil {
					httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to authenticate")
					return
				}
				requireBasicAuth(w)
				return
			}

			if err := limiter.ClearAuthFailures(r.Context(), username); err != nil {
				httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to authenticate")
				return
			}

			next.ServeHTTP(w, r.WithContext(withUserID(r.Context(), userID)))
		})
	}
}

func requireBasicAuth(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="CalDAV"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// tooManyRequests renders AuthRateLimiter's ceiling being hit (#240,
// ADR-0070). Deliberately no WWW-Authenticate challenge: retrying with the
// same or different Basic credentials won't help until the window passes,
// so this isn't the "send credentials again" signal that header means.
func tooManyRequests(w http.ResponseWriter) {
	http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
}

func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "

	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}

	token := strings.TrimPrefix(header, prefix)
	if token == "" {
		return "", false
	}

	return token, true
}
