import { create } from "zustand";
import { useAuthStore } from "./authStore";
import { calendarsApi, type GroupShare, type Role, type Share } from "./calendarsApi";
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
  // setCalendarColor sets the caller's own colour on id over the same PATCH
  // endpoint updateCalendar uses (ADR-0038, #122): an Owner's write lands on
  // the Calendar's own colour, anyone else's lands as their personal
  // override — the server decides which, not this store. Optimistic like
  // updateCalendar, since the colour applied is exactly the one the caller
  // just picked, with no override-vs-fallback ambiguity to resolve.
  setCalendarColor: (id: string, color: string) => Promise<void>;
  // clearCalendarColor removes the caller's personal override on id,
  // falling back to the Owner's colour (ADR-0038, #122). Not optimistic,
  // unlike setCalendarColor: the fallback colour is whatever the Owner's is,
  // which this store must never resolve itself — only the server's response
  // says what it is.
  clearCalendarColor: (id: string) => Promise<void>;
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
  // shareCalendar grants a new Share (#113, ADR-0034). Re-fetches Calendars
  // afterward so shareCount on the sidebar badge (#126) moves without a
  // reload — the caller is looking straight at it while granting, so a
  // count that doesn't move reads as a failed grant. Rethrows on failure so
  // CalendarSharingSection can show the specific error inline.
  shareCalendar: (id: string, email: string, role: Role) => Promise<Share>;
  // revokeCalendarShare removes a Share (#113, ADR-0034), Owner-only — the
  // counterpart to leaveCalendar. Re-fetches Calendars afterward for the
  // same shareCount-freshness reason as shareCalendar. Rethrows on failure.
  revokeCalendarShare: (id: string, userId: number) => Promise<void>;
  // shareCalendarWithGroup grants a new Group Share (#159, ADR-0045) —
  // shareCalendar's Group-targeted sibling, same shareCount-refresh and
  // rethrow-on-failure contract.
  shareCalendarWithGroup: (id: string, groupId: number, role: Role) => Promise<GroupShare>;
  // revokeCalendarGroupShare removes a Group Share (#159, ADR-0045) —
  // revokeCalendarShare's Group-targeted sibling.
  revokeCalendarGroupShare: (id: string, groupId: number) => Promise<void>;
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

  setCalendarColor: async (id, color) => {
    const previousCalendars = get().calendars;
    set((state) => ({
      calendars: state.calendars.map((calendar) =>
        calendar.id === id ? { ...calendar, color } : calendar,
      ),
    }));

    try {
      await calendarsApi.updateColor(requireAccessToken(), id, color);
    } catch {
      set({ calendars: previousCalendars });
      toast.error("Failed to update calendar color.");
    }
  },

  clearCalendarColor: async (id) => {
    try {
      const updated = await calendarsApi.clearColor(requireAccessToken(), id);
      set((state) => ({
        calendars: state.calendars.map((calendar) =>
          calendar.id === id ? { ...calendar, color: updated.color } : calendar,
        ),
      }));
    } catch {
      toast.error("Failed to clear calendar color.");
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

  shareCalendar: async (id, email, role) => {
    const share = await calendarsApi.share(requireAccessToken(), id, email, role);
    await get().fetchCalendars();
    return share;
  },

  revokeCalendarShare: async (id, userId) => {
    await calendarsApi.revokeShare(requireAccessToken(), id, userId);
    await get().fetchCalendars();
  },

  shareCalendarWithGroup: async (id, groupId, role) => {
    const share = await calendarsApi.shareWithGroup(requireAccessToken(), id, groupId, role);
    await get().fetchCalendars();
    return share;
  },

  revokeCalendarGroupShare: async (id, groupId) => {
    await calendarsApi.revokeGroupShare(requireAccessToken(), id, groupId);
    await get().fetchCalendars();
  },
}));
