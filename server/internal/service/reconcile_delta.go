// reconcile_delta.go implements Delta Refresh's reconciliation (#288,
// ADR-0053): a pure diff between what a Linked Calendar already stores and
// the small set of series a Provider reported as changed since a cursor.
//
// The rule this file exists to make structurally true: **in Delta mode,
// absence never means deletion.** A Provider's incremental response omits
// every unchanged series — the overwhelming majority — so pointing Full
// mode's reconciler (ReconcileSeries, which tombstones every stored series
// the input doesn't mention) at this input would wipe the entire Calendar
// on the first cycle. ReconcileDelta has no loop over stored-but-unmentioned
// series and no `seen` set; the only path to a Tombstone is an ExternalUID
// the Provider *explicitly* listed as deleted. That is ADR-0053's "tombstone
// -by-absence must be structurally unreachable, not merely switched off" —
// the function that computes absence is simply not in scope here.
package service

import "time"

// protectPendingWriteBacksDelta drops every batch change a Pending Write-back
// protects (#290, #292, ADR-0076, ADR-0077) — Delta mode's half of the same
// protection protectPendingWriteBacks gives Full mode, and simpler: a
// dropped change cannot cause a wrongful tombstone (ReconcileDelta reaches
// that only via the explicit deletions list, ADR-0053), so there is no
// unparseable-style bucket to also populate. Two pending sets:
//
//   - pendingMasterIDs: a stored series with a queued edit — its stale
//     batch copy would overwrite the not-yet-pushed local edit.
//   - pendingDeleteUIDs: a Master deleted here whose events.delete hasn't
//     drained. Its local row is gone, so a batch update for it would land in
//     ReconcileDelta's "no stored counterpart → new series" branch and
//     resurrect the Event the User deleted (ADR-0077).
//
// Both are empty for every Subscription (Delta and Write-back are both
// Connection-only), so this is a no-op pass-through there.
func protectPendingWriteBacksDelta(existing []ExistingSeries, changes []DeltaSeriesChange, pendingMasterIDs, pendingDeleteUIDs map[string]bool) []DeltaSeriesChange {
	if len(pendingMasterIDs) == 0 && len(pendingDeleteUIDs) == 0 {
		return changes
	}

	protectedUIDs := make(map[string]bool, len(pendingMasterIDs))
	for _, e := range existing {
		if pendingMasterIDs[e.MasterID] {
			protectedUIDs[e.ExternalUID] = true
		}
	}
	if len(protectedUIDs) == 0 && len(pendingDeleteUIDs) == 0 {
		return changes
	}

	filtered := make([]DeltaSeriesChange, 0, len(changes))
	for _, c := range changes {
		if protectedUIDs[c.ExternalUID] || pendingDeleteUIDs[c.ExternalUID] {
			continue
		}
		filtered = append(filtered, c)
	}
	return filtered
}

// ReconcileDelta diffs a Linked Calendar's stored series against one Delta
// Refresh batch (#288, ADR-0053):
//
//   - changes: series the Provider reported as touched. A change whose
//     ExternalUID is already stored is merged onto the stored series (Master
//     -level fields overlaid, changed instances added or replaced, stored
//     instances the batch didn't mention kept) and upserted in place, or
//     counted as a no-op when the merge changes nothing. A change with a
//     Master and no stored counterpart is a new series. A change that is
//     only instances of a series this Calendar doesn't store is skipped and
//     counted — present-but-unmappable, never an absence.
//   - deletions: ExternalUIDs the Provider explicitly stated are gone
//     (a top-level status=cancelled). The only source of a Tombstone.
func ReconcileDelta(existing []ExistingSeries, changes []DeltaSeriesChange, deletions []string) ReconcileResult {
	existingByUID := make(map[string]ExistingSeries, len(existing))
	for _, e := range existing {
		existingByUID[e.ExternalUID] = e
	}

	var result ReconcileResult
	for _, change := range changes {
		e, ok := existingByUID[change.ExternalUID]
		if !ok {
			if change.Master == nil {
				result.SkippedCount++
				continue
			}
			result.Upserts = append(result.Upserts, SeriesUpsert{Write: *change.Master})
			continue
		}

		merged := mergeDeltaChange(e.Content, change)
		if seriesContentEqual(e.Content, merged) {
			result.NoOpCount++
			continue
		}
		result.Upserts = append(result.Upserts, SeriesUpsert{MasterID: e.MasterID, Write: merged})
	}

	for _, uid := range deletions {
		if e, ok := existingByUID[uid]; ok {
			result.Tombstones = append(result.Tombstones, e.MasterID)
		}
	}

	return result
}

// mergeDeltaChange applies one DeltaSeriesChange onto a copy of the stored
// series it targets. A batch Master overlays every Master-level field (a
// recurrence-rule or title edit at the Provider) and merges the instances
// Google sent alongside it; instance-only changes add or replace Overrides
// and Exdates by RECURRENCE-ID. Stored Overrides and Exdates the batch never
// mentions survive untouched — Google does not re-send an unchanged
// exception when only the Master changed, so replacing wholesale would lose
// them.
func mergeDeltaChange(base SeriesWrite, change DeltaSeriesChange) SeriesWrite {
	merged := cloneSeriesWrite(base)

	if m := change.Master; m != nil {
		merged.Title = m.Title
		merged.Description = m.Description
		merged.Location = m.Location
		merged.URL = m.URL
		merged.Start = m.Start
		merged.End = m.End
		merged.AllDay = m.AllDay
		merged.Tzid = m.Tzid
		merged.Rrule = m.Rrule
		merged.ProviderEtag = m.ProviderEtag
		merged.RSVPStatus = m.RSVPStatus
		merged.ConferenceURL = m.ConferenceURL
		merged.GuestCount = m.GuestCount
		// #289, ADR-0075: resolved against base (the stored Master this
		// batch is overlaying), not merged — merged.Color/ProviderColor at
		// this point are still base's own, cloned verbatim by
		// cloneSeriesWrite, so reading either as "currentDisplay" would be
		// identical either way; base is used for clarity.
		merged.Color, merged.ProviderColor = resolveFollowedEventColor(base.Color, base.ProviderColor, m.ProviderColor)
		merged = applyDeltaOverrides(merged, m.Overrides)
		merged = applyDeltaCancellations(merged, m.Exdates)
	}

	merged = applyDeltaOverrides(merged, change.Overrides)
	merged = applyDeltaCancellations(merged, change.Cancellations)
	return merged
}

// applyDeltaOverrides adds or replaces each override by RECURRENCE-ID, and
// drops any Exdate at the same instant — a re-modified occurrence is no
// longer a cancelled one, and keeping both would leave the merged series
// self-contradictory. An override that replaces one already in w has its
// colour resolved against that stored one first (#289, ADR-0075's
// until-touched rule) — o.Color/ProviderColor arrive equal, the Provider's
// current colour verbatim, exactly as the mapper seeds a brand new instance;
// a stored instance the batch never mentioned isn't touched here at all, so
// it needs no such resolution.
func applyDeltaOverrides(w SeriesWrite, overrides []OverrideWrite) SeriesWrite {
	existingByRecurrenceID := overridesByRecurrenceID(w.Overrides)

	for _, o := range overrides {
		if match, ok := existingByRecurrenceID[o.RecurrenceID.UnixNano()]; ok {
			o.Color, o.ProviderColor = resolveFollowedEventColor(match.Color, match.ProviderColor, o.ProviderColor)
		}

		kept := w.Overrides[:0:0]
		for _, existing := range w.Overrides {
			if !existing.RecurrenceID.Equal(o.RecurrenceID) {
				kept = append(kept, existing)
			}
		}
		w.Overrides = append(kept, o)
		w.Exdates = removeInstant(w.Exdates, o.RecurrenceID)
	}
	return w
}

// applyDeltaCancellations adds each cancellation instant not already present
// and drops any Override at the same instant — the mirror of
// applyDeltaOverrides.
func applyDeltaCancellations(w SeriesWrite, cancellations []time.Time) SeriesWrite {
	for _, c := range cancellations {
		if !containsInstant(w.Exdates, c) {
			w.Exdates = append(w.Exdates, c)
		}
		kept := w.Overrides[:0:0]
		for _, existing := range w.Overrides {
			if !existing.RecurrenceID.Equal(c) {
				kept = append(kept, existing)
			}
		}
		w.Overrides = kept
	}
	return w
}

func containsInstant(instants []time.Time, t time.Time) bool {
	for _, i := range instants {
		if i.Equal(t) {
			return true
		}
	}
	return false
}

func removeInstant(instants []time.Time, t time.Time) []time.Time {
	kept := instants[:0:0]
	for _, i := range instants {
		if !i.Equal(t) {
			kept = append(kept, i)
		}
	}
	return kept
}

// cloneSeriesWrite copies w with fresh backing arrays for the slices
// mergeDeltaChange mutates, so the stored ExistingSeries.Content it was
// built from is never disturbed — ReconcileDelta compares the two afterward
// to decide upsert vs no-op.
func cloneSeriesWrite(w SeriesWrite) SeriesWrite {
	clone := w
	if w.Exdates != nil {
		clone.Exdates = append([]time.Time(nil), w.Exdates...)
	}
	if w.Overrides != nil {
		clone.Overrides = append([]OverrideWrite(nil), w.Overrides...)
	}
	return clone
}
