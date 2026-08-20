import { addDays, startOfDay } from "date-fns";
import type { Occurrence } from "./occurrence";

/**
 * One day's worth of a (possibly midnight-crossing) Occurrence: the
 * Occurrence's own `[start, end)` clipped to that day's `[00:00, 24:00)`
 * bounds, so a grid column never has to render past its own midnight.
 */
export interface OccurrenceDaySegment {
  occurrence: Occurrence;
  start: Date;
  end: Date;
}

/** Whether any part of `occurrence`'s span falls within `day`. */
export function occurrenceIntersectsDay(occurrence: Occurrence, day: Date): boolean {
  const dayStart = startOfDay(day);
  const dayEnd = addDays(dayStart, 1);
  return occurrence.start < dayEnd && occurrence.end > dayStart;
}

/**
 * `occurrence`'s span clipped to `day`'s bounds. Only meaningful when
 * `occurrenceIntersectsDay` is true for the same pair — callers that already
 * filter with it get a segment with positive duration back.
 */
export function occurrenceSegmentForDay(
  occurrence: Occurrence,
  day: Date,
): OccurrenceDaySegment {
  const dayStart = startOfDay(day);
  const dayEnd = addDays(dayStart, 1);
  return {
    occurrence,
    start: occurrence.start < dayStart ? dayStart : occurrence.start,
    end: occurrence.end > dayEnd ? dayEnd : occurrence.end,
  };
}

/**
 * Every `occurrences` segment that touches `day` — one per Occurrence that
 * intersects it, clipped to its bounds. An Occurrence spanning midnight
 * yields a segment on each day it covers (issue #230): a same-day Occurrence
 * gets one segment equal to its own `[start, end)`, a midnight-crossing one
 * gets a segment per day it touches, clipped at whichever edges fall outside
 * that day.
 */
export function getDaySegments(
  occurrences: Occurrence[],
  day: Date,
): OccurrenceDaySegment[] {
  return occurrences
    .filter((occurrence) => occurrenceIntersectsDay(occurrence, day))
    .map((occurrence) => occurrenceSegmentForDay(occurrence, day));
}
