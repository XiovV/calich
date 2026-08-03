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
  /** The Master Event this instance was expanded from. */
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
 * The Event changes to apply when an Occurrence is moved/resized to a new
 * `[start, end)`. Interim behaviour (ADR-0016): editing a recurring Occurrence
 * edits the whole series (the "All events" path), re-anchoring the master's rule
 * to the new start so its rule never falls out of sync with its start. A
 * non-recurring Event just moves. This is the single place the interim policy
 * lives, so replacing it with the Tier-3 scope picker touches one function.
 */
export function seriesEditChanges(
  event: Event,
  start: Date,
  end: Date,
): Pick<Event, "start" | "end" | "rrule"> {
  return { start, end, rrule: reanchorRule(event.rrule, start) };
}
