package service

import "testing"

// TestProtectPendingWriteBacks_FullMode_StaleFetchNeverOverwritesOrTombstonesThePendingSeries
// is #290's own ADR-0076 table test — the multi-agent review's most serious
// finding: without this, a Full Refresh landing between "the local edit
// committed" and "the push actually reached the Provider" would silently
// revert the user's own not-yet-pushed edit back to the Provider's stale
// copy, since ReconcileSeries has no notion of "ours is newer" on its own.
// Two scenarios in one series, since a real interleaving could land either
// way depending on whether the batch still lists the changed series:
//   - present in incoming, with the Provider's stale content: must produce
//     neither an Upsert (which would revert the edit) nor a NoOp (which
//     would silently discard the difference as coincidence).
//   - a second, unrelated series with no pending push: reconciles normally,
//     proving the protection is scoped to the one row, not every row.
func TestProtectPendingWriteBacks_FullMode_StaleFetchNeverOverwritesOrTombstonesThePendingSeries(t *testing.T) {
	pendingWrite := basicWrite(t, "Standup (edited locally, not yet pushed)")
	existing := []ExistingSeries{
		{MasterID: "master-pending", ExternalUID: "uid-pending", Content: pendingWrite},
		{MasterID: "master-ordinary", ExternalUID: "uid-ordinary", Content: basicWrite(t, "Retro")},
	}
	incoming := []IncomingSeries{
		// The Provider's still-stale copy of the pending series — what a
		// Full Refresh's fetch returns before the queued push ever reaches it.
		{ExternalUID: "uid-pending", Write: basicWrite(t, "Standup (stale provider copy)")},
		{ExternalUID: "uid-ordinary", Write: basicWrite(t, "Retro (renamed at the provider)")},
	}
	pendingMasterIDs := map[string]bool{"master-pending": true}

	filteredIncoming, unparseable := protectPendingWriteBacks(existing, incoming, nil, pendingMasterIDs)

	result := ReconcileSeries(existing, filteredIncoming, unparseable)

	for _, u := range result.Upserts {
		if u.MasterID == "master-pending" {
			t.Fatalf("expected the pending series never upserted from the provider's stale copy, got %+v", result.Upserts)
		}
	}
	for _, tomb := range result.Tombstones {
		if tomb == "master-pending" {
			t.Fatalf("expected the pending series never tombstoned, got %+v", result.Tombstones)
		}
	}
	if result.SkippedCount != 1 {
		t.Fatalf("expected the pending series counted as skipped (present but protected), got %d", result.SkippedCount)
	}

	// The unrelated series must still reconcile normally — protection is
	// scoped to the one pending row.
	if len(result.Upserts) != 1 || result.Upserts[0].MasterID != "master-ordinary" {
		t.Fatalf("expected the unrelated series upserted normally, got %+v", result.Upserts)
	}
}

// TestProtectPendingWriteBacks_FullMode_NoPendingWriteBacksIsANoOp covers the
// overwhelmingly common case — a Subscription, or a Connection Calendar with
// nothing queued — where protectPendingWriteBacks must return its inputs
// completely unchanged, not merely functionally equivalent copies, so every
// existing caller's behaviour is untouched.
func TestProtectPendingWriteBacks_FullMode_NoPendingWriteBacksIsANoOp(t *testing.T) {
	existing := []ExistingSeries{{MasterID: "master-1", ExternalUID: "uid-1", Content: basicWrite(t, "Standup")}}
	incoming := []IncomingSeries{{ExternalUID: "uid-1", Write: basicWrite(t, "Standup (renamed)")}}

	filteredIncoming, unparseable := protectPendingWriteBacks(existing, incoming, nil, nil)

	if len(filteredIncoming) != 1 {
		t.Fatalf("expected incoming untouched, got %+v", filteredIncoming)
	}
	if unparseable != nil {
		t.Fatalf("expected unparseable untouched (nil), got %+v", unparseable)
	}
}

// TestProtectPendingWriteBacksDelta_StaleBatchNeverOverwritesThePendingSeries
// is the Delta-mode sibling: a Delta Refresh whose batch still carries the
// Provider's stale copy of a series with a pending push must drop that one
// change from the batch entirely, leaving an unrelated change in the same
// batch untouched.
func TestProtectPendingWriteBacksDelta_StaleBatchNeverOverwritesThePendingSeries(t *testing.T) {
	pendingWrite := basicWrite(t, "Standup (edited locally, not yet pushed)")
	existing := []ExistingSeries{
		{MasterID: "master-pending", ExternalUID: "uid-pending", Content: pendingWrite},
		{MasterID: "master-ordinary", ExternalUID: "uid-ordinary", Content: basicWrite(t, "Retro")},
	}
	staleWrite := basicWrite(t, "Standup (stale provider copy)")
	renamedWrite := basicWrite(t, "Retro (renamed at the provider)")
	changes := []DeltaSeriesChange{
		{ExternalUID: "uid-pending", Master: &staleWrite},
		{ExternalUID: "uid-ordinary", Master: &renamedWrite},
	}
	pendingMasterIDs := map[string]bool{"master-pending": true}

	filtered := protectPendingWriteBacksDelta(existing, changes, pendingMasterIDs)

	result := ReconcileDelta(existing, filtered, nil)

	for _, u := range result.Upserts {
		if u.MasterID == "master-pending" {
			t.Fatalf("expected the pending series never upserted from the provider's stale batch, got %+v", result.Upserts)
		}
	}
	if len(result.Upserts) != 1 || result.Upserts[0].MasterID != "master-ordinary" {
		t.Fatalf("expected the unrelated change to reconcile normally, got %+v", result.Upserts)
	}
}

// TestProtectPendingWriteBacksDelta_NoPendingWriteBacksIsANoOp mirrors Full
// mode's own no-op case.
func TestProtectPendingWriteBacksDelta_NoPendingWriteBacksIsANoOp(t *testing.T) {
	write := basicWrite(t, "Standup (renamed)")
	changes := []DeltaSeriesChange{{ExternalUID: "uid-1", Master: &write}}

	filtered := protectPendingWriteBacksDelta(nil, changes, nil)

	if len(filtered) != 1 {
		t.Fatalf("expected changes untouched, got %+v", filtered)
	}
}
