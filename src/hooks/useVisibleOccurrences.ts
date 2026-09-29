import { useMemo } from "react";
import { useEventsStore } from "../lib/eventsStore";
import { expandOccurrences } from "../lib/expandOccurrences";
import type { Occurrence } from "../lib/occurrence";
import { useVisibleCalendarIds } from "./useVisibleCalendarIds";

/**
 * The Occurrences visible in the half-open window `[windowStartMs, windowEndMs)`:
 * every Event on a Calendar that is both in the Active Calendar Set and checked
 * (#303, ADR-0082), expanded over that window (ADR-0016). The window is passed
 * as epoch milliseconds so the memo is keyed on primitives — navigating within
 * the same window re-uses the result. Shared by the Day/Week (TimeGrid) and
 * Month grids, which differ only in how they derive the window.
 */
export function useVisibleOccurrences(
  windowStartMs: number,
  windowEndMs: number,
): Occurrence[] {
  const events = useEventsStore((state) => state.events);
  const visibleCalendarIds = useVisibleCalendarIds();

  return useMemo(() => {
    const visibleEvents = events.filter((event) =>
      visibleCalendarIds.has(event.calendarId),
    );
    return expandOccurrences(
      visibleEvents,
      new Date(windowStartMs),
      new Date(windowEndMs),
    );
  }, [events, visibleCalendarIds, windowStartMs, windowEndMs]);
}
