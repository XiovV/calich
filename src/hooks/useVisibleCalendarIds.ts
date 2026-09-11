import { useMemo } from "react";
import { getCheckedCalendars } from "../lib/calendar";
import { inScopeCalendars } from "../lib/calendarSetScope";
import { useActiveCalendarSet } from "../lib/calendarSetsStore";
import { useCalendarsStore } from "../lib/calendarsStore";
import { useShellStore } from "../lib/shellStore";

// useVisibleCalendarIds answers which Calendar ids the grid renders Events
// for right now (#303, ADR-0082): in the Active Calendar Set *and* toggled
// on. The grid's two consumers — useVisibleOccurrences (Day/Week/Month) and
// YearGrid — compose over this one answer instead of each re-deriving it,
// mirroring the sidebar's own use of inScopeCalendars.
export function useVisibleCalendarIds(): Set<string> {
  const calendars = useCalendarsStore((state) => state.calendars);
  const checkedCalendarIds = useShellStore((state) => state.checkedCalendarIds);
  const activeCalendarSet = useActiveCalendarSet();

  return useMemo(() => {
    const visible = getCheckedCalendars(
      inScopeCalendars(calendars, activeCalendarSet),
      checkedCalendarIds,
    );
    return new Set(visible.map((calendar) => calendar.id));
  }, [calendars, activeCalendarSet, checkedCalendarIds]);
}
