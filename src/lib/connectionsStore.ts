import { create } from "zustand";
import { type Calendar } from "./calendar";
import { type Connection, type PickerCalendar, connectionsApi } from "./connectionsApi";
import { useAuthStore } from "./authStore";

interface ConnectionsState {
  connections: Connection[];
  fetchConnections: () => Promise<void>;
  // Returns the Google authorize URL — the caller navigates the browser
  // there itself (ConnectionsSection), since this store has no way to
  // complete a full-page OAuth round trip on its own.
  connectGoogle: () => Promise<string>;
  disconnect: (id: number) => Promise<void>;
  // The Calendar picker (#286): listPickerCalendars is the read side, not
  // cached here (CalendarPickerModal owns its own fetched list, since it's
  // shown once per Connect and never needs to be re-derived from other
  // state). importCalendars is the write side; its caller is responsible
  // for refreshing calendarsStore afterward so the sidebar picks up the new
  // Linked Calendars.
  listPickerCalendars: (id: number) => Promise<PickerCalendar[]>;
  importCalendars: (id: number, calendarIds: string[]) => Promise<Calendar[]>;
}

function requireAccessToken(): string {
  const accessToken = useAuthStore.getState().accessToken;
  if (!accessToken) throw new Error("Not authenticated.");
  return accessToken;
}

export const useConnectionsStore = create<ConnectionsState>((set, get) => ({
  connections: [],

  fetchConnections: async () => {
    const connections = await connectionsApi.list(requireAccessToken());
    set({ connections });
  },

  connectGoogle: async () => {
    return connectionsApi.connectGoogle(requireAccessToken());
  },

  disconnect: async (id) => {
    await connectionsApi.disconnect(requireAccessToken(), id);
    set({ connections: get().connections.filter((c) => c.id !== id) });
  },

  listPickerCalendars: async (id) => {
    return connectionsApi.listPickerCalendars(requireAccessToken(), id);
  },

  importCalendars: async (id, calendarIds) => {
    return connectionsApi.importCalendars(requireAccessToken(), id, calendarIds);
  },
}));
