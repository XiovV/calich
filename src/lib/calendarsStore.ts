import { create } from "zustand";
import { useAuthStore } from "./authStore";
import { calendarsApi } from "./calendarsApi";
import { useEventsStore } from "./eventsStore";
import { getCalendarById, type Calendar } from "./calendar";
import { toast } from "./toast";

interface CalendarsState {
  calendars: Calendar[];
  fetchCalendars: () => Promise<void>;
  addCalendar: (calendar: Calendar) => Promise<void>;
  updateCalendar: (
    id: string,
    changes: {
      name: string;
      color: string;
      keepAlarms?: boolean;
      url?: string;
    },
  ) => Promise<void>;
  // Resolves to whether the delete actually succeeded, so callers that
  // cascade other local state off of it (e.g. deleteCalendarCascade) know
  // whether to undo that cascade too.
  removeCalendar: (id: string) => Promise<boolean>;
  // Renounces the caller's own Share (#114, ADR-0034) — the non-Owner
  // counterpart to removeCalendar, which only an Owner may call. Same
  // optimistic-remove/rollback contract.
  leaveCalendar: (id: string) => Promise<boolean>;
  // Not optimistic, unlike addCalendar — the server does the fetch and
  // write, so there's nothing to show until it responds. Rethrows on
  // failure so the Subscribe dialog can show the specific error (bad URL,
  // auth failure, ...) inline rather than a generic toast. Re-fetches
  // events afterward, since SubscribeService.Subscribe writes the feed's
  // Events synchronously before responding — nothing else asks for them,
  // so without this the grid stays empty until a manual reload (#93).
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
  // response itself only carries a summary, and re-fetches events for the
  // same reason as subscribeCalendar (#93). Rethrows on failure so the
  // caller can show a specific error.
  refreshCalendar: (id: string) => Promise<void>;
}

function requireAccessToken(): string {
  const accessToken = useAuthStore.getState().accessToken;
  if (!accessToken) throw new Error("Not authenticated.");
  return accessToken;
}

// Re-fetches events after a subscribe/refresh that already succeeded (#93).
// Swallows its own failure rather than rethrowing: the calendar is already
// committed to state at this point, so a failure here is a stale grid, not
// a failed subscribe/refresh — the caller's own rethrow-on-failure contract
// must stay reserved for genuine subscribe/refresh failures.
async function refetchEventsAfter(action: "subscribe" | "refresh"): Promise<void> {
  try {
    await useEventsStore.getState().fetchEvents();
  } catch {
    toast.error(
      action === "subscribe"
        ? "Subscribed, but couldn't load its events. Reload to see them."
        : "Refreshed, but couldn't load its events. Reload to see them.",
    );
  }
}

// accessChangeMessage refetches Calendars after an Event write elsewhere was
// refused because the caller's Access changed, and returns a toast message
// naming the affected Calendar (#116). Exported for eventsStore's write
// catch blocks to call, rather than having them reach into this store's
// state and fetchCalendars directly — this is the one place that touches
// both. The name is resolved before the refetch, since a revoked Share may
// drop the Calendar from the list entirely.
export async function accessChangeMessage(calendarId: string): Promise<string> {
  const calendar = getCalendarById(useCalendarsStore.getState().calendars, calendarId);
  await useCalendarsStore.getState().fetchCalendars().catch(() => {});
  return calendar
    ? `Your access to "${calendar.name}" has changed. Refreshing calendars.`
    : "Your access to this calendar has changed. Refreshing calendars.";
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
    // url is optimistically applied from the server's response, not from
    // changes itself — the User typed a raw (possibly password-bearing)
    // URL, and every surface showing a Subscription URL must show the
    // masked form the server returns instead (#88, ADR-0032).
    const { url, ...displayChanges } = changes;
    set((state) => ({
      calendars: state.calendars.map((calendar) =>
        calendar.id === id ? { ...calendar, ...displayChanges } : calendar,
      ),
    }));

    try {
      const updated = await calendarsApi.update(requireAccessToken(), id, changes);
      if (url !== undefined) {
        set((state) => ({
          calendars: state.calendars.map((calendar) =>
            calendar.id === id
              ? { ...calendar, sourceUrl: updated.sourceUrl }
              : calendar,
          ),
        }));
      }
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

  leaveCalendar: async (id) => {
    const previousCalendars = get().calendars;
    set((state) => ({
      calendars: state.calendars.filter((calendar) => calendar.id !== id),
    }));

    try {
      await calendarsApi.leave(requireAccessToken(), id);
      return true;
    } catch {
      set({ calendars: previousCalendars });
      toast.error("Failed to leave calendar.");
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
    await refetchEventsAfter("subscribe");
    return calendar;
  },

  refreshCalendar: async (id) => {
    const accessToken = requireAccessToken();
    await calendarsApi.refresh(accessToken, id);
    const calendar = await calendarsApi.get(accessToken, id);
    set((state) => ({
      calendars: state.calendars.map((c) => (c.id === id ? calendar : c)),
    }));
    await refetchEventsAfter("refresh");
  },
}));
