import { create } from "zustand";
import { useAuthStore } from "./authStore";
import { type AvailabilityRange, type AvailabilitySchedule, availabilitySchedulesApi } from "./availabilitySchedulesApi";

// The Availability Schedules Settings Section's data source (#320,
// ADR-0085). Mirrors calendarSetsStore's shape, minus any Workspace
// dependency — a Schedule belongs to the User alone, so there is nothing to
// re-fetch on Workspace switch.
interface AvailabilitySchedulesState {
  schedules: AvailabilitySchedule[];
  fetchSchedules: (tzHint?: string) => Promise<void>;
  createSchedule: (name: string, tzid: string, ranges: AvailabilityRange[]) => Promise<AvailabilitySchedule>;
  updateSchedule: (id: number, name: string, tzid: string, ranges: AvailabilityRange[]) => Promise<void>;
  deleteSchedule: (id: number) => Promise<void>;
}

function requireAccessToken(): string {
  const accessToken = useAuthStore.getState().accessToken;
  if (!accessToken) throw new Error("Not authenticated.");
  return accessToken;
}

export const useAvailabilitySchedulesStore = create<AvailabilitySchedulesState>((set, get) => ({
  schedules: [],

  fetchSchedules: async (tzHint) => {
    const schedules = await availabilitySchedulesApi.list(requireAccessToken(), tzHint);
    set({ schedules });
  },

  createSchedule: async (name, tzid, ranges) => {
    const created = await availabilitySchedulesApi.create(requireAccessToken(), name, tzid, ranges);
    set({ schedules: [...get().schedules, created] });
    return created;
  },

  updateSchedule: async (id, name, tzid, ranges) => {
    const updated = await availabilitySchedulesApi.update(requireAccessToken(), id, name, tzid, ranges);
    set({ schedules: get().schedules.map((s) => (s.id === id ? updated : s)) });
  },

  deleteSchedule: async (id) => {
    await availabilitySchedulesApi.remove(requireAccessToken(), id);
    set({ schedules: get().schedules.filter((s) => s.id !== id) });
  },
}));
