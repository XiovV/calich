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
