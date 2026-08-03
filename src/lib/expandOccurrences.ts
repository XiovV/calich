import { RRule } from "rrule";
import type { Event } from "./event";
import type { Occurrence } from "./occurrence";
import { fromFloating, toFloating } from "./floatingTime";

/** Whether `[start, end)` overlaps the half-open window `[windowStart, windowEnd)`. */
function overlapsWindow(
  start: Date,
  end: Date,
  windowStart: Date,
  windowEnd: Date,
): boolean {
  return start < windowEnd && end > windowStart;
}

/**
 * Expands each Event into the Occurrences that fall within
 * `[windowStart, windowEnd)`. A non-recurring Event is a series of one — it
 * yields a single Occurrence when it overlaps the window. A recurring Event's
 * rule is expanded over the window only, so an unbounded ("Never ends") series
 * is always safe: `rule.between` is bounded by the window and never enumerates
 * the whole infinite series.
 *
 * Pure: depends only on its arguments (and the ambient timezone, for wall-clock
 * conversion). Occurrences are returned unsorted.
 */
export function expandOccurrences(
  events: Event[],
  windowStart: Date,
  windowEnd: Date,
): Occurrence[] {
  const occurrences: Occurrence[] = [];

  for (const event of events) {
    const durationMs = event.end.getTime() - event.start.getTime();

    if (!event.rrule) {
      if (overlapsWindow(event.start, event.end, windowStart, windowEnd)) {
        occurrences.push({ event, start: event.start, end: event.end });
      }
      continue;
    }

    const rule = new RRule({
      ...RRule.parseString(event.rrule),
      dtstart: toFloating(event.start),
    });

    // Look back by one duration so an Occurrence that starts just before the
    // window but spans into it is still captured, matching the non-recurring
    // overlap test above.
    const searchStart = new Date(windowStart.getTime() - durationMs);
    const floatingStarts = rule.between(
      toFloating(searchStart),
      toFloating(windowEnd),
      true,
    );

    for (const floatingStart of floatingStarts) {
      const start = fromFloating(floatingStart);
      const end = new Date(start.getTime() + durationMs);
      if (overlapsWindow(start, end, windowStart, windowEnd)) {
        occurrences.push({ event, start, end });
      }
    }
  }

  return occurrences;
}
