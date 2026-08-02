import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./apiClient";
import { eventsApi } from "./eventsApi";

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

const wireEvent = {
  id: "evt-1",
  calendarId: "cal-1",
  title: "Standup",
  start: "2026-01-01T09:00:00Z",
  end: "2026-01-01T10:00:00Z",
};

describe("eventsApi.list", () => {
  it("sends the bearer token and maps wire events to Event objects", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, [wireEvent]));
    vi.stubGlobal("fetch", fetchMock);

    const events = await eventsApi.list("token-123");

    expect(events).toEqual([
      {
        id: "evt-1",
        calendarId: "cal-1",
        title: "Standup",
        start: new Date("2026-01-01T09:00:00Z"),
        end: new Date("2026-01-01T10:00:00Z"),
      },
    ]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws an ApiError on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(401, { error: { code: "unauthorized", message: "missing token" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(eventsApi.list("token-123")).rejects.toMatchObject({
      code: "unauthorized",
      status: 401,
    });
  });
});

describe("eventsApi.create", () => {
  it("posts ISO timestamps and returns the mapped created event", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(201, wireEvent));
    vi.stubGlobal("fetch", fetchMock);

    const created = await eventsApi.create("token-123", {
      id: "evt-1",
      calendarId: "cal-1",
      title: "Standup",
      start: new Date("2026-01-01T09:00:00Z"),
      end: new Date("2026-01-01T10:00:00Z"),
    });

    expect(created).toEqual({
      id: "evt-1",
      calendarId: "cal-1",
      title: "Standup",
      start: new Date("2026-01-01T09:00:00Z"),
      end: new Date("2026-01-01T10:00:00Z"),
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({
          id: "evt-1",
          calendarId: "cal-1",
          title: "Standup",
          start: "2026-01-01T09:00:00.000Z",
          end: "2026-01-01T10:00:00.000Z",
        }),
      }),
    );
  });

  it("throws an ApiError with the backend's code on validation failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, { error: { code: "invalid_request", message: "end must be after start" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      eventsApi.create("token-123", {
        id: "evt-1",
        calendarId: "cal-1",
        title: "Standup",
        start: new Date("2026-01-01T10:00:00Z"),
        end: new Date("2026-01-01T09:00:00Z"),
      }),
    ).rejects.toBeInstanceOf(ApiError);
  });
});

describe("eventsApi.update", () => {
  it("patches the event and returns the mapped updated record", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { ...wireEvent, title: "Renamed" }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const updated = await eventsApi.update("token-123", "evt-1", {
      calendarId: "cal-1",
      title: "Renamed",
      start: new Date("2026-01-01T09:00:00Z"),
      end: new Date("2026-01-01T10:00:00Z"),
    });

    expect(updated.title).toBe("Renamed");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/evt-1",
      expect.objectContaining({
        method: "PATCH",
        credentials: "include",
        body: JSON.stringify({
          calendarId: "cal-1",
          title: "Renamed",
          start: "2026-01-01T09:00:00.000Z",
          end: "2026-01-01T10:00:00.000Z",
        }),
      }),
    );
  });
});

describe("eventsApi.remove", () => {
  it("sends a DELETE request", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await expect(eventsApi.remove("token-123", "evt-1")).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/evt-1",
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

    await expect(eventsApi.remove("token-123", "evt-1")).rejects.toMatchObject({
      code: "not_found",
    });
  });
});
