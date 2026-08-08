import { create } from "zustand";
import { setSessionRefresher } from "./apiClient";
import { ApiError, authApi, type TimeFormat, type User } from "./authApi";
import { useShellStore, type ActiveView } from "./shellStore";

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
  updateUsername: (username: string) => Promise<void>;
  updateSyncedDeviceReminders: (enabled: boolean) => Promise<void>;
  updateWeekStart: (weekStart: number) => Promise<void>;
  updateDefaultView: (defaultView: ActiveView) => Promise<void>;
  updateTimeFormat: (timeFormat: TimeFormat) => Promise<void>;
  updateWorkingHours: (workingHours: { start: number; end: number } | null) => Promise<void>;
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

export const useAuthStore = create<AuthState>((set, get) => {
  // Registered once at module init so apiClient's authedFetch can recover
  // from a 401 without importing authStore itself (#124).
  setSessionRefresher(async () => {
    try {
      const { accessToken } = await authApi.refresh();
      set({ accessToken });
      return accessToken;
    } catch (err) {
      set(unauthenticated());
      throw err;
    }
  });

  return {
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
        useShellStore.getState().setActiveView(user.defaultView);
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
      useShellStore.getState().setActiveView(user.defaultView);
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
      // A forced password change completes the login that was blocked on it
      // (mustChangePassword), so it seeds the shell the same way bootstrap
      // and login do (ADR-0039) — otherwise a User with a non-default
      // Default view lands on the module-load "week" fallback instead.
      useShellStore.getState().setActiveView(user.defaultView);
    },

    updateEmail: async (email) => {
      const { accessToken } = get();
      if (!accessToken) throw new Error("Not authenticated.");

      const user = await authApi.updateEmail(accessToken, email);
      set(authenticated(user, accessToken));
    },

    updateUsername: async (username) => {
      const { accessToken } = get();
      if (!accessToken) throw new Error("Not authenticated.");

      const user = await authApi.updateUsername(accessToken, username);
      set(authenticated(user, accessToken));
    },

    updateSyncedDeviceReminders: async (enabled) => {
      const { accessToken } = get();
      if (!accessToken) throw new Error("Not authenticated.");

      const user = await authApi.updateSyncedDeviceReminders(accessToken, enabled);
      set(authenticated(user, accessToken));
    },

    updateWeekStart: async (weekStart) => {
      const { accessToken } = get();
      if (!accessToken) throw new Error("Not authenticated.");

      const user = await authApi.updatePreferences(accessToken, { weekStart });
      set(authenticated(user, accessToken));
    },

    // Sets the User's Default view Preference alone — Active view is
    // untouched, since Default view only seeds a Session's Active view at
    // bootstrap/login and is never written back to (ADR-0039).
    updateDefaultView: async (defaultView) => {
      const { accessToken } = get();
      if (!accessToken) throw new Error("Not authenticated.");

      const user = await authApi.updatePreferences(accessToken, { defaultView });
      set(authenticated(user, accessToken));
    },

    updateTimeFormat: async (timeFormat) => {
      const { accessToken } = get();
      if (!accessToken) throw new Error("Not authenticated.");

      const user = await authApi.updatePreferences(accessToken, { timeFormat });
      set(authenticated(user, accessToken));
    },

    updateWorkingHours: async (workingHours) => {
      const { accessToken } = get();
      if (!accessToken) throw new Error("Not authenticated.");

      const user = await authApi.updatePreferences(accessToken, { workingHours });
      set(authenticated(user, accessToken));
    },
  };
});
