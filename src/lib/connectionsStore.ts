import { create } from "zustand";
import { type Connection, connectionsApi } from "./connectionsApi";
import { useAuthStore } from "./authStore";

interface ConnectionsState {
  connections: Connection[];
  fetchConnections: () => Promise<void>;
  // Returns the Google authorize URL — the caller navigates the browser
  // there itself (ConnectionsSection), since this store has no way to
  // complete a full-page OAuth round trip on its own.
  connectGoogle: () => Promise<string>;
  disconnect: (id: number) => Promise<void>;
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
}));
