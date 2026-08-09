import { ApiError, authedFetch, errorFromResponse } from "./apiClient";
import type { ActiveView } from "./shellStore";

export { ApiError };

// Time format (ADR-0039): whether this app renders a time itself as 12-hour
// or 24-hour. Never reaches the browser's native time input, which renders
// in the browser's own locale regardless.
export type TimeFormat = "12h" | "24h";

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
  // Week start (ADR-0039): a date-fns weekStartsOn index, 0 (Sunday) to 6
  // (Saturday). Never fed into a Recurrence rule's WKST.
  weekStart: number;
  // Default view (ADR-0039): seeds Active view when a Session is
  // established (authStore's bootstrap/login) and is never written back.
  defaultView: ActiveView;
  // Time format (ADR-0039): applied wherever this app formats a time itself.
  timeFormat: TimeFormat;
  // Working hours (ADR-0039): a minutes-since-midnight range (0-1439, #136)
  // shading the hours outside it in Day and Week view. Both null means no
  // shading — the default.
  workingHoursStart: number | null;
  workingHoursEnd: number | null;
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
  week_start: number;
  default_view: ActiveView;
  time_format: TimeFormat;
  working_hours_start: number | null;
  working_hours_end: number | null;
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
    weekStart: wire.week_start,
    defaultView: wire.default_view,
    timeFormat: wire.time_format,
    workingHoursStart: wire.working_hours_start,
    workingHoursEnd: wire.working_hours_end,
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

  // Sets a password for the Pending account token names and logs the caller
  // straight in (ADR-0042) — the public, unauthenticated counterpart to
  // login for an Invite link.
  async acceptInvite(token: string, password: string): Promise<LoginResult> {
    const response = await fetch("/api/auth/accept-invite", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token, password }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as { access_token: string; must_change_password: boolean };
    return { accessToken: body.access_token, mustChangePassword: body.must_change_password };
  },

  // Resolves an Invite token to the username it names, without consuming it
  // (ADR-0042) — the accept-invite page calls this on load to show the
  // invitee which pre-chosen account they're setting a password for.
  async previewInvite(token: string): Promise<string> {
    const response = await fetch(`/api/auth/accept-invite?token=${encodeURIComponent(token)}`, {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as { username: string };
    return body.username;
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

  async updateUsername(accessToken: string, username: string): Promise<User> {
    const response = await authedFetch(accessToken, "/api/auth/username", {
      method: "PUT",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username }),
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

  // Partial PATCH body: only the fields present in `updates` are sent, so a
  // Week start update never touches Default view and vice versa (ADR-0039).
  // workingHours is a pair sent together — `null` clears it back to no
  // shading, and it's the one field this app never sends alone (Working
  // hours must be valid, start < end, before PreferencesSection dispatches).
  async updatePreferences(
    accessToken: string,
    updates: {
      weekStart?: number;
      defaultView?: ActiveView;
      timeFormat?: TimeFormat;
      workingHours?: { start: number; end: number } | null;
    },
  ): Promise<User> {
    const body: Record<string, unknown> = {};
    if (updates.weekStart !== undefined) body.week_start = updates.weekStart;
    if (updates.defaultView !== undefined) body.default_view = updates.defaultView;
    if (updates.timeFormat !== undefined) body.time_format = updates.timeFormat;
    if (updates.workingHours !== undefined) {
      body.working_hours_start = updates.workingHours ? updates.workingHours.start : null;
      body.working_hours_end = updates.workingHours ? updates.workingHours.end : null;
    }

    const response = await authedFetch(accessToken, "/api/auth/preferences", {
      method: "PATCH",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromMeWire(await response.json());
  },
};
