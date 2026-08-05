import type { Event } from "./event";
import { eventsApi } from "./eventsApi";
import type { Occurrence } from "./occurrence";
import { seriesEditChanges } from "./occurrence";
import {
  makeException,
  makeOverride,
  resolveAllDay,
  resolveDescription,
  resolveLocation,
  resolveReminders,
  shouldDiscardChildren,
  splitFollowing,
  truncateSeriesBefore,
  type EditScope,
  type EventFieldChanges,
} from "./recurrenceScope";

/** `EventFieldChanges` plus the rule — the shape a master's own row carries,
 * as opposed to an Override's (which never has one). */
export type MasterFieldChanges = EventFieldChanges & { rrule?: string };

/** A master's own identifying fields, kept unchanged while only its rule
 * moves — carried by ops that truncate a series without replacing it
 * (`reanchorSeries`, `truncateSeries`). Includes `tzid` so truncating never
 * silently clears the Anchor zone (ADR-0019), and `reminders` so it never
 * silently clears the master's own Reminders either (ADR-0020) — the update
 * API replaces Reminders wholesale, so an omitted field would wipe them. */
type MasterCoreFields = Pick<
  Event,
  "calendarId" | "title" | "start" | "end" | "tzid" | "reminders" | "description" | "location"
>;

/**
 * A scoped edit (This event/This and following/All events) compiles down to
 * an ordered list of these — the seam between the pure scope algebra
 * (`recurrenceScope`) and its two realizations, `applySeriesOps` (cache) and
 * `dispatchSeriesOps` (API), so the two can never drift out of sync
 * (ADR-0016, issue #40).
 */
export type SeriesOp =
  /** Replaces one Occurrence of a series with its own standalone row —
   * "This event": a create when `isNew`, an in-place update otherwise
   * (an edit of an Occurrence that's already an Override). */
  | {
      kind: "overrideOccurrence";
      id: string;
      isNew: boolean;
      parentId: string;
      recurrenceId: Date;
      fields: EventFieldChanges;
    }
  /** Rewrites a single Event row wholesale — "All events": the master's own
   * fields and rule. `discardChildren` is cache-only (ADR-0016: a rule-*
   * pattern* change invalidates existing Overrides/Exceptions' recurrence
   * ids; nothing is dispatched to the API for them, they just stop being
   * reachable from the new rule). */
  | {
      kind: "putEvent";
      id: string;
      fields: MasterFieldChanges;
      discardChildren: boolean;
    }
  /** Re-anchors a series at a split point — "This and following": the old
   * master is truncated with `UNTIL`, a new master carries the remainder,
   * and Overrides/Exceptions at-or-after the split move to it. */
  | {
      kind: "reanchorSeries";
      oldMasterId: string;
      oldMasterFields: MasterCoreFields;
      truncatedRrule: string;
      keptExdates: Date[];
      newMasterId: string;
      newMaster: MasterFieldChanges;
      movedExdates: Date[];
      reparentFromStart: Date;
    }
  /** Cancels a single Occurrence — "This event" delete of an as-yet-unmodified
   * Occurrence: the rule still generates that slot, but it's suppressed from
   * expansion (iCalendar EXDATE, ADR-0016). */
  | {
      kind: "cancelOccurrence";
      parentId: string;
      occurrenceStart: Date;
    }
  /** Drops an Event row and any local children — "All events" delete (the
   * master and its Overrides/Exceptions), and also "This event" delete of an
   * Occurrence that's already an Override (which never has children of its
   * own, so the filter is a no-op there). The backend cascades children on
   * delete (`ON DELETE CASCADE`); nothing else is dispatched for them. */
  | {
      kind: "removeSeries";
      id: string;
    }
  /** Truncates a series from a split point on, with no remainder master —
   * "This and following" delete: shortens the rule with `UNTIL` and drops
   * Overrides/Exceptions at-or-after the split, unlike `reanchorSeries`
   * which carries them into a new master instead. */
  | {
      kind: "truncateSeries";
      masterId: string;
      masterFields: MasterCoreFields;
      truncatedRrule: string;
      keptExdates: Date[];
      orphanedIds: string[];
    };

/** Splits `dates` into [before boundary, at-or-after boundary]. */
export function partitionByBoundary(dates: Date[], boundary: Date): [Date[], Date[]] {
  const before: Date[] = [];
  const atOrAfter: Date[] = [];
  for (const date of dates) {
    (date >= boundary ? atOrAfter : before).push(date);
  }
  return [before, atOrAfter];
}

export interface PlanEditOccurrenceInput {
  master: Event;
  occurrence: Occurrence;
  /** Whether `occurrence` is itself an Override (its own row), rather than
   * an as-yet-unmodified expansion of `master`. */
  isOverride: boolean;
  /** The Occurrence's original start (RECURRENCE-ID) — distinct from
   * `occurrence.start` when it's already an Override that's been moved. */
  originalStart: Date;
  scope: EditScope;
  changes: MasterFieldChanges;
}

/**
 * Pure planner: computes the ordered list of Series operations for a scoped
 * edit, across all three scopes — including the trivial "this event" edit of
 * an existing Override, which goes through `overrideOccurrence` (an in-place
 * update) rather than a plain event update, so it stays governed by the same
 * algebra as every other scope.
 *
 * Assigns ids for any new master/override rows itself via `newId` (defaults
 * to `crypto.randomUUID`; tests can inject a deterministic generator).
 */
export function planEditOccurrence(
  { master, occurrence, isOverride, originalStart, scope, changes }: PlanEditOccurrenceInput,
  newId: () => string = () => crypto.randomUUID(),
): SeriesOp[] {
  if (scope === "all") {
    // The whole series shifts by the delta `occurrence` moved by (see
    // seriesEditChanges) — this covers both a drag (an explicit new
    // date+time) and the modal (a time-only edit, so the delta has no day
    // component and the master's own date survives untouched).
    const seriesChanges = seriesEditChanges(master, occurrence, changes.start, changes.end);
    // "rrule" may be present-but-undefined (the user picked "Does not
    // repeat"), which must win over falling back to seriesChanges' own
    // reanchored rule — hence an `in` check rather than `??`.
    const rrule = "rrule" in changes ? changes.rrule : seriesChanges.rrule;
    return [
      {
        kind: "putEvent",
        id: master.id,
        fields: {
          calendarId: changes.calendarId,
          title: changes.title,
          start: seriesChanges.start,
          end: seriesChanges.end,
          allDay: resolveAllDay(changes, master),
          rrule,
          // Preserved unchanged — no picker exists to change it (ADR-0019).
          tzid: master.tzid,
          reminders: resolveReminders(changes, master),
          description: resolveDescription(changes, master),
          location: resolveLocation(changes, master),
        },
        discardChildren: shouldDiscardChildren(master.rrule, rrule),
      },
    ];
  }

  if (scope === "following") {
    const { truncatedMaster, newMaster, reparentFromStart } = splitFollowing(
      master,
      originalStart,
      changes,
    );
    const [keptExdates, movedExdates] = partitionByBoundary(
      master.exdates ?? [],
      reparentFromStart,
    );
    return [
      {
        kind: "reanchorSeries",
        oldMasterId: master.id,
        oldMasterFields: {
          calendarId: master.calendarId,
          title: master.title,
          start: master.start,
          end: master.end,
          tzid: master.tzid,
          reminders: master.reminders,
          description: master.description,
          location: master.location,
        },
        truncatedRrule: truncatedMaster.rrule,
        keptExdates,
        newMasterId: newId(),
        newMaster,
        movedExdates,
        reparentFromStart,
      },
    ];
  }

  // scope === "this"
  if (isOverride) {
    return [
      {
        kind: "overrideOccurrence",
        id: occurrence.event.id,
        isNew: false,
        parentId: master.id,
        // occurrence.event is itself the Override; its recurrenceId is the
        // Occurrence it replaces and never changes on a "this event" edit.
        recurrenceId: occurrence.event.recurrenceId!,
        fields: {
          calendarId: changes.calendarId,
          title: changes.title,
          start: changes.start,
          end: changes.end,
          allDay: resolveAllDay(changes, master),
          // Preserved unchanged — no picker exists to change it (ADR-0019).
          tzid: occurrence.event.tzid,
          // Editing an existing Override's Reminders only affects its own
          // row, never the rest of the series (ADR-0020).
          reminders: resolveReminders(changes, occurrence.event),
          description: resolveDescription(changes, occurrence.event),
          location: resolveLocation(changes, occurrence.event),
        },
      },
    ];
  }

  const override = makeOverride(master, originalStart, changes);
  return [
    {
      kind: "overrideOccurrence",
      id: newId(),
      isNew: true,
      parentId: override.parentId,
      recurrenceId: override.recurrenceId,
      fields: {
        calendarId: override.calendarId,
        title: override.title,
        start: override.start,
        end: override.end,
        allDay: override.allDay,
        tzid: override.tzid,
        reminders: override.reminders,
        description: override.description,
        location: override.location,
      },
    },
  ];
}

export interface PlanDeleteOccurrenceInput {
  /** The full local cache — unlike `planEditOccurrence`, a "following"
   * delete needs to know which existing rows are orphaned by the split so
   * it can plan their removal by id. */
  events: Event[];
  master: Event;
  occurrence: Occurrence;
  isOverride: boolean;
  originalStart: Date;
  scope: EditScope;
}

/**
 * Pure planner: computes the ordered list of Series operations for a scoped
 * delete, across all three scopes — including the trivial "this event"
 * delete of an existing Override, which goes through `removeSeries` rather
 * than a plain event removal, so it stays governed by the same algebra as
 * every other scope (issue #41).
 */
export function planDeleteOccurrence({
  events,
  master,
  occurrence,
  isOverride,
  originalStart,
  scope,
}: PlanDeleteOccurrenceInput): SeriesOp[] {
  if (scope === "all") {
    return [{ kind: "removeSeries", id: master.id }];
  }

  if (scope === "following") {
    const truncatedRrule = truncateSeriesBefore(master, originalStart);
    const orphaned = events.filter(
      (event) =>
        event.parentId === master.id &&
        event.recurrenceId &&
        event.recurrenceId >= originalStart,
    );
    const [keptExdates] = partitionByBoundary(master.exdates ?? [], originalStart);
    return [
      {
        kind: "truncateSeries",
        masterId: master.id,
        masterFields: {
          calendarId: master.calendarId,
          title: master.title,
          start: master.start,
          end: master.end,
          tzid: master.tzid,
          reminders: master.reminders,
          description: master.description,
          location: master.location,
        },
        truncatedRrule,
        keptExdates,
        orphanedIds: orphaned.map((event) => event.id),
      },
    ];
  }

  // scope === "this"
  if (isOverride) {
    return [{ kind: "removeSeries", id: occurrence.event.id }];
  }

  const exception = makeException(master, originalStart);
  return [
    {
      kind: "cancelOccurrence",
      parentId: exception.parentId,
      occurrenceStart: exception.occurrenceStart,
    },
  ];
}

/** Cache projection: the next `Event[]` after applying an ordered list of
 * Series operations (ADR-0016, issue #40). */
export function applySeriesOps(events: Event[], ops: SeriesOp[]): Event[] {
  return ops.reduce(applySeriesOp, events);
}

function applySeriesOp(events: Event[], op: SeriesOp): Event[] {
  switch (op.kind) {
    case "overrideOccurrence": {
      const overrideEvent: Event = {
        id: op.id,
        parentId: op.parentId,
        recurrenceId: op.recurrenceId,
        ...op.fields,
      };
      return op.isNew
        ? [...events, overrideEvent]
        : events.map((event) => (event.id === op.id ? overrideEvent : event));
    }

    case "putEvent": {
      return events
        .map((event) => (event.id === op.id ? { ...event, ...op.fields } : event))
        .filter((event) => !op.discardChildren || event.parentId !== op.id);
    }

    case "reanchorSeries": {
      const createdNewMaster: Event = {
        id: op.newMasterId,
        ...op.newMaster,
        exdates: op.movedExdates,
      };
      return events
        .map((event) =>
          event.id === op.oldMasterId
            ? { ...event, rrule: op.truncatedRrule, exdates: op.keptExdates }
            : event,
        )
        .concat(createdNewMaster)
        .map((event) =>
          event.parentId === op.oldMasterId &&
          event.recurrenceId &&
          event.recurrenceId >= op.reparentFromStart
            ? { ...event, parentId: op.newMasterId }
            : event,
        );
    }

    case "cancelOccurrence": {
      return events.map((event) =>
        event.id === op.parentId
          ? { ...event, exdates: [...(event.exdates ?? []), op.occurrenceStart] }
          : event,
      );
    }

    case "removeSeries": {
      return events.filter((event) => event.id !== op.id && event.parentId !== op.id);
    }

    case "truncateSeries": {
      return events
        .map((event) =>
          event.id === op.masterId
            ? { ...event, rrule: op.truncatedRrule, exdates: op.keptExdates }
            : event,
        )
        .filter((event) => !op.orphanedIds.includes(event.id));
    }
  }
}

/** API projection: dispatches an ordered list of Series operations to the
 * existing endpoints, in order (e.g. the following-split fires truncate →
 * create → reparent), relying on the backend `ON DELETE CASCADE` for
 * children (ADR-0016, issue #40). */
export async function dispatchSeriesOps(accessToken: string, ops: SeriesOp[]): Promise<void> {
  for (const op of ops) {
    await dispatchSeriesOp(accessToken, op);
  }
}

async function dispatchSeriesOp(accessToken: string, op: SeriesOp): Promise<void> {
  switch (op.kind) {
    case "overrideOccurrence": {
      if (op.isNew) {
        await eventsApi.create(accessToken, {
          id: op.id,
          parentId: op.parentId,
          recurrenceId: op.recurrenceId,
          ...op.fields,
        });
      } else {
        // An Override never carries a rule of its own (ADR-0016).
        await eventsApi.update(accessToken, op.id, { ...op.fields, rrule: undefined });
      }
      return;
    }

    case "putEvent": {
      await eventsApi.update(accessToken, op.id, op.fields);
      return;
    }

    case "reanchorSeries": {
      await eventsApi.update(accessToken, op.oldMasterId, {
        ...op.oldMasterFields,
        rrule: op.truncatedRrule,
      });
      await eventsApi.create(accessToken, { id: op.newMasterId, ...op.newMaster });
      await eventsApi.reparentSeries(
        accessToken,
        op.oldMasterId,
        op.newMasterId,
        op.reparentFromStart,
      );
      return;
    }

    case "cancelOccurrence": {
      await eventsApi.addException(accessToken, op.parentId, op.occurrenceStart);
      return;
    }

    case "removeSeries": {
      await eventsApi.remove(accessToken, op.id);
      return;
    }

    case "truncateSeries": {
      await eventsApi.update(accessToken, op.masterId, {
        ...op.masterFields,
        rrule: op.truncatedRrule,
      });
      for (const id of op.orphanedIds) {
        await eventsApi.remove(accessToken, id);
      }
      return;
    }
  }
}
