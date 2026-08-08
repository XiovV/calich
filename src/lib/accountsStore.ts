import { create } from "zustand";
import { type Account, accountsApi } from "./accountsApi";
import { useAuthStore } from "./authStore";

interface AccountsState {
  accounts: Account[];
  fetchAccounts: () => Promise<void>;
  createAccount: (username: string, password: string) => Promise<void>;
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
}));
