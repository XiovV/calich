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
