import { afterEach, describe, expect, it, vi } from "vitest";
import { accountsApi } from "./accountsApi";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("accountsApi.list", () => {
  it("sends the bearer token and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, [
        {
          id: 1,
          username: "admin",
          is_admin: true,
          is_disabled: false,
          must_change_password: false,
          created_at: "2026-01-01T00:00:00Z",
        },
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await accountsApi.list("token-123");

    expect(result).toEqual([
      {
        id: 1,
        username: "admin",
        isAdmin: true,
        isDisabled: false,
        mustChangePassword: false,
        createdAt: "2026-01-01T00:00:00Z",
      },
    ]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/accounts/",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws an ApiError when the caller is not an admin", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(403, { error: { code: "forbidden", message: "admin access required" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(accountsApi.list("token-123")).rejects.toMatchObject({ code: "forbidden" });
  });
});

describe("accountsApi.create", () => {
  it("sends the username and temporary password, and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(201, {
        id: 2,
        username: "kid",
        is_admin: false,
        is_disabled: false,
        must_change_password: true,
        created_at: "2026-01-01T00:00:00Z",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await accountsApi.create("token-123", "kid", "temp-pass");

    expect(result).toEqual({
      id: 2,
      username: "kid",
      isAdmin: false,
      isDisabled: false,
      mustChangePassword: true,
      createdAt: "2026-01-01T00:00:00Z",
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/accounts/",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ username: "kid", password: "temp-pass" }),
      }),
    );
  });

  it("throws an explanation on a duplicate username", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(409, { error: { code: "username_taken", message: "username is already taken" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(accountsApi.create("token-123", "admin", "temp-pass")).rejects.toMatchObject({
      code: "username_taken",
    });
  });
});
