// Package httpauth wires access-token authentication into net/http:
// extracting the bearer token, validating it, and carrying the resulting
// user id through the request context.
package httpauth

import (
	"context"
	"net/http"
	"strings"

	"github.com/XiovV/calendar/server/internal/httpresponse"
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

// RequireCalDAVAuth is the CalDAV counterpart to RequireAuth (ADR-0023,
// ADR-0024): it validates "Authorization: Basic" credentials against a
// hashed App password rather than a bearer access token, and otherwise makes
// the authenticated user id available via the same UserIDFromContext.
func RequireCalDAVAuth(auth CalDAVAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			username, password, ok := r.BasicAuth()
			if !ok {
				requireBasicAuth(w)
				return
			}

			userID, err := auth.Authenticate(r.Context(), username, password)
			if err != nil {
				requireBasicAuth(w)
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
