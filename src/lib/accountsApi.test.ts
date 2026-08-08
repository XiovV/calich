import { afterEach, describe, expect, it, vi } from "vitest";
import { accountsApi } from "./accountsApi";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function noContentResponse(): Response {
  return new Response(null, { status: 204 });
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

describe("accountsApi.resetPassword", () => {
  it("sends the new temporary password and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 2,
        username: "kid",
        is_admin: false,
        is_disabled: false,
        must_change_password: true,
        created_at: "2026-01-01T00:00:00Z",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await accountsApi.resetPassword("token-123", 2, "new-temp-pass");

    expect(result).toEqual({
      id: 2,
      username: "kid",
      isAdmin: false,
      isDisabled: false,
      mustChangePassword: true,
      createdAt: "2026-01-01T00:00:00Z",
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/accounts/2/reset-password",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ password: "new-temp-pass" }),
      }),
    );
  });

  it("throws an ApiError when the account doesn't exist", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "account not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(accountsApi.resetPassword("token-123", 99, "new-temp-pass")).rejects.toMatchObject({
      code: "not_found",
    });
  });
});

describe("accountsApi.setAdmin", () => {
  it("sends the admin flag and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 2,
        username: "kid",
        is_admin: true,
        is_disabled: false,
        must_change_password: false,
        created_at: "2026-01-01T00:00:00Z",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await accountsApi.setAdmin("token-123", 2, true);

    expect(result.isAdmin).toBe(true);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/accounts/2/admin",
      expect.objectContaining({
        method: "PUT",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ is_admin: true }),
      }),
    );
  });

  it("throws an explanation when demoting the last remaining admin", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(409, {
        error: { code: "last_admin", message: "cannot remove the last remaining admin" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(accountsApi.setAdmin("token-123", 1, false)).rejects.toMatchObject({
      code: "last_admin",
      message: "cannot remove the last remaining admin",
    });
  });
});

describe("accountsApi.setDisabled", () => {
  it("sends the disabled flag and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 2,
        username: "kid",
        is_admin: false,
        is_disabled: true,
        must_change_password: false,
        created_at: "2026-01-01T00:00:00Z",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await accountsApi.setDisabled("token-123", 2, true);

    expect(result.isDisabled).toBe(true);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/accounts/2/disabled",
      expect.objectContaining({
        method: "PUT",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ is_disabled: true }),
      }),
    );
  });

  it("throws an explanation when disabling the last remaining admin", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(409, {
        error: { code: "last_admin", message: "cannot disable the last remaining admin" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(accountsApi.setDisabled("token-123", 1, true)).rejects.toMatchObject({
      code: "last_admin",
      message: "cannot disable the last remaining admin",
    });
  });
});

describe("accountsApi.deleteImpact", () => {
  it("sends the bearer token and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        calendars: [{ id: "cal-1", name: "Family", share_count: 2 }],
        affected_user_count: 2,
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await accountsApi.deleteImpact("token-123", 2);

    expect(result).toEqual({
      calendars: [{ id: "cal-1", name: "Family", shareCount: 2 }],
      affectedUserCount: 2,
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/accounts/2/delete-impact",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws an ApiError when the account doesn't exist", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "account not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(accountsApi.deleteImpact("token-123", 99)).rejects.toMatchObject({
      code: "not_found",
    });
  });
});

describe("accountsApi.delete", () => {
  it("sends the disposition and deletes the account", async () => {
    const fetchMock = vi.fn().mockResolvedValue(noContentResponse());
    vi.stubGlobal("fetch", fetchMock);

    await accountsApi.delete("token-123", 2, "delete");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/accounts/2",
      expect.objectContaining({
        method: "DELETE",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ owned_calendars: "delete" }),
      }),
    );
  });

  it("sends the transfer target when transferring", async () => {
    const fetchMock = vi.fn().mockResolvedValue(noContentResponse());
    vi.stubGlobal("fetch", fetchMock);

    await accountsApi.delete("token-123", 2, "transfer", 3);

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/accounts/2",
      expect.objectContaining({
        method: "DELETE",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ owned_calendars: "transfer", transfer_to: 3 }),
      }),
    );
  });

  it("throws an explanation when deleting the last remaining admin", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(409, {
        error: { code: "last_admin", message: "cannot delete the last remaining admin" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(accountsApi.delete("token-123", 1, "delete")).rejects.toMatchObject({
      code: "last_admin",
      message: "cannot delete the last remaining admin",
    });
  });

  it("throws an explanation when the transfer target doesn't exist", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(400, { error: { code: "invalid_request", message: "transfer target not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(accountsApi.delete("token-123", 2, "transfer", 99)).rejects.toMatchObject({
      code: "invalid_request",
      message: "transfer target not found",
    });
  });
});
