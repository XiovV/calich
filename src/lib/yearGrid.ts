import { format } from "date-fns";
import type { Occurrence } from "./occurrence";

const MONTHS_PER_YEAR = 12;

/** Stable per-day key (local calendar date) used to mark Event presence. */
export function dayKey(date: Date): string {
  return format(date, "yyyy-MM-dd");
}

/** The twelve month-start dates (Jan..Dec) for the year of `selectedDate`. */
export function buildYearMonths(selectedDate: Date): Date[] {
  const year = selectedDate.getFullYear();
  return Array.from(
    { length: MONTHS_PER_YEAR },
    (_, month) => new Date(year, month, 1),
  );
}

/**
 * The set of day keys that should carry an Event-presence dot: every day with
 * at least one Occurrence on a checked Calendar. Occurrences on unchecked
 * Calendars are excluded, and multiple Occurrences on one day collapse to a
 * single key.
 */
export function buildDaysWithEvents(
  occurrences: Occurrence[],
  checkedCalendarIds: Set<string>,
): Set<string> {
  const days = new Set<string>();
  for (const occurrence of occurrences) {
    if (!checkedCalendarIds.has(occurrence.event.calendarId)) continue;
    days.add(dayKey(occurrence.start));
  }
  return days;
}
