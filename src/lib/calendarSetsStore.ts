import { create } from "zustand";
import { useAuthStore } from "./authStore";
import { type CalendarSet, calendarSetsApi } from "./calendarSetsApi";

// The Calendar Sets Settings Section's data source (#301, #302): every
// Calendar Set the caller owns in the active Workspace, each carrying its
// own membership. Mirrors groupsStore's shape, except membership rides
// along on the Set itself (calendarSet.calendarIds) rather than a
// separate membersByGroupId map — ADR-0082's List embeds it, so there is
// nothing to fetch on demand the way GroupMembersDialog does.
interface CalendarSetsState {
  calendarSets: CalendarSet[];
  fetchCalendarSets: () => Promise<void>;
  createCalendarSet: (name: string) => Promise<CalendarSet>;
  renameCalendarSet: (id: number, name: string) => Promise<void>;
  deleteCalendarSet: (id: number) => Promise<void>;
  addCalendarToSet: (id: number, calendarId: string) => Promise<void>;
  removeCalendarFromSet: (id: number, calendarId: string) => Promise<void>;
}

function requireAccessToken(): string {
  const accessToken = useAuthStore.getState().accessToken;
  if (!accessToken) throw new Error("Not authenticated.");
  return accessToken;
}

export const useCalendarSetsStore = create<CalendarSetsState>((set, get) => ({
  calendarSets: [],

  fetchCalendarSets: async () => {
    const calendarSets = await calendarSetsApi.list(requireAccessToken());
    set({ calendarSets });
  },

  createCalendarSet: async (name) => {
    const created = await calendarSetsApi.create(requireAccessToken(), name);
    set({ calendarSets: [...get().calendarSets, created] });
    return created;
  },

  renameCalendarSet: async (id, name) => {
    const renamed = await calendarSetsApi.rename(requireAccessToken(), id, name);
    set({ calendarSets: get().calendarSets.map((s) => (s.id === id ? renamed : s)) });
  },

  deleteCalendarSet: async (id) => {
    await calendarSetsApi.remove(requireAccessToken(), id);
    set({ calendarSets: get().calendarSets.filter((s) => s.id !== id) });
  },

  // Both immediate-effect, no save step (#302): the toggle in the
  // membership dialog awaits this directly, so a failure surfaces inline
  // rather than needing a rollback.
  addCalendarToSet: async (id, calendarId) => {
    await calendarSetsApi.addCalendar(requireAccessToken(), id, calendarId);
    set({
      calendarSets: get().calendarSets.map((s) =>
        s.id === id && !s.calendarIds.includes(calendarId)
          ? { ...s, calendarIds: [...s.calendarIds, calendarId] }
          : s,
      ),
    });
  },

  removeCalendarFromSet: async (id, calendarId) => {
    await calendarSetsApi.removeCalendar(requireAccessToken(), id, calendarId);
    set({
      calendarSets: get().calendarSets.map((s) =>
        s.id === id ? { ...s, calendarIds: s.calendarIds.filter((c) => c !== calendarId) } : s,
      ),
    });
  },
}));
