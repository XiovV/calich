import { ApiError, authedFetch, errorFromResponse } from "./apiClient";

export { ApiError };

export interface User {
  id: number;
  username: string;
  mustChangePassword: boolean;
  email: string | null;
  // Whether the Email Channel can actually be used for a new Reminder: the
  // user has an email set *and* the self-hoster has SMTP configured
  // (ADR-0021, ADR-0010).
  emailReminderChannelAvailable: boolean;
  // "Let my synced devices show reminder pop-ups (disable in-app reminder
  // notifications)" (ADR-0027). Defaults false.
  syncedDeviceRemindersEnabled: boolean;
  // Authority over who exists on the instance (ADR-0037) — gates whether the
  // app renders any administration UI at all (#119).
  isAdmin: boolean;
}

export interface LoginResult {
  accessToken: string;
  mustChangePassword: boolean;
}

interface MeWire {
  id: number;
  username: string;
  must_change_password: boolean;
  email: string | null;
  email_reminder_channel_available: boolean;
  synced_device_reminders_enabled: boolean;
  is_admin: boolean;
}

function fromMeWire(wire: MeWire): User {
  return {
    id: wire.id,
    username: wire.username,
    mustChangePassword: wire.must_change_password,
    email: wire.email,
    emailReminderChannelAvailable: wire.email_reminder_channel_available,
    syncedDeviceRemindersEnabled: wire.synced_device_reminders_enabled,
    isAdmin: wire.is_admin,
  };
}

export const authApi = {
  async login(username: string, password: string): Promise<LoginResult> {
    const response = await fetch("/api/auth/login", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as { access_token: string; must_change_password: boolean };
    return { accessToken: body.access_token, mustChangePassword: body.must_change_password };
  },

  // Relies on the httpOnly refresh_token cookie the browser sends automatically —
  // there is nothing to pass in.
  async refresh(): Promise<{ accessToken: string }> {
    const response = await fetch("/api/auth/refresh", {
      method: "POST",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as { access_token: string };
    return { accessToken: body.access_token };
  },

  async logout(): Promise<void> {
    const response = await fetch("/api/auth/logout", {
      method: "POST",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  async me(accessToken: string): Promise<User> {
    const response = await authedFetch(accessToken, "/api/auth/me", {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromMeWire(await response.json());
  },

  async changePassword(
    accessToken: string,
    currentPassword: string,
    newPassword: string,
  ): Promise<{ accessToken: string }> {
    const response = await authedFetch(accessToken, "/api/auth/change-password", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as { access_token: string };
    return { accessToken: body.access_token };
  },

  async updateEmail(accessToken: string, email: string): Promise<User> {
    const response = await authedFetch(accessToken, "/api/auth/email", {
      method: "PUT",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromMeWire(await response.json());
  },

  async updateSyncedDeviceReminders(accessToken: string, enabled: boolean): Promise<User> {
    const response = await authedFetch(accessToken, "/api/auth/synced-device-reminders", {
      method: "PUT",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ synced_device_reminders_enabled: enabled }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromMeWire(await response.json());
  },
};
