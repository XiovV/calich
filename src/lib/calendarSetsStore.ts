import { create } from "zustand";
import { useAuthStore } from "./authStore";
import { type CalendarSet, calendarSetsApi } from "./calendarSetsApi";

// The Calendar Sets Settings Section's data source (#301): every Calendar
// Set the caller owns in the active Workspace. Mirrors groupsStore's shape,
// minus membership management — a Set's membership is #302's.
interface CalendarSetsState {
  calendarSets: CalendarSet[];
  fetchCalendarSets: () => Promise<void>;
  createCalendarSet: (name: string) => Promise<CalendarSet>;
  renameCalendarSet: (id: number, name: string) => Promise<void>;
  deleteCalendarSet: (id: number) => Promise<void>;
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
}));
