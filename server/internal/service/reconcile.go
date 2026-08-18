// reconcile.go implements Refresh's core rule (#85, ADR-0033): a pure diff,
// keyed by ExternalUID, between what a Subscribed Calendar already stores
// and what one fetch of its feed produced. It is deliberately a function of
// its two argument slices alone — no database, no clock — so every rule the
// ADR calls out, including the no-op case and the unparseable-vs-absent
// distinction, table-tests directly without touching SQLite.
package service

import (
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// ExistingSeries is one series a Subscribed Calendar already stores, as
// Refresh diffs it against a freshly fetched feed: its row id, so a
// changed series is updated in place rather than minting a new one, and
// enough of its content to decide whether the feed's version actually
// changed anything.
type ExistingSeries struct {
	MasterID    string
	ExternalUID string
	Content     SeriesWrite
}

// IncomingSeries is one series a Refresh's fetch parsed this time, keyed by
// its foreign UID.
type IncomingSeries struct {
	ExternalUID string
	Write       SeriesWrite
}

// SeriesUpsert is one series ReconcileSeries decided needs a write.
// MasterID is empty for a series the feed introduced since the last
// Refresh (create); set for one that already exists (update in place,
// keeping MasterID and its ExternalUID).
type SeriesUpsert struct {
	MasterID string
	Write    SeriesWrite
}

// ReconcileResult is ReconcileSeries' whole verdict.
type ReconcileResult struct {
	// Upserts are series to create (MasterID empty) or update in place
	// (MasterID set) because the feed's content for that ExternalUID is new
	// or has changed.
	Upserts []SeriesUpsert
	// Tombstones are existing MasterIDs whose ExternalUID the feed no
	// longer serves at all — deleted.
	Tombstones []string
	// SkippedCount is how many existing series were left untouched because
	// the feed still lists their ExternalUID but this fetch couldn't parse
	// it. Present but unparseable is never a reason to tombstone.
	SkippedCount int
	// NoOpCount is how many series the feed still lists, unchanged from what
	// is already stored — no write, no change_seq bump.
	NoOpCount int
}

// ReconcileSeries diffs existing against incoming by ExternalUID:
//
//   - A UID in both: upsert if its content differs from what's stored,
//     otherwise a no-op.
//   - A UID only in incoming: upsert as a new series.
//   - A UID only in existing and named in unparseableUIDs: the feed still
//     serves it but this fetch couldn't parse it — left alone, counted as
//     skipped.
//   - A UID only in existing and not in unparseableUIDs: the feed no
//     longer serves it — tombstoned.
func ReconcileSeries(existing []ExistingSeries, incoming []IncomingSeries, unparseableUIDs map[string]bool) ReconcileResult {
	existingByUID := make(map[string]ExistingSeries, len(existing))
	for _, e := range existing {
		existingByUID[e.ExternalUID] = e
	}

	var result ReconcileResult
	seen := make(map[string]bool, len(incoming))
	for _, in := range incoming {
		seen[in.ExternalUID] = true

		e, ok := existingByUID[in.ExternalUID]
		if !ok {
			result.Upserts = append(result.Upserts, SeriesUpsert{Write: in.Write})
			continue
		}
		if seriesContentEqual(e.Content, in.Write) {
			result.NoOpCount++
			continue
		}
		result.Upserts = append(result.Upserts, SeriesUpsert{MasterID: e.MasterID, Write: in.Write})
	}

	for _, e := range existing {
		if seen[e.ExternalUID] {
			continue
		}
		if unparseableUIDs[e.ExternalUID] {
			result.SkippedCount++
			continue
		}
		result.Tombstones = append(result.Tombstones, e.MasterID)
	}

	return result
}

// seriesContentEqual reports whether a and b describe the same series:
// the Master's own fields, its Reminders and Exdates as sets, and its
// Overrides matched by RecurrenceID and compared as a set — order never
// matters, since nothing about either side's slice order is meaningful.
// Comparing Reminders here (rather than excluding them, as an ordinary
// content diff might) is what lets a KeepAlarms toggle actually reach
// storage on the next Refresh that reconciles the series (#87, ADR-0032) —
// a scheduled Refresh a conditional GET still short-circuits before this
// runs, but a forced "Refresh now" always reaches it.
func seriesContentEqual(a, b SeriesWrite) bool {
	if a.Title != b.Title || a.Description != b.Description || a.Location != b.Location || a.URL != b.URL {
		return false
	}
	if !a.Start.Equal(b.Start) || !a.End.Equal(b.End) {
		return false
	}
	if a.AllDay != b.AllDay || a.Rrule != b.Rrule {
		return false
	}
	if !stringPtrEqual(a.Tzid, b.Tzid) {
		return false
	}
	if !timeSetEqual(a.Exdates, b.Exdates) {
		return false
	}
	if !reminderSetEqual(a.Reminders, b.Reminders) {
		return false
	}
	return overrideSetEqual(a.Overrides, b.Overrides)
}

func stringPtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func timeSetEqual(a, b []time.Time) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[int64]int, len(a))
	for _, t := range a {
		counts[t.UnixNano()]++
	}
	for _, t := range b {
		counts[t.UnixNano()]--
	}
	for _, c := range counts {
		if c != 0 {
			return false
		}
	}
	return true
}

// overrideSetEqual compares two series' Overrides as a set, matched by
// RecurrenceID alone rather than the full (ExternalUID, RecurrenceID) key
// ADR-0033 describes: both sides here already belong to the one series this
// call is comparing, so every Override in a and b necessarily shares that
// series' ExternalUID (a Master's ExternalUID is unique per Calendar,
// enforced by idx_events_calendar_external_uid) — RecurrenceID alone is
// already unambiguous at this scope. updateSubscribedSeries applies the
// same reasoning when it matches existing Overrides to write them.
func overrideSetEqual(a, b []OverrideWrite) bool {
	if len(a) != len(b) {
		return false
	}
	byRecurrenceID := make(map[int64]OverrideWrite, len(a))
	for _, o := range a {
		byRecurrenceID[o.RecurrenceID.UnixNano()] = o
	}
	for _, o := range b {
		match, ok := byRecurrenceID[o.RecurrenceID.UnixNano()]
		if !ok || !overrideContentEqual(match, o) {
			return false
		}
	}
	return true
}

func overrideContentEqual(a, b OverrideWrite) bool {
	if a.Title != b.Title || a.Description != b.Description || a.Location != b.Location || a.URL != b.URL {
		return false
	}
	if !a.Start.Equal(b.Start) || !a.End.Equal(b.End) {
		return false
	}
	if a.AllDay != b.AllDay {
		return false
	}
	if !stringPtrEqual(a.Tzid, b.Tzid) {
		return false
	}
	return reminderSetEqual(a.Reminders, b.Reminders)
}

// reminderSetEqual compares two Reminder slices as a set of (offset,
// channel) pairs, ignoring ID — an incoming Reminder parsed from a feed
// never carries one, while a stored one always does (#87, ADR-0032).
func reminderSetEqual(a, b []repository.Reminder) bool {
	if len(a) != len(b) {
		return false
	}
	type key struct {
		offset  int
		channel string
	}
	counts := make(map[key]int, len(a))
	for _, r := range a {
		counts[key{r.OffsetMinutes, r.Channel}]++
	}
	for _, r := range b {
		counts[key{r.OffsetMinutes, r.Channel}]--
	}
	for _, c := range counts {
		if c != 0 {
			return false
		}
	}
	return true
}
