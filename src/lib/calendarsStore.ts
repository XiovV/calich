import { create } from "zustand";
import { useAuthStore } from "./authStore";
import { calendarsApi } from "./calendarsApi";
import type { Calendar } from "./calendar";
import { toast } from "./toast";

interface CalendarsState {
  calendars: Calendar[];
  fetchCalendars: () => Promise<void>;
  addCalendar: (calendar: Calendar) => Promise<void>;
  updateCalendar: (
    id: string,
    changes: { name: string; color: string; keepAlarms?: boolean },
  ) => Promise<void>;
  // Resolves to whether the delete actually succeeded, so callers that
  // cascade other local state off of it (e.g. deleteCalendarCascade) know
  // whether to undo that cascade too.
  removeCalendar: (id: string) => Promise<boolean>;
  // Not optimistic, unlike addCalendar — the server does the fetch and
  // write, so there's nothing to show until it responds. Rethrows on
  // failure so the Subscribe dialog can show the specific error (bad URL,
  // auth failure, ...) inline rather than a generic toast.
  subscribeCalendar: (
    url: string,
    name: string,
    color: string,
    keepAlarms: boolean,
  ) => Promise<Calendar>;
  // refreshCalendar triggers Refresh now for a Subscribed Calendar (#85).
  // Not optimistic, like subscribeCalendar — the server does the fetch and
  // reconcile, so there's nothing to show until it responds. Re-fetches the
  // Calendar afterward for its updated lastSyncedAt, since the refresh
  // response itself only carries a summary. Rethrows on failure so the
  // caller can show a specific error.
  refreshCalendar: (id: string) => Promise<void>;
}

function requireAccessToken(): string {
  const accessToken = useAuthStore.getState().accessToken;
  if (!accessToken) throw new Error("Not authenticated.");
  return accessToken;
}

export const useCalendarsStore = create<CalendarsState>((set, get) => ({
  calendars: [],

  fetchCalendars: async () => {
    const calendars = await calendarsApi.list(requireAccessToken());
    set({ calendars });
  },

  addCalendar: async (calendar) => {
    set((state) => ({ calendars: [...state.calendars, calendar] }));

    try {
      await calendarsApi.create(requireAccessToken(), calendar);
    } catch {
      set((state) => ({
        calendars: state.calendars.filter((c) => c.id !== calendar.id),
      }));
      toast.error(`Failed to create calendar "${calendar.name}".`);
    }
  },

  updateCalendar: async (id, changes) => {
    const previousCalendars = get().calendars;
    set((state) => ({
      calendars: state.calendars.map((calendar) =>
        calendar.id === id ? { ...calendar, ...changes } : calendar,
      ),
    }));

    try {
      await calendarsApi.update(requireAccessToken(), id, changes);
    } catch {
      set({ calendars: previousCalendars });
      toast.error("Failed to update calendar.");
    }
  },

  removeCalendar: async (id) => {
    const previousCalendars = get().calendars;
    set((state) => ({
      calendars: state.calendars.filter((calendar) => calendar.id !== id),
    }));

    try {
      await calendarsApi.remove(requireAccessToken(), id);
      return true;
    } catch {
      set({ calendars: previousCalendars });
      toast.error("Failed to delete calendar.");
      return false;
    }
  },

  subscribeCalendar: async (url, name, color, keepAlarms) => {
    const calendar = await calendarsApi.subscribe(requireAccessToken(), {
      url,
      name,
      color,
      keepAlarms,
    });
    set((state) => ({ calendars: [...state.calendars, calendar] }));
    return calendar;
  },

  refreshCalendar: async (id) => {
    const accessToken = requireAccessToken();
    await calendarsApi.refresh(accessToken, id);
    const calendar = await calendarsApi.get(accessToken, id);
    set((state) => ({
      calendars: state.calendars.map((c) => (c.id === id ? calendar : c)),
    }));
  },
}));
