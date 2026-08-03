import { create } from "zustand";
import { useAuthStore } from "./authStore";
import type { Event } from "./event";
import { eventsApi } from "./eventsApi";
import { toast } from "./toast";

type EventChanges = Partial<Omit<Event, "id">>;

interface EventsState {
  events: Event[];
  fetchEvents: () => Promise<void>;
  addEvent: (event: Event) => Promise<void>;
  updateEvent: (id: string, changes: EventChanges) => Promise<void>;
  removeEvent: (id: string) => Promise<void>;
  removeEventsByCalendarId: (calendarId: string) => void;
}

function requireAccessToken(): string {
  const accessToken = useAuthStore.getState().accessToken;
  if (!accessToken) throw new Error("Not authenticated.");
  return accessToken;
}

export const useEventsStore = create<EventsState>((set, get) => ({
  events: [],

  fetchEvents: async () => {
    const events = await eventsApi.list(requireAccessToken());
    set({ events });
  },

  addEvent: async (event) => {
    set((state) => ({ events: [...state.events, event] }));

    try {
      await eventsApi.create(requireAccessToken(), event);
    } catch {
      set((state) => ({
        events: state.events.filter((e) => e.id !== event.id),
      }));
      toast.error(`Failed to create event "${event.title}".`);
    }
  },

  updateEvent: async (id, changes) => {
    const previousEvents = get().events;
    const current = previousEvents.find((event) => event.id === id);
    if (!current) return;

    const updated: Event = { ...current, ...changes };
    set((state) => ({
      events: state.events.map((event) => (event.id === id ? updated : event)),
    }));

    try {
      await eventsApi.update(requireAccessToken(), id, {
        calendarId: updated.calendarId,
        title: updated.title,
        start: updated.start,
        end: updated.end,
        rrule: updated.rrule,
      });
    } catch {
      set({ events: previousEvents });
      toast.error("Failed to update event.");
    }
  },

  removeEvent: async (id) => {
    const previousEvents = get().events;
    set((state) => ({
      events: state.events.filter((event) => event.id !== id),
    }));

    try {
      await eventsApi.remove(requireAccessToken(), id);
    } catch {
      set({ events: previousEvents });
      toast.error("Failed to delete event.");
    }
  },

  removeEventsByCalendarId: (calendarId) =>
    set((state) => ({
      events: state.events.filter((event) => event.calendarId !== calendarId),
    })),
}));
