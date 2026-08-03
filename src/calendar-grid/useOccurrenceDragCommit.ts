import { useCallback, useMemo, useState } from "react";
import { useEventsStore } from "../lib/eventsStore";
import { isRecurringOccurrence, seriesEditChanges, type Occurrence } from "../lib/occurrence";
import type { EditScope } from "../lib/recurrenceScope";

interface PendingDragEdit {
  occurrence: Occurrence;
  start: Date;
  end: Date;
}

/**
 * Commits a drag/resize's new `[start, end)` for an Occurrence (ADR-0016,
 * issue #37). A non-recurring Occurrence updates immediately, exactly as
 * before. A recurring Occurrence instead holds the change pending until a
 * scope is chosen: the store is never touched until then, so cancelling the
 * picker needs no revert — the Occurrence simply renders at its original
 * position once the drag preview stops.
 *
 * The returned object is itself memoized, changing identity only when
 * `pending` actually does — not on every render — so callers can depend on
 * it from a mousemove-driven effect without re-subscribing on every frame.
 */
export function useOccurrenceDragCommit() {
  const updateEvent = useEventsStore((state) => state.updateEvent);
  const editOccurrence = useEventsStore((state) => state.editOccurrence);
  const [pending, setPending] = useState<PendingDragEdit | null>(null);

  const commit = useCallback(
    (occurrence: Occurrence, start: Date, end: Date) => {
      if (isRecurringOccurrence(occurrence)) {
        setPending({ occurrence, start, end });
        return;
      }
      updateEvent(
        occurrence.event.id,
        seriesEditChanges(occurrence.event, occurrence, start, end),
      );
    },
    [updateEvent],
  );

  const confirmScope = useCallback(
    (scope: EditScope) => {
      if (!pending) return;
      const { occurrence, start, end } = pending;
      editOccurrence(occurrence, scope, {
        calendarId: occurrence.event.calendarId,
        title: occurrence.event.title,
        start,
        end,
      });
      setPending(null);
    },
    [editOccurrence, pending],
  );

  const cancel = useCallback(() => setPending(null), []);

  return useMemo(
    () => ({ isScopePickerOpen: pending !== null, commit, confirmScope, cancel }),
    [pending, commit, confirmScope, cancel],
  );
}
