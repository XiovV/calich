import { create } from "zustand";
import { type AppPassword, appPasswordsApi } from "./appPasswordsApi";
import { useAuthStore } from "./authStore";

interface AppPasswordsState {
  appPasswords: AppPassword[];
  fetchAppPasswords: () => Promise<void>;
  // Returns the plaintext secret — shown to the user exactly once (ADR-0024).
  createAppPassword: (label: string) => Promise<string>;
  revokeAppPassword: (id: number) => Promise<void>;
}

function requireAccessToken(): string {
  const accessToken = useAuthStore.getState().accessToken;
  if (!accessToken) throw new Error("Not authenticated.");
  return accessToken;
}

export const useAppPasswordsStore = create<AppPasswordsState>((set, get) => ({
  appPasswords: [],

  fetchAppPasswords: async () => {
    const appPasswords = await appPasswordsApi.list(requireAccessToken());
    set({ appPasswords });
  },

  createAppPassword: async (label) => {
    const { secret, ...created } = await appPasswordsApi.create(requireAccessToken(), label);
    set({ appPasswords: [created, ...get().appPasswords] });
    return secret;
  },

  revokeAppPassword: async (id) => {
    await appPasswordsApi.revoke(requireAccessToken(), id);
    set({ appPasswords: get().appPasswords.filter((p) => p.id !== id) });
  },
}));
