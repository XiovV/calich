import { useMemo } from "react";
import { useEventsStore } from "../lib/eventsStore";
import { useShellStore } from "../lib/shellStore";
import { expandOccurrences } from "../lib/expandOccurrences";
import type { Occurrence } from "../lib/occurrence";

/**
 * The Occurrences visible in the half-open window `[windowStartMs, windowEndMs)`:
 * every Event on a checked Calendar, expanded over that window (ADR-0016). The
 * window is passed as epoch milliseconds so the memo is keyed on primitives —
 * navigating within the same window re-uses the result. Shared by the Day/Week
 * (TimeGrid) and Month grids, which differ only in how they derive the window.
 */
export function useVisibleOccurrences(
  windowStartMs: number,
  windowEndMs: number,
): Occurrence[] {
  const events = useEventsStore((state) => state.events);
  const checkedCalendarIds = useShellStore((state) => state.checkedCalendarIds);

  return useMemo(() => {
    const visibleEvents = events.filter((event) =>
      checkedCalendarIds.has(event.calendarId),
    );
    return expandOccurrences(
      visibleEvents,
      new Date(windowStartMs),
      new Date(windowEndMs),
    );
  }, [events, checkedCalendarIds, windowStartMs, windowEndMs]);
}
