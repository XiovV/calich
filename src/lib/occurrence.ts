import type { Event } from "./event";
import { reanchorRule } from "./recurrencePresets";

/**
 * A single dated instance produced by expanding an Event's Recurrence rule over
 * a date window. Occurrences are computed, never stored; they are what the grid
 * actually renders. A non-recurring Event yields exactly one Occurrence.
 *
 * Identified by `(event.id, start)`, where `start` is the datetime the rule
 * produced — its iCalendar `RECURRENCE-ID`. See ADR-0016 and CONTEXT.md.
 */
export interface Occurrence {
  /**
   * The Event this instance renders. Usually the Master it was expanded from;
   * when this Occurrence has been overridden ("this event" edit), it's the
   * Override instead, so its own title/calendar/start/end are used.
   */
  event: Event;
  /** This instance's start (the `RECURRENCE-ID`). */
  start: Date;
  /** This instance's end (start + the Master's duration). */
  end: Date;
}

/** Stable identity for an Occurrence: `(event.id, occurrenceStart)`. */
export function occurrenceKey(occurrence: Occurrence): string {
  return `${occurrence.event.id}::${occurrence.start.toISOString()}`;
}

/**
 * Whether an Occurrence belongs to a recurring series — a Master with its own
 * rule, or an Override of one — and so should offer the This event/This and
 * following/All events scope picker on edit or delete (ADR-0016). A
 * non-recurring Event's lone Occurrence never does.
 */
export function isRecurringOccurrence(occurrence: Occurrence): boolean {
  return Boolean(occurrence.event.rrule || occurrence.event.parentId);
}

/**
 * The Master an Occurrence belongs to: the Occurrence's own Event when it's
 * already a Master (recurring or not), or its parent looked up by id when
 * it's an Override (ADR-0016). `null` if the Occurrence is an Override whose
 * parent isn't in `events` (e.g. the series was since deleted).
 */
export function resolveMaster(events: Event[], occurrence: Occurrence): Event | null {
  if (!occurrence.event.parentId) return occurrence.event;
  return events.find((event) => event.id === occurrence.event.parentId) ?? null;
}

/**
 * `master`'s changes for the "All events" scope, when `occurrence` (whichever
 * instance was actually dragged, resized, or opened for editing) moves to a
 * new `[newStart, newEnd)`. The whole series shifts by the *delta* `occurrence`
 * moved by, re-anchoring the rule to the new start so it never falls out of
 * sync (ADR-0016) — a non-recurring Event just moves, since it's its own
 * Master and only Occurrence.
 *
 * `master`'s own `start`/`end` are shifted by that delta, not replaced with
 * `newStart`/`newEnd` outright: `occurrence.start` only equals the Master's
 * own start for its very first (or an as-yet-unmoved) Occurrence. Replacing
 * it directly would re-anchor the Master's `DTSTART` to whichever Occurrence
 * happened to be edited, silently truncating every earlier Occurrence out of
 * the series. Taking `master` and `occurrence` as separate parameters also
 * covers editing an already-Overridden Occurrence with "All events": the
 * delta comes from how far the Override itself moved, applied to the actual
 * Master rather than the Override.
 *
 * The rule itself is only rebuilt when the edit actually lands on a
 * different weekday. `reanchorRule` only understands the fixed presets, so
 * rebuilding it on every edit — including a same-day resize that changes
 * nothing about *which* days repeat — would collapse a custom rule (e.g.
 * "every Mon/Wed/Fri", or an INTERVAL > 1) down to a single weekday.
 */
export function seriesEditChanges(
  master: Event,
  occurrence: Occurrence,
  newStart: Date,
  newEnd: Date,
): Pick<Event, "start" | "end" | "rrule"> {
  const startDeltaMs = newStart.getTime() - occurrence.start.getTime();
  const endDeltaMs = newEnd.getTime() - occurrence.end.getTime();
  const start = new Date(master.start.getTime() + startDeltaMs);
  const end = new Date(master.end.getTime() + endDeltaMs);
  const dayChanged = newStart.getDay() !== occurrence.start.getDay();
  const rrule = dayChanged ? reanchorRule(master.rrule, newStart) : master.rrule;
  return { start, end, rrule };
}
