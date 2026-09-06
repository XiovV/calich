package service

import (
	"testing"
	"time"
)

func TestRefreshModeForCursor(t *testing.T) {
	if RefreshModeForCursor(true) != RefreshModeDelta {
		t.Fatal("a stored cursor should select Delta")
	}
	if RefreshModeForCursor(false) != RefreshModeFull {
		t.Fatal("no cursor should select Full")
	}
}

// storedSeries is one ExistingSeries fixture for ReconcileDelta tests.
func storedSeries(t *testing.T, masterID, uid, title string) ExistingSeries {
	t.Helper()
	return ExistingSeries{MasterID: masterID, ExternalUID: uid, Content: basicWrite(t, title)}
}

// The test that is the executable form of ADR-0053: a Delta batch naming one
// changed series, against a stored Calendar of many, produces exactly one
// upsert and ZERO tombstones. Absence is the overwhelming majority of the
// stored set and means "unchanged".
func TestReconcileDelta_OneChangedSeriesAgainstManyProducesOneUpsertZeroTombstones(t *testing.T) {
	existing := []ExistingSeries{
		storedSeries(t, "master-1", "uid-1", "Standup"),
		storedSeries(t, "master-2", "uid-2", "Retro"),
		storedSeries(t, "master-3", "uid-3", "1:1"),
		storedSeries(t, "master-4", "uid-4", "Planning"),
	}
	renamed := basicWrite(t, "Standup (renamed)")
	renamed.ExternalUID = "uid-1"
	changes := []DeltaSeriesChange{{ExternalUID: "uid-1", Master: &renamed}}

	result := ReconcileDelta(existing, changes, nil)

	if len(result.Upserts) != 1 {
		t.Fatalf("expected exactly 1 upsert, got %+v", result.Upserts)
	}
	if result.Upserts[0].MasterID != "master-1" {
		t.Fatalf("expected the stored row id to be kept, got %q", result.Upserts[0].MasterID)
	}
	if result.Upserts[0].Write.Title != "Standup (renamed)" {
		t.Fatalf("expected the new title, got %q", result.Upserts[0].Write.Title)
	}
	if len(result.Tombstones) != 0 {
		t.Fatalf("delta mode must never tombstone by absence, got %+v", result.Tombstones)
	}
	if result.NoOpCount != 0 {
		t.Fatalf("the four unmentioned series are simply not in scope, not no-ops; got NoOpCount %d", result.NoOpCount)
	}
}

func TestReconcileDelta_EmptyBatchChangesNothing(t *testing.T) {
	existing := []ExistingSeries{
		storedSeries(t, "master-1", "uid-1", "Standup"),
		storedSeries(t, "master-2", "uid-2", "Retro"),
	}

	result := ReconcileDelta(existing, nil, nil)

	if len(result.Upserts) != 0 || len(result.Tombstones) != 0 || result.NoOpCount != 0 || result.SkippedCount != 0 {
		t.Fatalf("expected an empty result for an empty batch, got %+v", result)
	}
}

// A top-level status=cancelled is the ONLY path to a tombstone, and it hits
// only the named series.
func TestReconcileDelta_ExplicitDeletionTombstonesOnlyThatSeries(t *testing.T) {
	existing := []ExistingSeries{
		storedSeries(t, "master-1", "uid-1", "Standup"),
		storedSeries(t, "master-2", "uid-2", "Retro"),
	}

	result := ReconcileDelta(existing, nil, []string{"uid-2"})

	if len(result.Tombstones) != 1 || result.Tombstones[0] != "master-2" {
		t.Fatalf("expected exactly master-2 tombstoned, got %+v", result.Tombstones)
	}
	if len(result.Upserts) != 0 {
		t.Fatalf("expected no upserts, got %+v", result.Upserts)
	}
}

func TestReconcileDelta_DeletionOfUnknownUIDIsIgnored(t *testing.T) {
	existing := []ExistingSeries{storedSeries(t, "master-1", "uid-1", "Standup")}

	result := ReconcileDelta(existing, nil, []string{"uid-never-stored"})

	if len(result.Tombstones) != 0 {
		t.Fatalf("expected no tombstones for an unknown deletion, got %+v", result.Tombstones)
	}
}

// A new series the Provider introduced (Master present, nothing stored) is
// an ordinary create.
func TestReconcileDelta_NewSeriesIsCreated(t *testing.T) {
	newMaster := basicWrite(t, "New meeting")
	newMaster.ExternalUID = "uid-new"
	changes := []DeltaSeriesChange{{ExternalUID: "uid-new", Master: &newMaster}}

	result := ReconcileDelta(nil, changes, nil)

	if len(result.Upserts) != 1 || result.Upserts[0].MasterID != "" {
		t.Fatalf("expected one create (empty MasterID), got %+v", result.Upserts)
	}
}

// The common Delta case: one instance of a recurring series changed, and the
// Master never came in the batch. The change merges onto the stored series,
// keeping the Overrides the batch didn't mention, and tombstones nothing.
func TestReconcileDelta_InstanceOnlyChangeMergesOntoStoredSeries(t *testing.T) {
	recID1 := mustTime(t, "2026-01-05T09:00:00Z")
	recID2 := mustTime(t, "2026-01-12T09:00:00Z")

	stored := basicWrite(t, "Standup")
	stored.Rrule = "FREQ=WEEKLY"
	stored.Overrides = []OverrideWrite{{
		RecurrenceID: recID1,
		Title:        "Standup (week 1 moved)",
		Start:        mustTime(t, "2026-01-05T10:00:00Z"),
		End:          mustTime(t, "2026-01-05T10:30:00Z"),
	}}
	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: stored}}

	changedInstance := OverrideWrite{
		RecurrenceID: recID2,
		Title:        "Standup (week 2 moved)",
		Start:        mustTime(t, "2026-01-12T11:00:00Z"),
		End:          mustTime(t, "2026-01-12T11:30:00Z"),
		ExternalUID:  "uid-1",
	}
	changes := []DeltaSeriesChange{{ExternalUID: "uid-1", Overrides: []OverrideWrite{changedInstance}}}

	result := ReconcileDelta(existing, changes, nil)

	if len(result.Upserts) != 1 || result.Upserts[0].MasterID != "master-1" {
		t.Fatalf("expected one in-place upsert, got %+v", result.Upserts)
	}
	if len(result.Tombstones) != 0 {
		t.Fatalf("expected zero tombstones, got %+v", result.Tombstones)
	}
	got := result.Upserts[0].Write
	if got.Title != "Standup" || got.Rrule != "FREQ=WEEKLY" {
		t.Fatalf("expected Master-level fields untouched, got title=%q rrule=%q", got.Title, got.Rrule)
	}
	if len(got.Overrides) != 2 {
		t.Fatalf("expected both the stored and the changed Override, got %d: %+v", len(got.Overrides), got.Overrides)
	}
}

// A cancelled instance whose Master isn't in the batch adds an Exdate to the
// stored series and tombstones nothing.
func TestReconcileDelta_InstanceCancellationAddsExdate(t *testing.T) {
	cancelAt := mustTime(t, "2026-01-19T09:00:00Z")
	stored := basicWrite(t, "Standup")
	stored.Rrule = "FREQ=WEEKLY"
	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: stored}}

	changes := []DeltaSeriesChange{{ExternalUID: "uid-1", Cancellations: []time.Time{cancelAt}}}

	result := ReconcileDelta(existing, changes, nil)

	if len(result.Upserts) != 1 {
		t.Fatalf("expected one upsert, got %+v", result.Upserts)
	}
	if len(result.Tombstones) != 0 {
		t.Fatalf("expected zero tombstones, got %+v", result.Tombstones)
	}
	got := result.Upserts[0].Write
	if len(got.Exdates) != 1 || !got.Exdates[0].Equal(cancelAt) {
		t.Fatalf("expected the cancellation instant in Exdates, got %+v", got.Exdates)
	}
}

// An instance-only change to a series this Calendar doesn't store has
// nothing to merge into — present-but-unmappable, counted as skipped, never
// treated as an absence.
func TestReconcileDelta_OrphanInstanceChangeIsSkippedNotTombstoned(t *testing.T) {
	existing := []ExistingSeries{storedSeries(t, "master-1", "uid-1", "Standup")}

	orphan := OverrideWrite{
		RecurrenceID: mustTime(t, "2026-02-02T09:00:00Z"),
		Title:        "Ghost",
		Start:        mustTime(t, "2026-02-02T09:00:00Z"),
		End:          mustTime(t, "2026-02-02T09:30:00Z"),
	}
	changes := []DeltaSeriesChange{{ExternalUID: "uid-unknown", Overrides: []OverrideWrite{orphan}}}

	result := ReconcileDelta(existing, changes, nil)

	if result.SkippedCount != 1 {
		t.Fatalf("expected SkippedCount 1, got %d", result.SkippedCount)
	}
	if len(result.Upserts) != 0 || len(result.Tombstones) != 0 {
		t.Fatalf("expected no upserts and no tombstones, got %+v", result)
	}
}

// A batch Master identical to what's stored is a no-op — no write, no
// change_seq bump.
func TestReconcileDelta_UnchangedMasterIsANoOp(t *testing.T) {
	stored := basicWrite(t, "Standup")
	stored.ExternalUID = "uid-1"
	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: stored}}

	same := basicWrite(t, "Standup")
	same.ExternalUID = "uid-1"
	changes := []DeltaSeriesChange{{ExternalUID: "uid-1", Master: &same}}

	result := ReconcileDelta(existing, changes, nil)

	if result.NoOpCount != 1 {
		t.Fatalf("expected NoOpCount 1, got %+v", result)
	}
	if len(result.Upserts) != 0 {
		t.Fatalf("expected no upserts, got %+v", result.Upserts)
	}
}

// A batch Master that changed only its title must not drop the Overrides
// Google didn't re-send alongside it.
func TestReconcileDelta_MasterChangePreservesUnmentionedOverrides(t *testing.T) {
	recID := mustTime(t, "2026-01-05T09:00:00Z")
	stored := basicWrite(t, "Standup")
	stored.Rrule = "FREQ=WEEKLY"
	stored.ExternalUID = "uid-1"
	stored.Overrides = []OverrideWrite{{
		RecurrenceID: recID,
		Title:        "Standup (moved)",
		Start:        mustTime(t, "2026-01-05T10:00:00Z"),
		End:          mustTime(t, "2026-01-05T10:30:00Z"),
	}}
	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: stored}}

	batchMaster := basicWrite(t, "Standup (renamed)")
	batchMaster.Rrule = "FREQ=WEEKLY"
	batchMaster.ExternalUID = "uid-1"
	changes := []DeltaSeriesChange{{ExternalUID: "uid-1", Master: &batchMaster}}

	result := ReconcileDelta(existing, changes, nil)

	if len(result.Upserts) != 1 {
		t.Fatalf("expected one upsert, got %+v", result.Upserts)
	}
	got := result.Upserts[0].Write
	if got.Title != "Standup (renamed)" {
		t.Fatalf("expected the renamed title, got %q", got.Title)
	}
	if len(got.Overrides) != 1 || !got.Overrides[0].RecurrenceID.Equal(recID) {
		t.Fatalf("expected the stored Override to survive a Master-only change, got %+v", got.Overrides)
	}
}

// --- #289, ADR-0075: Delta Refresh applies the same until-touched colour
// rule Full Refresh does, via mergeDeltaChange ---

func TestReconcileDelta_UntouchedMasterFollowsProviderRecolour(t *testing.T) {
	stored := basicWrite(t, "Standup")
	stored.ExternalUID = "uid-1"
	stored.Color = strPtr("#FBD75BFF")
	stored.ProviderColor = strPtr("#FBD75BFF")
	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: stored}}

	batchMaster := basicWrite(t, "Standup")
	batchMaster.ExternalUID = "uid-1"
	batchMaster.Color = strPtr("#DC2127FF")
	batchMaster.ProviderColor = strPtr("#DC2127FF")
	changes := []DeltaSeriesChange{{ExternalUID: "uid-1", Master: &batchMaster}}

	result := ReconcileDelta(existing, changes, nil)

	if len(result.Upserts) != 1 {
		t.Fatalf("expected one upsert, got %+v", result.Upserts)
	}
	got := result.Upserts[0].Write
	if got.Color == nil || *got.Color != "#DC2127FF" {
		t.Fatalf("expected the untouched Master to follow the Provider's recolour, got %v", got.Color)
	}
}

func TestReconcileDelta_MasterRecolouredHereSurvivesProviderColourUnchanged(t *testing.T) {
	stored := basicWrite(t, "Standup")
	stored.ExternalUID = "uid-1"
	stored.Color = strPtr("#123456FF")
	stored.ProviderColor = strPtr("#FBD75BFF")
	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: stored}}

	// The Provider's own colour (#FBD75BFF) is unchanged from the shadow, but
	// the title changed — Google still resends the whole Master, colorId
	// included, on any Master-level edit.
	batchMaster := basicWrite(t, "Standup (renamed)")
	batchMaster.ExternalUID = "uid-1"
	batchMaster.Color = strPtr("#FBD75BFF")
	batchMaster.ProviderColor = strPtr("#FBD75BFF")
	changes := []DeltaSeriesChange{{ExternalUID: "uid-1", Master: &batchMaster}}

	result := ReconcileDelta(existing, changes, nil)

	if len(result.Upserts) != 1 {
		t.Fatalf("expected one upsert (the title alone changed), got %+v", result.Upserts)
	}
	got := result.Upserts[0].Write
	if got.Color == nil || *got.Color != "#123456FF" {
		t.Fatalf("expected the local recolour to survive an unchanged Provider colour, got %v", got.Color)
	}
}

func TestReconcileDelta_InstanceOnlyChangeFollowsProviderRecolourAgainstStoredOverride(t *testing.T) {
	recID := mustTime(t, "2026-01-12T09:00:00Z")
	stored := basicWrite(t, "Standup")
	stored.Rrule = "FREQ=WEEKLY"
	stored.ExternalUID = "uid-1"
	stored.Overrides = []OverrideWrite{{
		RecurrenceID: recID, Title: "Standup (moved)",
		Start: mustTime(t, "2026-01-12T10:00:00Z"), End: mustTime(t, "2026-01-12T10:30:00Z"),
		Color: strPtr("#123456FF"), ProviderColor: strPtr("#FBD75BFF"),
	}}
	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: stored}}

	changedInstance := OverrideWrite{
		RecurrenceID: recID, Title: "Standup (moved)",
		Start: mustTime(t, "2026-01-12T10:00:00Z"), End: mustTime(t, "2026-01-12T10:30:00Z"),
		ExternalUID: "uid-1", Color: strPtr("#DC2127FF"), ProviderColor: strPtr("#DC2127FF"),
	}
	changes := []DeltaSeriesChange{{ExternalUID: "uid-1", Overrides: []OverrideWrite{changedInstance}}}

	result := ReconcileDelta(existing, changes, nil)

	if len(result.Upserts) != 1 {
		t.Fatalf("expected one upsert, got %+v", result.Upserts)
	}
	got := result.Upserts[0].Write
	if len(got.Overrides) != 1 {
		t.Fatalf("expected 1 override, got %+v", got.Overrides)
	}
	if got.Overrides[0].Color == nil || *got.Overrides[0].Color != "#123456FF" {
		t.Fatalf("expected the Override's local recolour to survive the Provider's recolour, got %v", got.Overrides[0].Color)
	}
	if got.Overrides[0].ProviderColor == nil || *got.Overrides[0].ProviderColor != "#DC2127FF" {
		t.Fatalf("expected the Override's shadow to still move, got %v", got.Overrides[0].ProviderColor)
	}
}

// mergeDeltaChange must not mutate the stored ExistingSeries.Content it was
// built from — ReconcileDelta compares the two afterward.
func TestReconcileDelta_DoesNotMutateStoredContent(t *testing.T) {
	stored := basicWrite(t, "Standup")
	stored.Rrule = "FREQ=WEEKLY"
	stored.ExternalUID = "uid-1"
	stored.Exdates = []time.Time{mustTime(t, "2026-01-05T09:00:00Z")}
	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: stored}}

	changes := []DeltaSeriesChange{{ExternalUID: "uid-1", Cancellations: []time.Time{mustTime(t, "2026-01-12T09:00:00Z")}}}
	ReconcileDelta(existing, changes, nil)

	if len(existing[0].Content.Exdates) != 1 {
		t.Fatalf("stored content was mutated: Exdates now %+v", existing[0].Content.Exdates)
	}
}
