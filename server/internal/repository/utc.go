package repository

import "time"

// Every time.Time bound into a query must be normalized to UTC first.
//
// SQLite has no time type: the TIMESTAMP columns are TEXT, and
// modernc.org/sqlite serializes a bound time.Time with the layout
// "2006-01-02 15:04:05 -0700 MST", then parses it back on scan by the
// column's declared type. Two things break when the value carries a non-UTC
// zone:
//
//   - Scanning fails outright when the zone is *nameless*. time.Parse only
//     attaches a named Location when the offset happens to match the
//     process's local zone (a "+01:00" body parsed on a UTC host yields a
//     Location of ""), and Go renders a nameless zone's abbreviation as the
//     numeric offset — "…  +0100 +0100". That matches no layout the driver
//     tries, so it hands back a string and the scan into *time.Time errors.
//     This is host-timezone-dependent: the same write succeeds on a laptop
//     in CET and fails in the UTC container we ship.
//
//   - Comparisons silently return the wrong rows. SQLite compares TEXT
//     lexically, and columns defaulted to CURRENT_TIMESTAMP are always UTC
//     with no offset suffix, so a zoned value sorts against them as a string
//     rather than as an instant.
//
// Normalizing at the bind site keeps both the stored form and every
// comparison canonical regardless of the host's zone or the offset a client
// happened to send.
func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
