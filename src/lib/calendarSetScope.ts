import type { Calendar } from "./calendar";
import type { CalendarSet } from "./calendarSetsApi";

// inScopeCalendars answers the one question the whole Calendar Set feature
// reduces to (#303, ADR-0082): which Calendars exist right now. No Active
// Calendar Set (activeCalendarSet is null — "All calendars") answers with
// every Calendar, exactly as the app behaves today. An active one narrows
// to its members, filtered through the caller's own current Calendar list —
// so a member the caller can no longer see (a revoked Share, an
// unsubscribe, a disconnected Connection) is silently absent rather than
// erroring, and an empty Set answers with nothing. The sidebar and the grid
// compose over this one answer rather than each re-deriving it, which is
// what keeps both of them ignorant of Sets and makes the rule testable
// once.
export function inScopeCalendars(
  calendars: Calendar[],
  activeCalendarSet: CalendarSet | null,
): Calendar[] {
  if (!activeCalendarSet) return calendars;
  const memberIds = new Set(activeCalendarSet.calendarIds);
  return calendars.filter((calendar) => memberIds.has(calendar.id));
}
