package service

import (
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return parsed
}

func basicWrite(t *testing.T, title string) SeriesWrite {
	t.Helper()
	return SeriesWrite{
		Title: title,
		Start: mustTime(t, "2026-01-01T10:00:00Z"),
		End:   mustTime(t, "2026-01-01T11:00:00Z"),
	}
}

func TestReconcileSeries_NewUIDIsInsertedAsUpsertWithNoMasterID(t *testing.T) {
	incoming := []IncomingSeries{{ExternalUID: "uid-1", Write: basicWrite(t, "Standup")}}

	result := ReconcileSeries(nil, incoming, nil)

	if len(result.Upserts) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(result.Upserts))
	}
	if result.Upserts[0].MasterID != "" {
		t.Fatalf("expected empty MasterID for a new series, got %q", result.Upserts[0].MasterID)
	}
	if result.Upserts[0].Write.Title != "Standup" {
		t.Fatalf("expected the incoming write to be carried through, got %+v", result.Upserts[0].Write)
	}
	if result.NoOpCount != 0 || result.SkippedCount != 0 || len(result.Tombstones) != 0 {
		t.Fatalf("expected no other buckets populated, got %+v", result)
	}
}

func TestReconcileSeries_ChangedSeriesIsUpsertedKeepingMasterID(t *testing.T) {
	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: basicWrite(t, "Standup")}}
	incoming := []IncomingSeries{{ExternalUID: "uid-1", Write: basicWrite(t, "Standup (renamed)")}}

	result := ReconcileSeries(existing, incoming, nil)

	if len(result.Upserts) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(result.Upserts))
	}
	if result.Upserts[0].MasterID != "master-1" {
		t.Fatalf("expected the existing row id to be kept, got %q", result.Upserts[0].MasterID)
	}
	if result.Upserts[0].Write.Title != "Standup (renamed)" {
		t.Fatalf("expected the new title, got %q", result.Upserts[0].Write.Title)
	}
	if result.NoOpCount != 0 {
		t.Fatalf("expected NoOpCount 0 for a changed series, got %d", result.NoOpCount)
	}
}

func TestReconcileSeries_UnchangedSeriesIsANoOp(t *testing.T) {
	write := basicWrite(t, "Standup")
	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: write}}
	incoming := []IncomingSeries{{ExternalUID: "uid-1", Write: write}}

	result := ReconcileSeries(existing, incoming, nil)

	if len(result.Upserts) != 0 {
		t.Fatalf("expected no upserts for an unchanged series, got %+v", result.Upserts)
	}
	if result.NoOpCount != 1 {
		t.Fatalf("expected NoOpCount 1, got %d", result.NoOpCount)
	}
}

func TestReconcileSeries_UnchangedSeriesWithinAChangedFeedIsStillANoOp(t *testing.T) {
	unchanged := basicWrite(t, "Standup")
	existing := []ExistingSeries{
		{MasterID: "master-1", ExternalUID: "uid-1", Content: unchanged},
		{MasterID: "master-2", ExternalUID: "uid-2", Content: basicWrite(t, "Retro")},
	}
	incoming := []IncomingSeries{
		{ExternalUID: "uid-1", Write: unchanged},
		{ExternalUID: "uid-2", Write: basicWrite(t, "Retro (renamed)")},
	}

	result := ReconcileSeries(existing, incoming, nil)

	if result.NoOpCount != 1 {
		t.Fatalf("expected exactly the untouched series to count as a no-op, got %d", result.NoOpCount)
	}
	if len(result.Upserts) != 1 || result.Upserts[0].MasterID != "master-2" {
		t.Fatalf("expected only the changed series to be upserted, got %+v", result.Upserts)
	}
}

func TestReconcileSeries_PresentButUnparseableIsSkippedNotTombstoned(t *testing.T) {
	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: basicWrite(t, "Standup")}}
	unparseable := map[string]bool{"uid-1": true}

	result := ReconcileSeries(existing, nil, unparseable)

	if result.SkippedCount != 1 {
		t.Fatalf("expected SkippedCount 1, got %d", result.SkippedCount)
	}
	if len(result.Tombstones) != 0 {
		t.Fatalf("expected no tombstones for an unparseable-but-present series, got %+v", result.Tombstones)
	}
	if len(result.Upserts) != 0 {
		t.Fatalf("expected no upserts, got %+v", result.Upserts)
	}
}

func TestReconcileSeries_AbsentSeriesIsTombstoned(t *testing.T) {
	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: basicWrite(t, "Standup")}}

	result := ReconcileSeries(existing, nil, nil)

	if len(result.Tombstones) != 1 || result.Tombstones[0] != "master-1" {
		t.Fatalf("expected master-1 tombstoned, got %+v", result.Tombstones)
	}
	if result.SkippedCount != 0 {
		t.Fatalf("expected SkippedCount 0, got %d", result.SkippedCount)
	}
}

func TestReconcileSeries_AllThreeBucketsTogether(t *testing.T) {
	unchanged := basicWrite(t, "Standup")
	existing := []ExistingSeries{
		{MasterID: "master-unchanged", ExternalUID: "uid-unchanged", Content: unchanged},
		{MasterID: "master-changed", ExternalUID: "uid-changed", Content: basicWrite(t, "Retro")},
		{MasterID: "master-unparseable", ExternalUID: "uid-unparseable", Content: basicWrite(t, "Weekly sync")},
		{MasterID: "master-absent", ExternalUID: "uid-absent", Content: basicWrite(t, "1:1")},
	}
	incoming := []IncomingSeries{
		{ExternalUID: "uid-unchanged", Write: unchanged},
		{ExternalUID: "uid-changed", Write: basicWrite(t, "Retro (renamed)")},
		{ExternalUID: "uid-new", Write: basicWrite(t, "Brand new")},
	}
	unparseable := map[string]bool{"uid-unparseable": true}

	result := ReconcileSeries(existing, incoming, unparseable)

	if result.NoOpCount != 1 {
		t.Fatalf("expected 1 no-op, got %d", result.NoOpCount)
	}
	if len(result.Upserts) != 2 {
		t.Fatalf("expected 2 upserts (1 changed, 1 new), got %+v", result.Upserts)
	}
	if result.SkippedCount != 1 {
		t.Fatalf("expected 1 skipped, got %d", result.SkippedCount)
	}
	if len(result.Tombstones) != 1 || result.Tombstones[0] != "master-absent" {
		t.Fatalf("expected only master-absent tombstoned, got %+v", result.Tombstones)
	}
}

func TestReconcileSeries_OverridesMatchByRecurrenceIDNotOrder(t *testing.T) {
	recA := mustTime(t, "2026-01-08T10:00:00Z")
	recB := mustTime(t, "2026-01-15T10:00:00Z")

	master := basicWrite(t, "Standup")
	master.Rrule = "FREQ=WEEKLY"
	master.Overrides = []OverrideWrite{
		{RecurrenceID: recA, Title: "Standup (moved)", Start: recA, End: recA.Add(time.Hour)},
		{RecurrenceID: recB, Title: "Standup", Start: recB, End: recB.Add(time.Hour)},
	}

	// Same overrides, reversed order — must still compare equal.
	reordered := master
	reordered.Overrides = []OverrideWrite{master.Overrides[1], master.Overrides[0]}

	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: master}}
	incoming := []IncomingSeries{{ExternalUID: "uid-1", Write: reordered}}

	result := ReconcileSeries(existing, incoming, nil)

	if result.NoOpCount != 1 {
		t.Fatalf("expected reordered-but-identical overrides to be a no-op, got upserts=%+v noop=%d", result.Upserts, result.NoOpCount)
	}
}

func TestReconcileSeries_OverrideContentChangeMakesSeriesAnUpsert(t *testing.T) {
	rec := mustTime(t, "2026-01-08T10:00:00Z")

	before := basicWrite(t, "Standup")
	before.Rrule = "FREQ=WEEKLY"
	before.Overrides = []OverrideWrite{{RecurrenceID: rec, Title: "Standup (moved)", Start: rec, End: rec.Add(time.Hour)}}

	after := before
	after.Overrides = []OverrideWrite{{RecurrenceID: rec, Title: "Standup (moved again)", Start: rec, End: rec.Add(time.Hour)}}

	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: before}}
	incoming := []IncomingSeries{{ExternalUID: "uid-1", Write: after}}

	result := ReconcileSeries(existing, incoming, nil)

	if result.NoOpCount != 0 || len(result.Upserts) != 1 {
		t.Fatalf("expected an override content change to force an upsert, got noop=%d upserts=%+v", result.NoOpCount, result.Upserts)
	}
}

func TestReconcileSeries_OverrideURLChangeAloneMakesSeriesAnUpsert(t *testing.T) {
	rec := mustTime(t, "2026-01-08T10:00:00Z")

	before := basicWrite(t, "Standup")
	before.Rrule = "FREQ=WEEKLY"
	before.Overrides = []OverrideWrite{{RecurrenceID: rec, Title: "Standup (moved)", Start: rec, End: rec.Add(time.Hour)}}

	after := before
	after.Overrides = []OverrideWrite{{RecurrenceID: rec, Title: "Standup (moved)", Start: rec, End: rec.Add(time.Hour), URL: "https://example.com/ticket/42"}}

	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: before}}
	incoming := []IncomingSeries{{ExternalUID: "uid-1", Write: after}}

	result := ReconcileSeries(existing, incoming, nil)

	if result.NoOpCount != 0 || len(result.Upserts) != 1 {
		t.Fatalf("expected an override URL change alone to force an upsert, got noop=%d upserts=%+v", result.NoOpCount, result.Upserts)
	}
}

func TestReconcileSeries_OverrideAddedOrRemovedMakesSeriesAnUpsert(t *testing.T) {
	rec := mustTime(t, "2026-01-08T10:00:00Z")
	override := OverrideWrite{RecurrenceID: rec, Title: "Standup (moved)", Start: rec, End: rec.Add(time.Hour)}

	withOverride := basicWrite(t, "Standup")
	withOverride.Rrule = "FREQ=WEEKLY"
	withOverride.Overrides = []OverrideWrite{override}

	withoutOverride := withOverride
	withoutOverride.Overrides = nil

	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: withoutOverride}}
	incoming := []IncomingSeries{{ExternalUID: "uid-1", Write: withOverride}}

	result := ReconcileSeries(existing, incoming, nil)
	if result.NoOpCount != 0 || len(result.Upserts) != 1 {
		t.Fatalf("expected an added override to force an upsert, got noop=%d upserts=%+v", result.NoOpCount, result.Upserts)
	}

	// And the reverse: an override the feed no longer serves.
	existing = []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: withOverride}}
	incoming = []IncomingSeries{{ExternalUID: "uid-1", Write: withoutOverride}}

	result = ReconcileSeries(existing, incoming, nil)
	if result.NoOpCount != 0 || len(result.Upserts) != 1 {
		t.Fatalf("expected a removed override to force an upsert, got noop=%d upserts=%+v", result.NoOpCount, result.Upserts)
	}
}

func TestReconcileSeries_ExdatesComparedAsASetNotOrder(t *testing.T) {
	a := mustTime(t, "2026-01-08T10:00:00Z")
	b := mustTime(t, "2026-01-15T10:00:00Z")

	write := basicWrite(t, "Standup")
	write.Rrule = "FREQ=WEEKLY"
	write.Exdates = []time.Time{a, b}

	reordered := write
	reordered.Exdates = []time.Time{b, a}

	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: write}}
	incoming := []IncomingSeries{{ExternalUID: "uid-1", Write: reordered}}

	result := ReconcileSeries(existing, incoming, nil)
	if result.NoOpCount != 1 {
		t.Fatalf("expected reordered-but-identical exdates to be a no-op, got %+v", result)
	}
}

// TestReconcileSeries_ReminderChangeAloneMakesSeriesAnUpsert exercises #87's
// mechanism for KeepAlarms taking effect: two series with otherwise
// identical content differ only in their stored vs. feed-parsed Reminders
// (an existing row with none against an incoming write with one, the shape
// a KeepAlarms-off-to-on toggle produces on an otherwise unchanged feed),
// and must still be diffed as changed rather than a no-op.
func TestReconcileSeries_ReminderChangeAloneMakesSeriesAnUpsert(t *testing.T) {
	withoutReminder := basicWrite(t, "Standup")
	withReminder := withoutReminder
	withReminder.Reminders = []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}

	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: withoutReminder}}
	incoming := []IncomingSeries{{ExternalUID: "uid-1", Write: withReminder}}

	result := ReconcileSeries(existing, incoming, nil)
	if result.NoOpCount != 0 || len(result.Upserts) != 1 {
		t.Fatalf("expected a Reminder change alone to force an upsert, got noop=%d upserts=%+v", result.NoOpCount, result.Upserts)
	}
}

// TestReconcileSeries_URLChangeAloneMakesSeriesAnUpsert exercises #207's
// acceptance criterion that a feed changing only a series' URL is detected
// as a change, not treated as unchanged.
func TestReconcileSeries_URLChangeAloneMakesSeriesAnUpsert(t *testing.T) {
	withoutURL := basicWrite(t, "Standup")
	withURL := withoutURL
	withURL.URL = "https://example.com/ticket/42"

	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: withoutURL}}
	incoming := []IncomingSeries{{ExternalUID: "uid-1", Write: withURL}}

	result := ReconcileSeries(existing, incoming, nil)
	if result.NoOpCount != 0 || len(result.Upserts) != 1 {
		t.Fatalf("expected a URL change alone to force an upsert, got noop=%d upserts=%+v", result.NoOpCount, result.Upserts)
	}
}

// TestReconcileSeries_ReminderSetComparedNotOrder mirrors
// TestReconcileSeries_ExdatesComparedAsASetNotOrder for Reminders: the same
// Reminders in a different order, or carrying a different (unread-back) ID,
// must still compare equal.
func TestReconcileSeries_ReminderSetComparedNotOrder(t *testing.T) {
	write := basicWrite(t, "Standup")
	write.Reminders = []repository.Reminder{
		{ID: 1, OffsetMinutes: 10, Channel: "notification"},
		{OffsetMinutes: 30, Channel: "email"},
	}

	reordered := write
	reordered.Reminders = []repository.Reminder{
		{OffsetMinutes: 30, Channel: "email"},
		{ID: 99, OffsetMinutes: 10, Channel: "notification"},
	}

	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: write}}
	incoming := []IncomingSeries{{ExternalUID: "uid-1", Write: reordered}}

	result := ReconcileSeries(existing, incoming, nil)
	if result.NoOpCount != 1 {
		t.Fatalf("expected reordered-but-identical Reminders (ignoring ID) to be a no-op, got %+v", result)
	}
}

func TestReconcileSeries_EmptyInputsProduceEmptyResult(t *testing.T) {
	result := ReconcileSeries(nil, nil, nil)

	if len(result.Upserts) != 0 || len(result.Tombstones) != 0 || result.SkippedCount != 0 || result.NoOpCount != 0 {
		t.Fatalf("expected an empty result, got %+v", result)
	}
}
