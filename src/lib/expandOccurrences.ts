import { RRule } from "rrule";
import { addDays, differenceInCalendarDays } from "date-fns";
import type { Event } from "./event";
import type { Occurrence } from "./occurrence";
import { anchorZone, fromFloating, toFloating } from "./floatingTime";

/** Whether `[start, end)` overlaps the half-open window `[windowStart, windowEnd)`. */
function overlapsWindow(
  start: Date,
  end: Date,
  windowStart: Date,
  windowEnd: Date,
): boolean {
  return start < windowEnd && end > windowStart;
}

/** A Date's identity as an absolute instant, for keying exdates/overrides. */
function instantKey(date: Date): number {
  return date.getTime();
}

/** Every Override, grouped by parent id and keyed by the Occurrence it replaces. */
function indexOverrides(events: Event[]): Map<string, Map<number, Event>> {
  const byParent = new Map<string, Map<number, Event>>();
  for (const event of events) {
    if (!event.parentId || !event.recurrenceId) continue;
    let byRecurrenceId = byParent.get(event.parentId);
    if (!byRecurrenceId) {
      byRecurrenceId = new Map();
      byParent.set(event.parentId, byRecurrenceId);
    }
    byRecurrenceId.set(instantKey(event.recurrenceId), event);
  }
  return byParent;
}

/**
 * Expands each Master Event into the Occurrences that fall within
 * `[windowStart, windowEnd)`. A non-recurring Event is a series of one — it
 * yields a single Occurrence when it overlaps the window. A recurring Event's
 * rule is expanded over the window only, so an unbounded ("Never ends") series
 * is always safe: `rule.between` is bounded by the window and never enumerates
 * the whole infinite series.
 *
 * Overrides (rows with `parentId`/`recurrenceId`) are never expanded on their
 * own — they replace one Occurrence of their parent's series, so a matching
 * Occurrence renders the override's own fields instead of the master's
 * (`occurrence.event` becomes the override). Exceptions (`exdates` on the
 * master) drop their Occurrence entirely. See ADR-0016.
 *
 * Pure: depends only on its arguments — each Event's own Anchor zone drives
 * its wall-clock conversion, falling back to the ambient/Viewer zone only for
 * a Floating Event (no `tzid`, ADR-0019). Occurrences are returned unsorted.
 */
export function expandOccurrences(
  events: Event[],
  windowStart: Date,
  windowEnd: Date,
): Occurrence[] {
  const overridesByParent = indexOverrides(events);
  const occurrences: Occurrence[] = [];

  for (const event of events) {
    // Overrides are substituted in below, not expanded as their own series.
    if (event.parentId) continue;

    const exdateKeys = new Set((event.exdates ?? []).map(instantKey));
    const overridesByRecurrenceId = overridesByParent.get(event.id);
    const durationMs = event.end.getTime() - event.start.getTime();
    // All-day duration is measured in calendar days, not milliseconds, so a
    // recurring all-day Occurrence's end lands on the next whole date even
    // across a DST transition (ADR-0017).
    const durationDays = event.allDay
      ? differenceInCalendarDays(event.end, event.start)
      : 0;

    if (!event.rrule) {
      if (overlapsWindow(event.start, event.end, windowStart, windowEnd)) {
        occurrences.push({ event, start: event.start, end: event.end });
      }
      continue;
    }

    // Recurrence expands in the Event's Anchor zone — a named zone drives
    // DST-correct wall-clock recurrence, "Etc/UTC" expands with no DST, and
    // a Floating Event (no tzid) falls back to the Viewer zone, matching
    // pre-ADR-0019 behavior exactly. A nonexistent wall-clock (spring-forward
    // gap) shifts forward; an ambiguous one (fall-back overlap) resolves to
    // the earlier offset — date-fns-tz's defaults, applied in floatingTime's
    // fromFloating (ADR-0019).
    const zone = anchorZone(event.tzid);

    const rule = new RRule({
      ...RRule.parseString(event.rrule),
      dtstart: toFloating(event.start, zone),
    });

    // Look back by one duration so an Occurrence that starts just before the
    // window but spans into it is still captured, matching the non-recurring
    // overlap test above.
    const searchStart = new Date(windowStart.getTime() - durationMs);
    const floatingStarts = rule.between(
      toFloating(searchStart, zone),
      toFloating(windowEnd, zone),
      true,
    );

    for (const floatingStart of floatingStarts) {
      const start = fromFloating(floatingStart, zone);
      if (exdateKeys.has(instantKey(start))) continue;

      const override = overridesByRecurrenceId?.get(instantKey(start));
      if (override) {
        if (overlapsWindow(override.start, override.end, windowStart, windowEnd)) {
          occurrences.push({ event: override, start: override.start, end: override.end });
        }
        continue;
      }

      const end = event.allDay
        ? addDays(start, durationDays)
        : new Date(start.getTime() + durationMs);
      if (overlapsWindow(start, end, windowStart, windowEnd)) {
        occurrences.push({ event, start, end });
      }
    }
  }

  return occurrences;
}
