import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { connectionsApi } from "./connectionsApi";
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

describe("connectionsApi.list", () => {
  it("sends the bearer token and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, [
        {
          id: 1,
          provider: "google",
          account_email: "someone@gmail.com",
          status: "live",
          created_at: "2026-01-01T00:00:00Z",
        },
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await connectionsApi.list("token-123");

    expect(result).toEqual([
      {
        id: 1,
        provider: "google",
        accountEmail: "someone@gmail.com",
        status: "live",
        createdAt: "2026-01-01T00:00:00Z",
      },
    ]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/connections/",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws an ApiError on failure", async () => {
    // mockImplementation, not mockResolvedValue: a 401 makes authedFetch
    // attempt a session refresh over the same stubbed fetch, so the mock
    // must hand back a fresh Response per call — a single shared instance's
    // body would already be consumed by the refresh attempt's own read
    // before errorFromResponse ever gets to it (mirrors calendarsApi.test.ts's
    // own 401 case).
    const fetchMock = vi.fn().mockImplementation(() =>
      jsonResponse(401, { error: { code: "unauthorized", message: "authentication required" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(connectionsApi.list("token-123")).rejects.toMatchObject({ code: "unauthorized" });
  });
});

describe("connectionsApi.connectGoogle", () => {
  it("sends the bearer token and returns the authorize url", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { url: "https://accounts.google.com/o/oauth2/v2/auth?client_id=abc" }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await connectionsApi.connectGoogle("token-123");

    expect(result).toBe("https://accounts.google.com/o/oauth2/v2/auth?client_id=abc");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/connections/google/connect",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws when google isn't configured", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "google provider is not configured" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(connectionsApi.connectGoogle("token-123")).rejects.toMatchObject({ code: "not_found" });
  });
});

describe("connectionsApi.disconnect", () => {
  it("sends a DELETE with the bearer token", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await connectionsApi.disconnect("token-123", 1, "keep");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/connections/1?disposition=keep",
      expect.objectContaining({
        method: "DELETE",
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws when the connection doesn't exist", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "connection not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(connectionsApi.disconnect("token-123", 999, "keep")).rejects.toMatchObject({
      code: "not_found",
    });
  });
});

describe("connectionsApi.disconnectImpact", () => {
  it("GETs the impact and returns the linked calendars", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { linkedCalendars: [{ id: "cal-1", name: "Work", shareCount: 2 }] }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const impact = await connectionsApi.disconnectImpact("token-123", 5);

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/connections/5/impact",
      expect.objectContaining({ credentials: "include" }),
    );
    expect(impact.linkedCalendars).toEqual([{ id: "cal-1", name: "Work", shareCount: 2 }]);
  });
});

describe("connectionsApi.listPickerCalendars", () => {
  it("sends the bearer token and the active workspace, and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, [
        { id: "primary", name: "someone@gmail.com", color: "#0B8043FF", selected: true, writable: true },
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await connectionsApi.listPickerCalendars("token-123", 1);

    expect(result).toEqual([
      { id: "primary", name: "someone@gmail.com", color: "#0B8043FF", selected: true, writable: true },
    ]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/connections/1/calendars",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123", "X-Workspace-Id": "7" },
      }),
    );
  });

  it("throws an ApiError on failure", async () => {
    // mockImplementation, not mockResolvedValue — see connectionsApi.list's
    // own 401 test for why a shared Response instance doesn't survive the
    // session-refresh attempt authedFetch makes on a 401.
    const fetchMock = vi.fn().mockImplementation(() =>
      jsonResponse(400, { error: { code: "invalid_request", message: "this connection has no usable google access token" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(connectionsApi.listPickerCalendars("token-123", 1)).rejects.toMatchObject({
      code: "invalid_request",
    });
  });
});

describe("connectionsApi.importCalendars", () => {
  it("sends the selected ids and the active workspace, and returns the created calendars", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(201, [{ id: "cal-1", name: "someone@gmail.com", color: "#0B8043FF" }]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await connectionsApi.importCalendars("token-123", 1, ["primary"]);

    expect(result).toEqual([{ id: "cal-1", name: "someone@gmail.com", color: "#0B8043FF" }]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/connections/1/calendars",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: {
          Authorization: "Bearer token-123",
          "X-Workspace-Id": "7",
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ calendarIds: ["primary"] }),
      }),
    );
  });
});
