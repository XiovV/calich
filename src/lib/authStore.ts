import { create } from "zustand";
import { ApiError, authApi, type User } from "./authApi";

export type AuthStatus = "loading" | "unauthenticated" | "must-change-password" | "authenticated";

interface AuthState {
  status: AuthStatus;
  user: User | null;
  // The username typed at login, kept only for display on the forced
  // password-change screen — GET /api/auth/me is blocked (403) while a
  // password change is still required, so the full User record isn't
  // available yet. Cleared once authenticated.
  pendingUsername: string | null;
  accessToken: string | null;
  bootstrap: () => Promise<void>;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  changePassword: (currentPassword: string, newPassword: string) => Promise<void>;
  updateEmail: (email: string) => Promise<void>;
  updateSyncedDeviceReminders: (enabled: boolean) => Promise<void>;
}

type AuthFields = Pick<AuthState, "status" | "user" | "pendingUsername" | "accessToken">;

function unauthenticated(): AuthFields {
  return { status: "unauthenticated", user: null, pendingUsername: null, accessToken: null };
}

function mustChangePassword(accessToken: string, pendingUsername: string | null): AuthFields {
  return { status: "must-change-password", user: null, pendingUsername, accessToken };
}

function authenticated(user: User, accessToken: string): AuthFields {
  return { status: "authenticated", user, pendingUsername: null, accessToken };
}

export const useAuthStore = create<AuthState>((set, get) => ({
  ...unauthenticated(),
  status: "loading",

  bootstrap: async () => {
    let accessToken: string;
    try {
      ({ accessToken } = await authApi.refresh());
    } catch {
      set(unauthenticated());
      return;
    }

    try {
      const user = await authApi.me(accessToken);
      set(authenticated(user, accessToken));
    } catch (err) {
      if (err instanceof ApiError && err.code === "password_change_required") {
        set(mustChangePassword(accessToken, null));
        return;
      }
      set(unauthenticated());
    }
  },

  login: async (username, password) => {
    const { accessToken, mustChangePassword: passwordChangeRequired } = await authApi.login(username, password);

    if (passwordChangeRequired) {
      set(mustChangePassword(accessToken, username));
      return;
    }

    const user = await authApi.me(accessToken);
    set(authenticated(user, accessToken));
  },

  logout: async () => {
    // Best-effort: the local session ends regardless of whether the backend
    // call succeeds (e.g. a network hiccup shouldn't trap the user logged in
    // from their own point of view).
    await authApi.logout().catch(() => {});
    set(unauthenticated());
  },

  changePassword: async (currentPassword, newPassword) => {
    const { accessToken } = get();
    if (!accessToken) throw new Error("Not authenticated.");

    // The backend re-issues the Session on a password change rather than
    // just clearing it (#123), so the fresh access token must replace the
    // pre-change one before calling anything else with it.
    const changed = await authApi.changePassword(accessToken, currentPassword, newPassword);

    const user = await authApi.me(changed.accessToken);
    set(authenticated(user, changed.accessToken));
  },

  updateEmail: async (email) => {
    const { accessToken } = get();
    if (!accessToken) throw new Error("Not authenticated.");

    const user = await authApi.updateEmail(accessToken, email);
    set(authenticated(user, accessToken));
  },

  updateSyncedDeviceReminders: async (enabled) => {
    const { accessToken } = get();
    if (!accessToken) throw new Error("Not authenticated.");

    const user = await authApi.updateSyncedDeviceReminders(accessToken, enabled);
    set(authenticated(user, accessToken));
  },
}));
