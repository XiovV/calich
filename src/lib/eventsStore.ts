import { create } from "zustand";
import { mockEvents, type Event } from "./mockEvents";

interface EventsState {
  events: Event[];
  addEvent: (event: Event) => void;
  updateEvent: (id: string, changes: Partial<Event>) => void;
  removeEvent: (id: string) => void;
  removeEventsByCalendarId: (calendarId: string) => void;
}

export const useEventsStore = create<EventsState>((set) => ({
  events: mockEvents,
  addEvent: (event) =>
    set((state) => ({ events: [...state.events, event] })),
  updateEvent: (id, changes) =>
    set((state) => ({
      events: state.events.map((event) =>
        event.id === id ? { ...event, ...changes } : event,
      ),
    })),
  removeEvent: (id) =>
    set((state) => ({
      events: state.events.filter((event) => event.id !== id),
    })),
  removeEventsByCalendarId: (calendarId) =>
    set((state) => ({
      events: state.events.filter((event) => event.calendarId !== calendarId),
    })),
}));
