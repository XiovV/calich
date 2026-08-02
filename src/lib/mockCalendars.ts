import type { CalendarColor } from "./calendarColors";

export interface Calendar {
  id: string;
  name: string;
  color: CalendarColor;
}

export interface CalendarSection {
  label: string;
  calendars: Calendar[];
}

export function getCalendarById(id: string): Calendar | undefined {
  return mockCalendarSections
    .flatMap((section) => section.calendars)
    .find((calendar) => calendar.id === id);
}

export function getCheckedCalendars(checkedCalendarIds: Set<string>): Calendar[] {
  return mockCalendarSections
    .flatMap((section) => section.calendars)
    .filter((calendar) => checkedCalendarIds.has(calendar.id));
}

export const mockCalendarSections: CalendarSection[] = [
  {
    label: "My calendars",
    calendars: [
      { id: "personal", name: "Personal", color: "peacock" },
      { id: "work", name: "Work", color: "tomato" },
      { id: "family", name: "Family", color: "sage" },
    ],
  },
  {
    label: "Other calendars",
    calendars: [
      { id: "holidays", name: "Holidays", color: "banana" },
      { id: "birthdays", name: "Birthdays", color: "grape" },
    ],
  },
];
