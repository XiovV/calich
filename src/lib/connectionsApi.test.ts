import { afterEach, describe, expect, it, vi } from "vitest";
import { connectionsApi } from "./connectionsApi";

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
    const fetchMock = vi.fn().mockResolvedValue(
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

    await connectionsApi.disconnect("token-123", 1);

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/connections/1",
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

    await expect(connectionsApi.disconnect("token-123", 999)).rejects.toMatchObject({
      code: "not_found",
    });
  });
});
