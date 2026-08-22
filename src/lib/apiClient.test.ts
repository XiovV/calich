import { afterEach, describe, expect, it, vi } from "vitest";
import { authedFetch, setSessionRefresher } from "./apiClient";

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

describe("authedFetch", () => {
  it("returns a non-401 response untouched, without refreshing", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { ok: true }));
    vi.stubGlobal("fetch", fetchMock);
    const refresher = vi.fn();
    setSessionRefresher(refresher);

    const response = await authedFetch("token-1", "/api/events/", { credentials: "include" });

    expect(response.status).toBe(200);
    expect(refresher).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/events/",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-1" },
      }),
    );
  });

  it("refreshes once and retries with the new token on a 401", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(emptyResponse(401))
      .mockResolvedValueOnce(jsonResponse(200, { ok: true }));
    vi.stubGlobal("fetch", fetchMock);
    const refresher = vi.fn().mockResolvedValue("token-2");
    setSessionRefresher(refresher);

    const response = await authedFetch("token-1", "/api/events/", { credentials: "include" });

    expect(response.status).toBe(200);
    expect(refresher).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/events/",
      expect.objectContaining({ headers: { Authorization: "Bearer token-1" } }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/events/",
      expect.objectContaining({ headers: { Authorization: "Bearer token-2" } }),
    );
  });

  it("shares a single in-flight refresh across concurrent 401s", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(401));
    vi.stubGlobal("fetch", fetchMock);
    let resolveRefresh: (token: string) => void = () => {};
    const refresher = vi.fn().mockImplementation(
      () => new Promise<string>((resolve) => (resolveRefresh = resolve)),
    );
    setSessionRefresher(refresher);

    const firstCall = authedFetch("token-1", "/api/events/", {});
    const secondCall = authedFetch("token-1", "/api/calendars/", {});

    // Let both initial 401 responses resolve before releasing the refresh.
    await Promise.resolve();
    await Promise.resolve();
    resolveRefresh("token-2");

    fetchMock.mockResolvedValue(jsonResponse(200, { ok: true }));
    await Promise.all([firstCall, secondCall]);

    expect(refresher).toHaveBeenCalledTimes(1);
  });

  it("surfaces a 401 that recurs on the retry, rather than retrying again", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(401));
    vi.stubGlobal("fetch", fetchMock);
    const refresher = vi.fn().mockResolvedValue("token-2");
    setSessionRefresher(refresher);

    const response = await authedFetch("token-1", "/api/events/", {});

    expect(response.status).toBe(401);
    expect(refresher).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("returns a 401 untouched, without refreshing, when shouldRefreshOn401 resolves false", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(401, { error: { code: "invalid_credentials" } }));
    vi.stubGlobal("fetch", fetchMock);
    const refresher = vi.fn();
    setSessionRefresher(refresher);
    const shouldRefreshOn401 = vi.fn().mockResolvedValue(false);

    const response = await authedFetch("token-1", "/api/auth/change-password", {}, { shouldRefreshOn401 });

    expect(response.status).toBe(401);
    expect(refresher).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(shouldRefreshOn401).toHaveBeenCalledTimes(1);
    // The predicate is handed a clone — the original response's body must
    // still be readable by the caller afterward.
    await expect(response.json()).resolves.toEqual({ error: { code: "invalid_credentials" } });
  });

  it("refreshes and retries when shouldRefreshOn401 resolves true", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(401, { error: { code: "unauthorized" } }))
      .mockResolvedValueOnce(jsonResponse(200, { ok: true }));
    vi.stubGlobal("fetch", fetchMock);
    const refresher = vi.fn().mockResolvedValue("token-2");
    setSessionRefresher(refresher);
    const shouldRefreshOn401 = vi.fn().mockResolvedValue(true);

    const response = await authedFetch("token-1", "/api/auth/change-password", {}, { shouldRefreshOn401 });

    expect(response.status).toBe(200);
    expect(refresher).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("returns the original 401 untouched when the refresh itself fails", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(401));
    vi.stubGlobal("fetch", fetchMock);
    const refresher = vi.fn().mockRejectedValue(new Error("refresh failed"));
    setSessionRefresher(refresher);

    const response = await authedFetch("token-1", "/api/events/", {});

    expect(response.status).toBe(401);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
