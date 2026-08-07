package service

import "github.com/XiovV/calendar/server/internal/repository"

// Access is the resolved answer to "what may this User do with this
// Calendar" (ADR-0034, CONTEXT.md): Owner, Editor, Viewer, or None. Ordered
// from least to most permissive so CanRead/CanWrite/IsOwner and Resolve's
// Subscription clamp can compare Access values directly instead of
// switching on each one.
type Access int

const (
	AccessNone Access = iota
	AccessViewer
	AccessEditor
	AccessOwner
)

func (a Access) String() string {
	switch a {
	case AccessOwner:
		return "owner"
	case AccessEditor:
		return "editor"
	case AccessViewer:
		return "viewer"
	default:
		return "none"
	}
}

// CanRead reports whether a lets its holder see the Calendar and its Events
// at all — Viewer, Editor, or Owner.
func (a Access) CanRead() bool { return a >= AccessViewer }

// CanWrite reports whether a lets its holder create, edit, and delete the
// Calendar's Events and their Reminders — Editor or Owner. Rename, delete,
// re-share, and binding a Subscription are narrower still; see IsOwner.
func (a Access) CanWrite() bool { return a >= AccessEditor }

// IsOwner reports whether a lets its holder manage the Calendar itself —
// rename, recolour, delete, share, revoke, or bind a Subscription
// (CONTEXT.md's Owner entry) — which no Role, however permissive, grants.
func (a Access) IsOwner() bool { return a == AccessOwner }

// ResolveAccess computes Access(user, calendar) (ADR-0034): Owner if userID
// owns calendar, else shareRole's Access if a Share grants one, else None —
// then clamped to Viewer if calendar carries a Subscription, since a
// Subscribed Calendar is read-only for its Owner and every Editor alike
// (ADR-0032). shareRole is nil when no Share row exists for userID; the
// caller (CalendarService.Access) looks it up, since only it knows whether
// userID is the Owner and can skip the lookup entirely in that case. The
// clamp is applied here, at resolution time, rather than when a Share is
// granted, because a source URL can be attached to an already-shared
// Calendar after the fact (ADR-0034).
func ResolveAccess(userID int64, calendar repository.Calendar, shareRole *string) Access {
	base := AccessNone
	switch {
	case calendar.UserID == userID:
		base = AccessOwner
	case shareRole != nil:
		base = roleAccess(*shareRole)
	}

	if calendar.SourceURL != nil && base > AccessViewer {
		base = AccessViewer
	}

	return base
}

// roleAccess maps a Share's stored Role to its Access value. Any role other
// than the two calendar_shares' CHECK constraint allows resolves to None,
// so a row this function can't recognize never grants more than a stranger
// would get.
func roleAccess(role string) Access {
	switch role {
	case repository.RoleEditor:
		return AccessEditor
	case repository.RoleViewer:
		return AccessViewer
	default:
		return AccessNone
	}
}
