import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./apiClient";
import { calendarsApi } from "./calendarsApi";

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
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws an ApiError on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(401, { error: { code: "unauthorized", message: "missing token" } }),
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
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
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
