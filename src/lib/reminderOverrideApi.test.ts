import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./apiClient";
import { reminderOverrideApi } from "./reminderOverrideApi";

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

describe("reminderOverrideApi.get", () => {
  it("sends the bearer token and returns the caller's override", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse(200, { offsetMinutes: 120, channel: "email", muted: true }));
    vi.stubGlobal("fetch", fetchMock);

    const override = await reminderOverrideApi.get("token-123", "event-1");

    expect(override).toEqual({ offsetMinutes: 120, channel: "email", muted: true });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/event-1/reminder-override",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("normalizes a zero-value response into an unset override", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, {}));
    vi.stubGlobal("fetch", fetchMock);

    const override = await reminderOverrideApi.get("token-123", "event-1");

    expect(override).toEqual({ offsetMinutes: undefined, channel: undefined, muted: false });
  });

  it("throws an ApiError on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "event not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(reminderOverrideApi.get("token-123", "event-1")).rejects.toMatchObject({
      code: "not_found",
      status: 404,
    });
  });
});

describe("reminderOverrideApi.set", () => {
  it("puts the override and returns the saved record", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse(200, { offsetMinutes: 30, channel: "notification", muted: false }));
    vi.stubGlobal("fetch", fetchMock);

    const saved = await reminderOverrideApi.set("token-123", "event-1", {
      offsetMinutes: 30,
      channel: "notification",
      muted: false,
    });

    expect(saved).toEqual({ offsetMinutes: 30, channel: "notification", muted: false });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/event-1/reminder-override",
      expect.objectContaining({
        method: "PUT",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ offsetMinutes: 30, channel: "notification", muted: false }),
      }),
    );
  });

  it("omits offsetMinutes and channel from the request body when not overridden", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { muted: true }));
    vi.stubGlobal("fetch", fetchMock);

    await reminderOverrideApi.set("token-123", "event-1", { muted: true });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/event-1/reminder-override",
      expect.objectContaining({ body: JSON.stringify({ muted: true }) }),
    );
  });

  it("throws an ApiError with the backend's code on an invalid channel", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, {
        error: { code: "invalid_request", message: 'reminder channel must be "notification" or "email"' },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      reminderOverrideApi.set("token-123", "event-1", { channel: "sms" as never, muted: false }),
    ).rejects.toBeInstanceOf(ApiError);
  });
});

describe("reminderOverrideApi.clear", () => {
  it("sends a DELETE request", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await expect(reminderOverrideApi.clear("token-123", "event-1")).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/event-1/reminder-override",
      expect.objectContaining({
        method: "DELETE",
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "event not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(reminderOverrideApi.clear("token-123", "event-1")).rejects.toMatchObject({
      code: "not_found",
    });
  });
});
