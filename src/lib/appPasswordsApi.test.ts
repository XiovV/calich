import { afterEach, describe, expect, it, vi } from "vitest";
import { appPasswordsApi } from "./appPasswordsApi";

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

describe("appPasswordsApi.list", () => {
  it("sends the bearer token and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, [
        { id: 1, label: "iPhone", created_at: "2026-01-01T00:00:00Z", last_used_at: null },
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await appPasswordsApi.list("token-123");

    expect(result).toEqual([
      { id: 1, label: "iPhone", createdAt: "2026-01-01T00:00:00Z", lastUsedAt: null },
    ]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/app-passwords/",
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

    await expect(appPasswordsApi.list("token-123")).rejects.toMatchObject({ code: "unauthorized" });
  });
});

describe("appPasswordsApi.create", () => {
  it("sends the label and returns the plaintext secret", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(201, {
        id: 1,
        label: "iPhone",
        created_at: "2026-01-01T00:00:00Z",
        last_used_at: null,
        secret: "the-plaintext-secret",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await appPasswordsApi.create("token-123", "iPhone");

    expect(result).toEqual({
      id: 1,
      label: "iPhone",
      createdAt: "2026-01-01T00:00:00Z",
      lastUsedAt: null,
      secret: "the-plaintext-secret",
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/app-passwords/",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ label: "iPhone" }),
      }),
    );
  });

  it("throws on an empty label", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, { error: { code: "invalid_request", message: "label must not be empty" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(appPasswordsApi.create("token-123", "")).rejects.toMatchObject({
      code: "invalid_request",
    });
  });
});

describe("appPasswordsApi.revoke", () => {
  it("sends a DELETE with the bearer token", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await appPasswordsApi.revoke("token-123", 1);

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/app-passwords/1",
      expect.objectContaining({
        method: "DELETE",
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws when the app password doesn't exist", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "app password not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(appPasswordsApi.revoke("token-123", 999)).rejects.toMatchObject({
      code: "not_found",
    });
  });
});
