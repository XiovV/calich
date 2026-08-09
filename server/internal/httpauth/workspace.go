package httpauth

import (
	"context"
	"net/http"
	"strconv"

	"github.com/XiovV/calendar/server/internal/httpresponse"
)

type workspaceIDKeyType int

const workspaceIDKey workspaceIDKeyType = iota

func withWorkspaceID(ctx context.Context, workspaceID int64) context.Context {
	return context.WithValue(ctx, workspaceIDKey, workspaceID)
}

// WorkspaceIDFromContext returns the active Workspace id set by
// RequireWorkspace.
func WorkspaceIDFromContext(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(workspaceIDKey).(int64)
	return id, ok
}

// WorkspaceMembershipChecker reports whether a user belongs to a Workspace.
type WorkspaceMembershipChecker interface {
	IsMember(ctx context.Context, workspaceID, userID int64) (bool, error)
}

// RequireWorkspace resolves the caller's active Workspace from the
// X-Workspace-Id header (#155, ADR-0045) — there is no server-side notion
// of "currently active Workspace" beyond what the client asserts per
// request, since a User may belong to more than one. A missing or
// malformed header, and a header naming a Workspace the caller isn't a
// Member of, are both refused with 403 — indistinguishable from each
// other, the same way requireOwner's not-found-not-forbidden convention
// treats "doesn't exist" and "you have no authority over it" as one
// answer. Must run after RequireAuth, which populates the user id this
// checks against.
func RequireWorkspace(checker WorkspaceMembershipChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := UserIDFromContext(r.Context())
			if !ok {
				httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
				return
			}

			workspaceID, err := strconv.ParseInt(r.Header.Get("X-Workspace-Id"), 10, 64)
			if err != nil {
				httpresponse.Error(w, http.StatusForbidden, "forbidden", "not a member of this workspace")
				return
			}

			isMember, err := checker.IsMember(r.Context(), workspaceID, userID)
			if err != nil {
				httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to check workspace membership")
				return
			}
			if !isMember {
				httpresponse.Error(w, http.StatusForbidden, "forbidden", "not a member of this workspace")
				return
			}

			next.ServeHTTP(w, r.WithContext(withWorkspaceID(r.Context(), workspaceID)))
		})
	}
}
