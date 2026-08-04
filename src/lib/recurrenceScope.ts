import { RRule } from "rrule";
import type { Event } from "./event";
import { anchorZone, toFloating } from "./floatingTime";

/**
 * Pure transforms for the Tier-3 scope picker (This event / This and
 * following / All events) — ADR-0016. Each function computes the field
 * changes for its scope; the caller (the events store) is responsible for
 * turning them into API calls and picking new ids.
 */

export type EditScope = "this" | "following" | "all";

export interface EventFieldChanges {
  calendarId: string;
  title: string;
  start: Date;
  end: Date;
  // Absent from a drag-only edit (allDay isn't draggable yet); the caller
  // falls back to the master's own allDay in that case.
  allDay?: boolean;
  // The Anchor zone: preserved from whatever Event this change derives from
  // — no picker exists to change it explicitly yet (ADR-0019).
  tzid?: string;
}

/** `changes`' allDay, falling back to `master`'s own when the edit didn't
 * touch it (a drag-only edit, which can't yet change allDay-ness). */
export function resolveAllDay(
  changes: Pick<EventFieldChanges, "allDay">,
  master: Event,
): boolean | undefined {
  return changes.allDay ?? master.allDay;
}

/**
 * "This event": the fields for a new Override that replaces one Occurrence of
 * `master`'s series, keyed to the Occurrence it replaces (its RECURRENCE-ID).
 * A complete standalone instance, not a diff.
 */
export function makeOverride(
  master: Event,
  occurrenceStart: Date,
  changes: EventFieldChanges,
): { parentId: string; recurrenceId: Date } & EventFieldChanges {
  // Picked field-by-field rather than spread: callers (the event modal) build
  // `changes` from form state that also carries `rrule` for the "all"
  // scope's own use, and an Override must never carry a rule of its own.
  return {
    parentId: master.id,
    recurrenceId: occurrenceStart,
    calendarId: changes.calendarId,
    title: changes.title,
    start: changes.start,
    end: changes.end,
    allDay: resolveAllDay(changes, master),
    // An Override inherits its master's Anchor zone — only an explicit zone
    // choice would change it, and no picker exists yet (ADR-0019).
    tzid: master.tzid,
  };
}

/**
 * "This event" (delete): cancels a single Occurrence of `master`'s series —
 * the rule still generates that slot, but it is suppressed from expansion.
 */
export function makeException(
  master: Event,
  occurrenceStart: Date,
): { parentId: string; occurrenceStart: Date } {
  return { parentId: master.id, occurrenceStart };
}

/** An RRULE `UNTIL` value in the floating frame: `YYYYMMDDTHHMMSSZ`. */
function untilFromFloating(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  const y = date.getUTCFullYear();
  const mo = pad(date.getUTCMonth() + 1);
  const d = pad(date.getUTCDate());
  const h = pad(date.getUTCHours());
  const mi = pad(date.getUTCMinutes());
  const s = pad(date.getUTCSeconds());
  return `${y}${mo}${d}T${h}${mi}${s}Z`;
}

/** `rrule` with any existing UNTIL/COUNT replaced by a new UNTIL. */
function withUntil(rrule: string, untilFloating: Date): string {
  const kept = rrule
    .split(";")
    .filter((part) => !part.startsWith("UNTIL=") && !part.startsWith("COUNT="));
  kept.push(`UNTIL=${untilFromFloating(untilFloating)}`);
  return kept.join(";");
}

/**
 * `master`'s rule, truncated with `UNTIL` so its last Occurrence is just
 * before `splitStart` — the "This and following" boundary shared by editing
 * (a new master picks up the remainder) and deleting (there is no remainder)
 * a series from a point on (ADR-0016).
 */
export function truncateSeriesBefore(master: Event, splitStart: Date): string {
  const zone = anchorZone(master.tzid);
  const floatingStart = toFloating(master.start, zone);
  const rule = new RRule({
    ...RRule.parseString(master.rrule ?? ""),
    dtstart: floatingStart,
  });

  const lastBeforeSplit = rule.before(toFloating(splitStart, zone), false);
  // No earlier Occurrence (the split lands on the very first one) — truncate
  // to just before the master's own start so it produces nothing.
  const untilFloating = lastBeforeSplit ?? new Date(floatingStart.getTime() - 1000);

  return withUntil(master.rrule ?? "", untilFloating);
}

export interface SplitResult {
  /** The old master, truncated so its last Occurrence is just before the split. */
  truncatedMaster: { rrule: string };
  /** A new master carrying the same recurrence pattern, anchored at the split. */
  newMaster: EventFieldChanges & { rrule: string | undefined };
  /** Overrides/Exceptions at-or-after this instant move to the new master. */
  reparentFromStart: Date;
}

/**
 * "This and following": truncates `master` with `UNTIL` at the last
 * Occurrence before `splitStart`, and carries the same rule into a new master
 * for the remainder (ADR-0016). `changes` apply only to the new master (and
 * everything after it) — the boundary for reparenting overrides/exceptions is
 * always `splitStart`, the replaced Occurrence's original start, even if
 * `changes.start` moves it to a different time.
 */
export function splitFollowing(
  master: Event,
  splitStart: Date,
  changes: EventFieldChanges,
): SplitResult {
  return {
    truncatedMaster: { rrule: truncateSeriesBefore(master, splitStart) },
    // The new master carries the same Anchor zone as the old one — an
    // explicit zone choice would change it, and no picker exists yet
    // (ADR-0019).
    newMaster: {
      ...changes,
      allDay: resolveAllDay(changes, master),
      rrule: master.rrule,
      tzid: master.tzid,
    },
    reparentFromStart: splitStart,
  };
}

/** `rrule` with any UNTIL/COUNT end condition removed. */
function stripEndCondition(rrule: string): string {
  return rrule
    .split(";")
    .filter((part) => !part.startsWith("UNTIL=") && !part.startsWith("COUNT="))
    .join(";");
}

/**
 * "All events" forces a discard of existing Overrides/Exceptions when the
 * rule's *pattern* changes (or is added/removed) — their recurrence ids may
 * no longer be generated by the new rule (ADR-0016). Ignores the end
 * condition (UNTIL/COUNT): shortening or extending a series doesn't change
 * how earlier Occurrences are generated, so it doesn't invalidate their
 * Overrides/Exceptions. `undefined` and `""` both mean "does not recur".
 */
export function shouldDiscardChildren(
  oldRrule: string | undefined,
  newRrule: string | undefined,
): boolean {
  return stripEndCondition(oldRrule ?? "") !== stripEndCondition(newRrule ?? "");
}
