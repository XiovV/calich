import type { CalendarColor } from "./calendarColors";

export interface Calendar {
  id: string;
  name: string;
  color: CalendarColor;
}

export function getCalendarById(
  calendars: Calendar[],
  id: string,
): Calendar | undefined {
  return calendars.find((calendar) => calendar.id === id);
}

export function getCheckedCalendars(
  calendars: Calendar[],
  checkedCalendarIds: Set<string>,
): Calendar[] {
  return calendars.filter((calendar) => checkedCalendarIds.has(calendar.id));
}

export const mockCalendars: Calendar[] = [
  { id: "personal", name: "Personal", color: "peacock" },
  { id: "work", name: "Work", color: "tomato" },
  { id: "family", name: "Family", color: "sage" },
];
