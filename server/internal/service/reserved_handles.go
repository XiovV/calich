package service

import "strings"

// ReservedHandles is the single source of truth for every top-level path
// segment this app itself routes on (#321, ADR-0084) — a Handle sits in the
// same namespace as the app's own routes (`/:handle`, `/:handle/:slug`), so
// claiming one of these would shadow a real route rather than publish a
// page. The public router (#324, #325) will consume this list directly
// rather than keeping one of its own — a second list would drift, and a
// future top-level route added to the router without also being added here
// would silently shadow somebody's published page. Every entry beyond the
// app's actual current routes (admin, help, about, new) is deliberate
// future-proofing named in ADR-0084's Decision.
var ReservedHandles = map[string]bool{
	"login":                   true,
	"register":                true,
	"settings":                true,
	"api":                     true,
	"book":                    true,
	"assets":                  true,
	"static":                  true,
	"accept-workspace-invite": true,
	"admin":                   true,
	"help":                    true,
	"about":                   true,
	"new":                     true,
}

// IsReservedHandle reports whether handle collides with a path the app
// already routes on, compared case-insensitively like every other Handle
// comparison (ADR-0084). Callers are expected to have already normalized
// handle (lowercased, trimmed) via validateHandle/normalizeHandle; the
// lowercasing here is a second, cheap line of defense rather than the only
// one.
func IsReservedHandle(handle string) bool {
	return ReservedHandles[strings.ToLower(handle)]
}
