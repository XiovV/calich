import { create } from "zustand";
import { mockCalendars, type Calendar } from "./mockCalendars";

interface CalendarsState {
  calendars: Calendar[];
  addCalendar: (calendar: Calendar) => void;
  removeCalendar: (id: string) => void;
}

export const useCalendarsStore = create<CalendarsState>((set) => ({
  calendars: mockCalendars,
  addCalendar: (calendar) =>
    set((state) => ({ calendars: [...state.calendars, calendar] })),
  removeCalendar: (id) =>
    set((state) => ({
      calendars: state.calendars.filter((calendar) => calendar.id !== id),
    })),
}));
