import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, authApi } from "./authApi";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function emptyResponse(status: number): Response {
  return new Response(null, { status });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("authApi.login", () => {
  it("returns the access token and must_change_password flag on success", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { access_token: "token-123", must_change_password: true }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await authApi.login("admin", "admin");

    expect(result).toEqual({ accessToken: "token-123", mustChangePassword: true });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/login",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({ username: "admin", password: "admin" }),
      }),
    );
  });

  it("throws an ApiError with the backend's code and message on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(401, { error: { code: "invalid_credentials", message: "invalid username or password" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.login("admin", "wrong")).rejects.toMatchObject({
      code: "invalid_credentials",
      message: "invalid username or password",
      status: 401,
    });
  });

  it("falls back to a generic error when the body isn't valid JSON", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response("not json", { status: 500 }));
    vi.stubGlobal("fetch", fetchMock);

    const error = await authApi.login("admin", "admin").catch((e: unknown) => e);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).status).toBe(500);
  });
});

describe("authApi.acceptInvite", () => {
  it("returns the access token on success", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { access_token: "token-123", must_change_password: false }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await authApi.acceptInvite("invite-token-abc", "new-password");

    expect(result).toEqual({ accessToken: "token-123", mustChangePassword: false });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/accept-invite",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({ token: "invite-token-abc", password: "new-password" }),
      }),
    );
  });

  it("throws an ApiError when the invite is invalid or expired", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(401, { error: { code: "invite_invalid", message: "invite is invalid or has expired" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.acceptInvite("bad-token", "new-password")).rejects.toMatchObject({
      code: "invite_invalid",
    });
  });
});

describe("authApi.previewInvite", () => {
  it("returns the username on success", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { username: "alice" }));
    vi.stubGlobal("fetch", fetchMock);

    const result = await authApi.previewInvite("invite-token-abc");

    expect(result).toBe("alice");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/accept-invite?token=invite-token-abc",
      expect.objectContaining({ credentials: "include" }),
    );
  });

  it("throws an ApiError when the invite is invalid or expired", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(401, { error: { code: "invite_invalid", message: "invite is invalid or has expired" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.previewInvite("bad-token")).rejects.toMatchObject({
      code: "invite_invalid",
    });
  });
});

describe("authApi.refresh", () => {
  it("sends no body and includes credentials", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { access_token: "new-token" }));
    vi.stubGlobal("fetch", fetchMock);

    const result = await authApi.refresh();

    expect(result).toEqual({ accessToken: "new-token" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/refresh",
      expect.objectContaining({ method: "POST", credentials: "include" }),
    );
  });

  it("throws on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(401, { error: { code: "unauthorized", message: "missing refresh token" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.refresh()).rejects.toMatchObject({ code: "unauthorized" });
  });
});

describe("authApi.me", () => {
  it("sends the access token as a bearer header and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 1,
        username: "admin",
        must_change_password: false,
        email: "admin@example.com",
        email_reminder_channel_available: true,
        synced_device_reminders_enabled: false,
        is_admin: true,
        week_start: 1,
        default_view: "week",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const user = await authApi.me("token-123");

    expect(user).toEqual({
      id: 1,
      username: "admin",
      mustChangePassword: false,
      email: "admin@example.com",
      emailReminderChannelAvailable: true,
      syncedDeviceRemindersEnabled: false,
      isAdmin: true,
      weekStart: 1,
      defaultView: "week",
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/me",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws an ApiError with code password_change_required when blocked", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(403, { error: { code: "password_change_required", message: "password must be changed" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.me("token-123")).rejects.toMatchObject({
      code: "password_change_required",
      status: 403,
    });
  });
});

describe("authApi.logout", () => {
  it("resolves without a body on 204", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.logout()).resolves.toBeUndefined();
  });
});

describe("authApi.changePassword", () => {
  it("sends both passwords and the bearer token, and returns the new access token", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { access_token: "new-token-456" }));
    vi.stubGlobal("fetch", fetchMock);

    const result = await authApi.changePassword("token-123", "old-pw", "new-pw");

    expect(result).toEqual({ accessToken: "new-token-456" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/change-password",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ current_password: "old-pw", new_password: "new-pw" }),
      }),
    );
  });

  it("throws on an invalid current password", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(401, { error: { code: "invalid_credentials", message: "current password is incorrect" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.changePassword("token-123", "wrong", "new-pw")).rejects.toMatchObject({
      code: "invalid_credentials",
    });
  });
});

describe("authApi.updateEmail", () => {
  it("sends the email and bearer token, and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 1,
        username: "admin",
        must_change_password: false,
        email: "admin@example.com",
        email_reminder_channel_available: true,
        is_admin: false,
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const user = await authApi.updateEmail("token-123", "admin@example.com");

    expect(user).toEqual({
      id: 1,
      username: "admin",
      mustChangePassword: false,
      email: "admin@example.com",
      emailReminderChannelAvailable: true,
      isAdmin: false,
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/email",
      expect.objectContaining({
        method: "PUT",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ email: "admin@example.com" }),
      }),
    );
  });

  it("throws on an invalid email address", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, { error: { code: "invalid_request", message: "email is not a valid address" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.updateEmail("token-123", "not-an-email")).rejects.toMatchObject({
      code: "invalid_request",
    });
  });
});

describe("authApi.updateUsername", () => {
  it("sends the username and bearer token, and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 1,
        username: "newname",
        must_change_password: false,
        email: null,
        email_reminder_channel_available: false,
        is_admin: false,
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const user = await authApi.updateUsername("token-123", "newname");

    expect(user).toEqual({
      id: 1,
      username: "newname",
      mustChangePassword: false,
      email: null,
      emailReminderChannelAvailable: false,
      isAdmin: false,
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/username",
      expect.objectContaining({
        method: "PUT",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ username: "newname" }),
      }),
    );
  });

  it("throws username_taken on a conflict", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(409, { error: { code: "username_taken", message: "username is already taken" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.updateUsername("token-123", "bob")).rejects.toMatchObject({
      code: "username_taken",
    });
  });
});

describe("authApi.updateSyncedDeviceReminders", () => {
  it("sends the preference and bearer token, and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 1,
        username: "admin",
        must_change_password: false,
        email: null,
        email_reminder_channel_available: false,
        synced_device_reminders_enabled: true,
        is_admin: false,
        week_start: 1,
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const user = await authApi.updateSyncedDeviceReminders("token-123", true);

    expect(user).toEqual({
      id: 1,
      username: "admin",
      mustChangePassword: false,
      email: null,
      emailReminderChannelAvailable: false,
      syncedDeviceRemindersEnabled: true,
      isAdmin: false,
      weekStart: 1,
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/synced-device-reminders",
      expect.objectContaining({
        method: "PUT",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ synced_device_reminders_enabled: true }),
      }),
    );
  });

  it("throws on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(500, { error: { code: "internal_error", message: "failed to update reminder preference" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.updateSyncedDeviceReminders("token-123", true)).rejects.toMatchObject({
      code: "internal_error",
    });
  });
});

describe("authApi.updatePreferences", () => {
  it("sends week_start and the bearer token, and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 1,
        username: "admin",
        must_change_password: false,
        email: null,
        email_reminder_channel_available: false,
        synced_device_reminders_enabled: false,
        is_admin: false,
        week_start: 0,
        default_view: "week",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const user = await authApi.updatePreferences("token-123", { weekStart: 0 });

    expect(user).toEqual({
      id: 1,
      username: "admin",
      mustChangePassword: false,
      email: null,
      emailReminderChannelAvailable: false,
      syncedDeviceRemindersEnabled: false,
      isAdmin: false,
      weekStart: 0,
      defaultView: "week",
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/preferences",
      expect.objectContaining({
        method: "PATCH",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ week_start: 0 }),
      }),
    );
  });

  it("sends default_view and the bearer token, and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 1,
        username: "admin",
        must_change_password: false,
        email: null,
        email_reminder_channel_available: false,
        synced_device_reminders_enabled: false,
        is_admin: false,
        week_start: 1,
        default_view: "month",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const user = await authApi.updatePreferences("token-123", { defaultView: "month" });

    expect(user.defaultView).toBe("month");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/preferences",
      expect.objectContaining({
        method: "PATCH",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ default_view: "month" }),
      }),
    );
  });

  it("sends time_format and the bearer token, and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 1,
        username: "admin",
        must_change_password: false,
        email: null,
        email_reminder_channel_available: false,
        synced_device_reminders_enabled: false,
        is_admin: false,
        week_start: 1,
        default_view: "week",
        time_format: "12h",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const user = await authApi.updatePreferences("token-123", { timeFormat: "12h" });

    expect(user.timeFormat).toBe("12h");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/preferences",
      expect.objectContaining({
        method: "PATCH",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ time_format: "12h" }),
      }),
    );
  });

  it("sends both working hours bounds and the bearer token, and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 1,
        username: "admin",
        must_change_password: false,
        email: null,
        email_reminder_channel_available: false,
        synced_device_reminders_enabled: false,
        is_admin: false,
        week_start: 1,
        default_view: "week",
        time_format: "24h",
        working_hours_start: 9,
        working_hours_end: 17,
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const user = await authApi.updatePreferences("token-123", { workingHours: { start: 9, end: 17 } });

    expect(user.workingHoursStart).toBe(9);
    expect(user.workingHoursEnd).toBe(17);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/preferences",
      expect.objectContaining({
        method: "PATCH",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ working_hours_start: 9, working_hours_end: 17 }),
      }),
    );
  });

  it("sends both working hours bounds as null when clearing", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 1,
        username: "admin",
        must_change_password: false,
        email: null,
        email_reminder_channel_available: false,
        synced_device_reminders_enabled: false,
        is_admin: false,
        week_start: 1,
        default_view: "week",
        time_format: "24h",
        working_hours_start: null,
        working_hours_end: null,
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const user = await authApi.updatePreferences("token-123", { workingHours: null });

    expect(user.workingHoursStart).toBeNull();
    expect(user.workingHoursEnd).toBeNull();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/preferences",
      expect.objectContaining({
        body: JSON.stringify({ working_hours_start: null, working_hours_end: null }),
      }),
    );
  });

  it("throws on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, { error: { code: "invalid_request", message: "week_start must be between 0 and 6" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.updatePreferences("token-123", { weekStart: 7 })).rejects.toMatchObject({
      code: "invalid_request",
    });
  });
});
