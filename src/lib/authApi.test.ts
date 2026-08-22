import { afterEach, describe, expect, it, vi } from "vitest";
import { setSessionRefresher } from "./apiClient";
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
      jsonResponse(200, { access_token: "token-123", must_change_password: true, is_disabled: false }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await authApi.login("admin@example.com", "admin");

    expect(result).toEqual({ accessToken: "token-123", mustChangePassword: true, isDisabled: false });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/login",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({ email: "admin@example.com", password: "admin" }),
      }),
    );
  });

  it("throws an ApiError with the backend's code and message on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(401, { error: { code: "invalid_credentials", message: "invalid email or password" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.login("admin", "wrong")).rejects.toMatchObject({
      code: "invalid_credentials",
      message: "invalid email or password",
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

  it("reports is_disabled for a Disabled account instead of refusing (ADR-0044)", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { access_token: "token-123", must_change_password: false, is_disabled: true }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await authApi.login("alice", "hunter2");

    expect(result).toEqual({ accessToken: "token-123", mustChangePassword: false, isDisabled: true });
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
        name: "admin",
        must_change_password: false,
        email: "admin@example.com",
        email_reminder_channel_available: true,
        synced_device_reminders_enabled: false,
        week_start: 1,
        default_view: "week",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const user = await authApi.me("token-123");

    expect(user).toEqual({
      id: 1,
      name: "admin",
      mustChangePassword: false,
      email: "admin@example.com",
      emailReminderChannelAvailable: true,
      syncedDeviceRemindersEnabled: false,
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

  // #249: a 401 here means the current password was wrong, not an expired
  // access token, so it must not trigger a refresh-and-retry — that used to
  // cost a second bcrypt compare and a refresh-token rotation on every typo.
  it("does not refresh the session on an invalid current password", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(401, { error: { code: "invalid_credentials", message: "current password is incorrect" } }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const refresher = vi.fn().mockResolvedValue("token-456");
    setSessionRefresher(refresher);

    await expect(authApi.changePassword("token-123", "wrong", "new-pw")).rejects.toMatchObject({
      code: "invalid_credentials",
    });

    expect(refresher).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  // #249's fix must not break the case it looks identical to at the HTTP
  // level: RequireAuth's 401 "unauthorized" for an actually expired access
  // token still has to refresh and retry transparently, same as every other
  // authedFetch caller.
  it("still refreshes and retries on an expired access token", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse(401, { error: { code: "unauthorized", message: "invalid or expired access token" } }),
      )
      .mockResolvedValueOnce(jsonResponse(200, { access_token: "new-token-456" }));
    vi.stubGlobal("fetch", fetchMock);
    const refresher = vi.fn().mockResolvedValue("token-789");
    setSessionRefresher(refresher);

    const result = await authApi.changePassword("token-123", "old-pw", "new-pw");

    expect(result).toEqual({ accessToken: "new-token-456" });
    expect(refresher).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/auth/change-password",
      expect.objectContaining({ headers: expect.objectContaining({ Authorization: "Bearer token-789" }) }),
    );
  });
});

describe("authApi.updateEmail", () => {
  it("sends the email and bearer token, and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 1,
        name: "admin",
        must_change_password: false,
        email: "admin@example.com",
        email_reminder_channel_available: true,
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const user = await authApi.updateEmail("token-123", "admin@example.com");

    expect(user).toEqual({
      id: 1,
      name: "admin",
      mustChangePassword: false,
      email: "admin@example.com",
      emailReminderChannelAvailable: true,
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

describe("authApi.updateName", () => {
  it("sends the name and bearer token, and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 1,
        name: "New Name",
        must_change_password: false,
        email: "admin@example.com",
        email_reminder_channel_available: false,
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const user = await authApi.updateName("token-123", "New Name");

    expect(user).toEqual({
      id: 1,
      name: "New Name",
      mustChangePassword: false,
      email: "admin@example.com",
      emailReminderChannelAvailable: false,
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/name",
      expect.objectContaining({
        method: "PUT",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ name: "New Name" }),
      }),
    );
  });

  it("throws on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, { error: { code: "invalid_request", message: "name must not be empty" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.updateName("token-123", "")).rejects.toMatchObject({
      code: "invalid_request",
    });
  });
});

describe("authApi.setupStatus", () => {
  it("reports whether the instance has any accounts yet", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { has_accounts: false }));
    vi.stubGlobal("fetch", fetchMock);

    const result = await authApi.setupStatus();

    expect(result).toEqual({ hasAccounts: false });
    expect(fetchMock).toHaveBeenCalledWith("/api/auth/setup-status", { credentials: "include" });
  });
});

describe("authApi.updateSyncedDeviceReminders", () => {
  it("sends the preference and bearer token, and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 1,
        name: "admin",
        must_change_password: false,
        email: "admin@example.com",
        email_reminder_channel_available: false,
        synced_device_reminders_enabled: true,
        week_start: 1,
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const user = await authApi.updateSyncedDeviceReminders("token-123", true);

    expect(user).toEqual({
      id: 1,
      name: "admin",
      mustChangePassword: false,
      email: "admin@example.com",
      emailReminderChannelAvailable: false,
      syncedDeviceRemindersEnabled: true,
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
        name: "admin",
        must_change_password: false,
        email: "admin@example.com",
        email_reminder_channel_available: false,
        synced_device_reminders_enabled: false,
        week_start: 0,
        default_view: "week",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const user = await authApi.updatePreferences("token-123", { weekStart: 0 });

    expect(user).toEqual({
      id: 1,
      name: "admin",
      mustChangePassword: false,
      email: "admin@example.com",
      emailReminderChannelAvailable: false,
      syncedDeviceRemindersEnabled: false,
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
        name: "admin",
        must_change_password: false,
        email: "admin@example.com",
        email_reminder_channel_available: false,
        synced_device_reminders_enabled: false,
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
        name: "admin",
        must_change_password: false,
        email: "admin@example.com",
        email_reminder_channel_available: false,
        synced_device_reminders_enabled: false,
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
        name: "admin",
        must_change_password: false,
        email: "admin@example.com",
        email_reminder_channel_available: false,
        synced_device_reminders_enabled: false,
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
        name: "admin",
        must_change_password: false,
        email: "admin@example.com",
        email_reminder_channel_available: false,
        synced_device_reminders_enabled: false,
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
