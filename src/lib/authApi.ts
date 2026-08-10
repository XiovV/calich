import { ApiError, authedFetch, errorFromResponse } from "./apiClient";
import type { ActiveView } from "./shellStore";

export { ApiError };

// Time format (ADR-0039): whether this app renders a time itself as 12-hour
// or 24-hour. Never reaches the browser's native time input, which renders
// in the browser's own locale regardless.
export type TimeFormat = "12h" | "24h";

export interface User {
  id: number;
  // Name is a display-only label (ADR-0047) — not unique, not used to sign
  // in. Email is the login identifier.
  name: string;
  mustChangePassword: boolean;
  email: string;
  // Whether the Email Channel can actually be used for a new Reminder: every
  // account always has an email now (ADR-0047), so this collapses to just
  // whether the self-hoster has SMTP configured (ADR-0021, ADR-0010).
  emailReminderChannelAvailable: boolean;
  // "Let my synced devices show reminder pop-ups (disable in-app reminder
  // notifications)" (ADR-0027). Defaults false.
  syncedDeviceRemindersEnabled: boolean;
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
  // isDisabled is whether the account that just authenticated is Disabled
  // (ADR-0044) — Login still succeeds, since re-activating is the one action
  // a Disabled User must still be able to reach with no instance-wide Admin
  // left to do it for them.
  isDisabled: boolean;
}

interface MeWire {
  id: number;
  name: string;
  must_change_password: boolean;
  email: string;
  email_reminder_channel_available: boolean;
  synced_device_reminders_enabled: boolean;
  week_start: number;
  default_view: ActiveView;
  time_format: TimeFormat;
  working_hours_start: number | null;
  working_hours_end: number | null;
}

function fromMeWire(wire: MeWire): User {
  return {
    id: wire.id,
    name: wire.name,
    mustChangePassword: wire.must_change_password,
    email: wire.email,
    emailReminderChannelAvailable: wire.email_reminder_channel_available,
    syncedDeviceRemindersEnabled: wire.synced_device_reminders_enabled,
    weekStart: wire.week_start,
    defaultView: wire.default_view,
    timeFormat: wire.time_format,
    workingHoursStart: wire.working_hours_start,
    workingHoursEnd: wire.working_hours_end,
  };
}

export const authApi = {
  // Reports whether the instance has any accounts yet (#169, ADR-0047) —
  // public and unauthenticated, so it's answerable before anyone has signed
  // in. Drives the first-run redirect to Register instead of Sign-in.
  async setupStatus(): Promise<{ hasAccounts: boolean }> {
    const response = await fetch("/api/auth/setup-status", { credentials: "include" });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as { has_accounts: boolean };
    return { hasAccounts: body.has_accounts };
  },

  async login(email: string, password: string): Promise<LoginResult> {
    const response = await fetch("/api/auth/login", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email, password }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as {
      access_token: string;
      must_change_password: boolean;
      is_disabled: boolean;
    };
    return {
      accessToken: body.access_token,
      mustChangePassword: body.must_change_password,
      isDisabled: body.is_disabled,
    };
  },

  // Self-registers a new account (ADR-0044) and logs the caller straight in
  // — the public, unauthenticated first-run bootstrap form and
  // self-registration endpoint. Always succeeds for the very first account
  // on the instance; otherwise only while ENABLE_SIGNUPS is true, in which
  // case the server returns a "forbidden" ApiError.
  async register(name: string, email: string, password: string): Promise<LoginResult> {
    const response = await fetch("/api/auth/register", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, email, password }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as { access_token: string; must_change_password: boolean };
    return { accessToken: body.access_token, mustChangePassword: body.must_change_password, isDisabled: false };
  },

  // Resolves a Workspace Invite token to the Workspace and email it names,
  // without consuming it (ADR-0044), and reports whether that email already
  // has an account — the accept-workspace-invite page uses this to decide
  // whether to collect a name and password (new account) or ask the invitee
  // to log in (existing account).
  async previewWorkspaceInvite(
    token: string,
  ): Promise<{ workspaceName: string; email: string; userExists: boolean }> {
    const response = await fetch(`/api/auth/accept-workspace-invite?token=${encodeURIComponent(token)}`, {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as { workspace_name: string; email: string; user_exists: boolean };
    return { workspaceName: body.workspace_name, email: body.email, userExists: body.user_exists };
  },

  // Accepts a Workspace Invite for an email with no existing account
  // (ADR-0044): creates the account, adds it as a Member of the inviting
  // Workspace, and logs the caller straight in — the public,
  // unauthenticated new-account accept path.
  async acceptWorkspaceInvite(token: string, name: string, password: string): Promise<LoginResult> {
    const response = await fetch("/api/auth/accept-workspace-invite", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token, name, password }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as { access_token: string; must_change_password: boolean };
    return { accessToken: body.access_token, mustChangePassword: body.must_change_password, isDisabled: false };
  },

  // Accepts a Workspace Invite on behalf of the already-authenticated caller
  // (ADR-0044): just adds a Membership for the inviting Workspace — no new
  // account, no password step. The server refuses this unless the caller's
  // own account email matches the invite's exactly.
  async joinWorkspaceInvite(accessToken: string, token: string): Promise<{ id: number; name: string }> {
    const response = await authedFetch(accessToken, "/api/auth/accept-workspace-invite/join", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as { id: number; name: string };
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

  async updateName(accessToken: string, name: string): Promise<User> {
    const response = await authedFetch(accessToken, "/api/auth/name", {
      method: "PUT",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
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
