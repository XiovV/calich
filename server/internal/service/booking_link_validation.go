package service

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// minDurationMinutes and maxDurationMinutes bound a Booking Link's
	// Duration (#322): at least a minute, at most a day — long enough for
	// any Custom free-entry value a User would actually type.
	minDurationMinutes = 1
	maxDurationMinutes = 24 * 60

	// maxSlugLength bounds validateSlug (#322, ADR-0084) — a Slug is the
	// second half of the same URL path segment a Handle's first half
	// occupies (`/:handle/:slug`), so it shares Handle's own length ceiling
	// (auth_validation.go's maxHandleLength) without depending on that
	// constant directly: the two are free to diverge later without one
	// silently pulling the other along.
	maxSlugLength = 39
)

// slugPattern is a Slug's own character set — identical to a Handle's
// (auth_validation.go's handlePattern): lowercase letters, digits, and
// single hyphens between them, since a Slug is just as much a raw URL path
// segment as a Handle is, with no encoding step between what a User types
// and what appears in the address bar.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// validVisibilities are the three values a Booking Link's Visibility may
// take (ADR-0087, CONTEXT.md's Visibility entry).
var validVisibilities = map[string]bool{
	"public":  true,
	"private": true,
	"paused":  true,
}

// normalizeSlug trims and lowercases a Slug the same way normalizeHandle
// folds a Handle.
func normalizeSlug(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}

// validateSlug normalizes slug and checks it against slugPattern and
// maxSlugLength (#322, ADR-0084).
func validateSlug(slug string) (string, error) {
	slug = normalizeSlug(slug)
	if slug == "" {
		return "", ErrInvalidSlug
	}
	if utf8.RuneCountInString(slug) > maxSlugLength {
		return "", ErrInvalidSlug
	}
	if !slugPattern.MatchString(slug) {
		return "", ErrInvalidSlug
	}
	return slug, nil
}

// truncateSlug clamps s to maxSlugLength runes, trimming a trailing hyphen
// the cut can leave behind — BookingLinkService.Duplicate's own guard
// (mirroring auth_validation.go's appendHandleSuffix fix) against
// "<slug>-copy" silently exceeding the limit before any numeric
// disambiguation is even appended.
func truncateSlug(s string) string {
	if utf8.RuneCountInString(s) <= maxSlugLength {
		return s
	}
	runes := []rune(s)
	return strings.TrimSuffix(string(runes[:maxSlugLength]), "-")
}

// appendSlugSuffix appends "-<suffix>" to base for
// BookingLinkService.freeSlug's disambiguation loop, truncating base first
// to leave exact room for the marker — the same fix SuggestHandle's own
// numeric-suffix loop needed (auth_validation.go's appendHandleSuffix),
// applied here so a base already at maxSlugLength can't produce an
// over-length candidate or loop forever truncating the suffix straight
// back off again.
func appendSlugSuffix(base string, suffix int) string {
	marker := "-" + strconv.Itoa(suffix)
	maxBaseLen := maxSlugLength - len(marker)
	if utf8.RuneCountInString(base) > maxBaseLen {
		runes := []rune(base)
		base = strings.TrimSuffix(string(runes[:maxBaseLen]), "-")
	}
	return base + marker
}
