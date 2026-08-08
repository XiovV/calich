import { create } from "zustand";
import { type Account, type AccountDisposition, accountsApi } from "./accountsApi";
import { useAuthStore } from "./authStore";

interface AccountsState {
  accounts: Account[];
  fetchAccounts: () => Promise<void>;
  createAccount: (username: string, password: string) => Promise<void>;
  resetPassword: (id: number, password: string) => Promise<void>;
  setAdmin: (id: number, isAdmin: boolean) => Promise<void>;
  setDisabled: (id: number, isDisabled: boolean) => Promise<void>;
  deleteAccount: (id: number, disposition: AccountDisposition, transferTo?: number) => Promise<void>;
}

function requireAccessToken(): string {
  const accessToken = useAuthStore.getState().accessToken;
  if (!accessToken) throw new Error("Not authenticated.");
  return accessToken;
}

export const useAccountsStore = create<AccountsState>((set, get) => ({
  accounts: [],

  fetchAccounts: async () => {
    const accounts = await accountsApi.list(requireAccessToken());
    set({ accounts });
  },

  createAccount: async (username, password) => {
    const account = await accountsApi.create(requireAccessToken(), username, password);
    set({ accounts: [...get().accounts, account] });
  },

  resetPassword: async (id, password) => {
    const account = await accountsApi.resetPassword(requireAccessToken(), id, password);
    set({ accounts: get().accounts.map((a) => (a.id === id ? account : a)) });
  },

  setAdmin: async (id, isAdmin) => {
    const account = await accountsApi.setAdmin(requireAccessToken(), id, isAdmin);
    set({ accounts: get().accounts.map((a) => (a.id === id ? account : a)) });
  },

  setDisabled: async (id, isDisabled) => {
    const account = await accountsApi.setDisabled(requireAccessToken(), id, isDisabled);
    set({ accounts: get().accounts.map((a) => (a.id === id ? account : a)) });
  },

  deleteAccount: async (id, disposition, transferTo) => {
    await accountsApi.delete(requireAccessToken(), id, disposition, transferTo);
    set({ accounts: get().accounts.filter((a) => a.id !== id) });
  },
}));
