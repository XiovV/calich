import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./apiClient";
import { calendarsApi } from "./calendarsApi";
import { useWorkspacesStore } from "./workspacesStore";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function emptyResponse(status: number): Response {
  return new Response(null, { status });
}

beforeEach(() => {
  useWorkspacesStore.setState({ activeWorkspaceId: 7 });
});

afterEach(() => {
  vi.unstubAllGlobals();
  useWorkspacesStore.setState({ activeWorkspaceId: null });
});

describe("calendarsApi.list", () => {
  it("sends the bearer token and returns the calendars", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, [{ id: "cal-1", name: "Personal", color: "peacock" }]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const calendars = await calendarsApi.list("token-123");

    expect(calendars).toEqual([{ id: "cal-1", name: "Personal", color: "peacock" }]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123", "X-Workspace-Id": "7" },
      }),
    );
  });

  it("throws an ApiError on failure", async () => {
    // A fresh Response per call: importing calendarsApi now pulls in
    // workspacesStore -> authStore, which registers a real session
    // refresher (apiClient) at module init. A 401 here triggers that
    // refresher, which calls the same mocked fetch a second time (against
    // the refresh endpoint) — a single shared Response instance would have
    // its body already consumed by the second call.
    const fetchMock = vi.fn().mockImplementation(() =>
      Promise.resolve(
        jsonResponse(401, { error: { code: "unauthorized", message: "missing token" } }),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(calendarsApi.list("token-123")).rejects.toMatchObject({
      code: "unauthorized",
      status: 401,
    });
  });
});

describe("calendarsApi.create", () => {
  it("posts the calendar and returns the created record", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(201, { id: "cal-1", name: "Personal", color: "peacock" }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const created = await calendarsApi.create("token-123", {
      id: "cal-1",
      name: "Personal",
      color: "peacock",
    });

    expect(created).toEqual({ id: "cal-1", name: "Personal", color: "peacock" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({
          Authorization: "Bearer token-123",
          "X-Workspace-Id": "7",
        }),
        body: JSON.stringify({ id: "cal-1", name: "Personal", color: "peacock" }),
      }),
    );
  });

  it("throws an ApiError with the backend's code on validation failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, { error: { code: "invalid_request", message: "color is not a recognized calendar color" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      calendarsApi.create("token-123", { id: "cal-1", name: "Personal", color: "peacock" }),
    ).rejects.toBeInstanceOf(ApiError);
  });
});

describe("calendarsApi.update", () => {
  it("patches the calendar and returns the updated record", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { id: "cal-1", name: "Renamed", color: "tomato" }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const updated = await calendarsApi.update("token-123", "cal-1", {
      name: "Renamed",
      color: "tomato",
    });

    expect(updated).toEqual({ id: "cal-1", name: "Renamed", color: "tomato" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/cal-1",
      expect.objectContaining({
        method: "PATCH",
        credentials: "include",
        body: JSON.stringify({ name: "Renamed", color: "tomato" }),
      }),
    );
  });

  it("includes url and keepAlarms when editing a Subscription", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: "cal-1",
        name: "Team Holidays",
        color: "#8E44ADFF",
        sourceUrl: "https://new.example.com/feed.ics",
        keepAlarms: true,
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await calendarsApi.update("token-123", "cal-1", {
      name: "Team Holidays",
      color: "#8E44ADFF",
      keepAlarms: true,
      url: "https://new.example.com/feed.ics",
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/cal-1",
      expect.objectContaining({
        method: "PATCH",
        body: JSON.stringify({
          name: "Team Holidays",
          color: "#8E44ADFF",
          keepAlarms: true,
          url: "https://new.example.com/feed.ics",
        }),
      }),
    );
  });
});

describe("calendarsApi.updateColor", () => {
  it("patches only the color", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { id: "cal-1", name: "Family", color: "#654321FF" }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const updated = await calendarsApi.updateColor("token-123", "cal-1", "#654321");

    expect(updated).toEqual({ id: "cal-1", name: "Family", color: "#654321FF" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/cal-1",
      expect.objectContaining({
        method: "PATCH",
        credentials: "include",
        body: JSON.stringify({ color: "#654321" }),
      }),
    );
  });

  it("throws an ApiError on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(403, { error: { code: "forbidden", message: "only the calendar's owner may change its name" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      calendarsApi.updateColor("token-123", "cal-1", "#654321"),
    ).rejects.toMatchObject({ code: "forbidden" });
  });
});

describe("calendarsApi.previewSubscription", () => {
  it("posts the url with dryRun=1 and returns the preview", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { name: "Team Holidays", color: "#8E44ADFF", eventCount: 2 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const preview = await calendarsApi.previewSubscription(
      "token-123",
      "https://example.com/feed.ics",
    );

    expect(preview).toEqual({ name: "Team Holidays", color: "#8E44ADFF", eventCount: 2 });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/subscribe?dryRun=1",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ url: "https://example.com/feed.ics" }),
      }),
    );
  });

  // Omitting this header made the server refuse every URL as a non-Member
  // before it ever reached the feed, which gated the whole subscribe flow
  // (#225) — see previewSubscription's own comment.
  it("sends the active workspace id", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { name: "Team Holidays", color: "#8E44ADFF", eventCount: 2 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await calendarsApi.previewSubscription("token-123", "https://example.com/feed.ics");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/subscribe?dryRun=1",
      expect.objectContaining({
        headers: {
          Authorization: "Bearer token-123",
          "Content-Type": "application/json",
          "X-Workspace-Id": "7",
        },
      }),
    );
  });

  it("throws an ApiError on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, { error: { code: "invalid_request", message: "subscription URL is invalid" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      calendarsApi.previewSubscription("token-123", "not a url"),
    ).rejects.toMatchObject({ code: "invalid_request" });
  });
});

describe("calendarsApi.subscribe", () => {
  it("posts with dryRun=0 and returns the created calendar", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(201, {
        id: "cal-1",
        name: "Team Holidays",
        color: "#8E44ADFF",
        sourceUrl: "https://example.com/feed.ics",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const calendar = await calendarsApi.subscribe("token-123", {
      url: "https://example.com/feed.ics",
      name: "Team Holidays",
      color: "#8E44ADFF",
      keepAlarms: false,
    });

    expect(calendar).toEqual({
      id: "cal-1",
      name: "Team Holidays",
      color: "#8E44ADFF",
      sourceUrl: "https://example.com/feed.ics",
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/subscribe?dryRun=0",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({ "X-Workspace-Id": "7" }),
        body: JSON.stringify({
          url: "https://example.com/feed.ics",
          name: "Team Holidays",
          color: "#8E44ADFF",
          keepAlarms: false,
        }),
      }),
    );
  });
});

describe("calendarsApi.remove", () => {
  it("sends a DELETE request", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await expect(calendarsApi.remove("token-123", "cal-1")).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/cal-1",
      expect.objectContaining({
        method: "DELETE",
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "calendar not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(calendarsApi.remove("token-123", "cal-1")).rejects.toMatchObject({
      code: "not_found",
    });
  });
});

describe("calendarsApi.get", () => {
  it("returns the calendar", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { id: "cal-1", name: "Personal", color: "peacock" }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const calendar = await calendarsApi.get("token-123", "cal-1");

    expect(calendar).toEqual({ id: "cal-1", name: "Personal", color: "peacock" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/cal-1",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "calendar not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(calendarsApi.get("token-123", "cal-1")).rejects.toMatchObject({
      code: "not_found",
    });
  });
});

describe("calendarsApi.refresh", () => {
  it("posts to the refresh endpoint and returns the summary", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        notModified: false,
        created: 1,
        updated: 0,
        tombstoned: 0,
        unparseable: 0,
        noOp: 0,
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await calendarsApi.refresh("token-123", "cal-1");

    expect(result).toEqual({
      notModified: false,
      created: 1,
      updated: 0,
      tombstoned: 0,
      unparseable: 0,
      noOp: 0,
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/cal-1/refresh",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, { error: { code: "invalid_request", message: "not a subscribed calendar" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(calendarsApi.refresh("token-123", "cal-1")).rejects.toMatchObject({
      code: "invalid_request",
    });
  });
});

describe("calendarsApi.listShares", () => {
  it("returns the calendar's shares", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, [
        { userId: 2, name: "bob", email: "bob@example.com", role: "editor", createdAt: "2026-01-01T00:00:00Z" },
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const shares = await calendarsApi.listShares("token-123", "cal-1");

    expect(shares).toEqual([
      { userId: 2, name: "bob", email: "bob@example.com", role: "editor", createdAt: "2026-01-01T00:00:00Z" },
    ]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/cal-1/shares",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "calendar not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(calendarsApi.listShares("token-123", "cal-1")).rejects.toMatchObject({
      code: "not_found",
    });
  });
});

describe("calendarsApi.share", () => {
  it("posts the email and role and returns the created share", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { userId: 2, name: "bob", email: "bob@example.com", role: "viewer", createdAt: "2026-01-01T00:00:00Z" }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const share = await calendarsApi.share("token-123", "cal-1", "bob@example.com", "viewer");

    expect(share).toEqual({ userId: 2, name: "bob", email: "bob@example.com", role: "viewer", createdAt: "2026-01-01T00:00:00Z" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/cal-1/shares",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ email: "bob@example.com", role: "viewer" }),
      }),
    );
  });

  it("throws an ApiError with the backend's specific message on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, { error: { code: "invalid_request", message: "user not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      calendarsApi.share("token-123", "cal-1", "ghost", "viewer"),
    ).rejects.toMatchObject({ code: "invalid_request", message: "user not found" });
  });
});

describe("calendarsApi.leave", () => {
  it("posts to the leave endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await expect(calendarsApi.leave("token-123", "cal-1")).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/cal-1/leave",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "calendar not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(calendarsApi.leave("token-123", "cal-1")).rejects.toMatchObject({
      code: "not_found",
    });
  });
});

describe("calendarsApi.revokeShare", () => {
  it("sends a DELETE request to the share's user id", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await expect(calendarsApi.revokeShare("token-123", "cal-1", 2)).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/cal-1/shares/2",
      expect.objectContaining({
        method: "DELETE",
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "calendar not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(calendarsApi.revokeShare("token-123", "cal-1", 2)).rejects.toMatchObject({
      code: "not_found",
    });
  });
});

describe("calendarsApi.getDefaultReminders", () => {
  it("returns both lists for a calendar with defaults set", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        timed: [{ offsetMinutes: 10, channel: "notification" }],
        allDay: [{ offsetMinutes: 1440, channel: "email" }],
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const defaults = await calendarsApi.getDefaultReminders("token-123", "cal-1");

    expect(defaults).toEqual({
      timed: [{ offsetMinutes: 10, channel: "notification" }],
      allDay: [{ offsetMinutes: 1440, channel: "email" }],
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/calendars/cal-1/default-reminders",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  // An older server serves null rather than [] for a list nobody has ever
  // set (ADR-0064) — the untouched-Calendar case, which is every Calendar
  // until someone opens the edit dialog and saves one. Callers map over
  // both lists, so a null has to arrive here as an empty list.
  it("returns empty lists when the server serves nulls for an untouched calendar", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse(200, { timed: null, allDay: null }));
    vi.stubGlobal("fetch", fetchMock);

    const defaults = await calendarsApi.getDefaultReminders("token-123", "cal-1");

    expect(defaults).toEqual({ timed: [], allDay: [] });
  });
});
