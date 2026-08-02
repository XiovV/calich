import { create } from "zustand";
import { mockEvents, type Event } from "./mockEvents";

interface EventsState {
  events: Event[];
  addEvent: (event: Event) => void;
  removeEventsByCalendarId: (calendarId: string) => void;
}

export const useEventsStore = create<EventsState>((set) => ({
  events: mockEvents,
  addEvent: (event) =>
    set((state) => ({ events: [...state.events, event] })),
  removeEventsByCalendarId: (calendarId) =>
    set((state) => ({
      events: state.events.filter((event) => event.calendarId !== calendarId),
    })),
}));
