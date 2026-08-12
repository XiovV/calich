import { afterEach, describe, expect, it, vi } from "vitest";
import { notificationsApi } from "./notificationsApi";

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

const wireNotification = {
  id: 1,
  eventId: "evt-1",
  kind: "reminder" as const,
  title: "Standup",
  occurrenceStart: "2026-01-01T09:00:00Z",
  firedAt: "2026-01-01T08:50:00Z",
  seen: false,
};

describe("notificationsApi.list", () => {
  it("sends the bearer token and maps wire notifications to Notification objects", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, [wireNotification]));
    vi.stubGlobal("fetch", fetchMock);

    const notifications = await notificationsApi.list("token-123");

    expect(notifications).toEqual([
      {
        id: 1,
        eventId: "evt-1",
        kind: "reminder",
        title: "Standup",
        occurrenceStart: new Date("2026-01-01T09:00:00Z"),
        firedAt: new Date("2026-01-01T08:50:00Z"),
        seen: false,
      },
    ]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/notifications/",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("maps an invite notification's null occurrenceStart to null", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, [
        {
          id: 2,
          eventId: "evt-1",
          kind: "invite",
          title: "Standup",
          occurrenceStart: null,
          firedAt: "2026-01-01T08:50:00Z",
          seen: false,
        },
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const notifications = await notificationsApi.list("token-123");

    expect(notifications).toEqual([
      {
        id: 2,
        eventId: "evt-1",
        kind: "invite",
        title: "Standup",
        occurrenceStart: null,
        firedAt: new Date("2026-01-01T08:50:00Z"),
        seen: false,
      },
    ]);
  });

  it("throws an ApiError on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(401, { error: { code: "unauthorized", message: "missing token" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(notificationsApi.list("token-123")).rejects.toMatchObject({
      code: "unauthorized",
    });
  });
});

describe("notificationsApi.markSeen", () => {
  it("posts to the seen endpoint with the bearer token", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await notificationsApi.markSeen("token-123");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/notifications/seen",
      expect.objectContaining({
        method: "POST",
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

    await expect(notificationsApi.markSeen("token-123")).rejects.toMatchObject({
      code: "unauthorized",
    });
  });
});
