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
          is_pending: false,
          invite_email_available: false,
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
        isPending: false,
        inviteExpiresAt: null,
        inviteEmailAvailable: false,
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
        is_pending: false,
        invite_email_available: false,
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
      isPending: false,
      inviteExpiresAt: null,
      inviteEmailAvailable: false,
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

describe("accountsApi.createInvite", () => {
  it("sends the username and email, and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(201, {
        account: {
          id: 2,
          username: "kid",
          is_admin: false,
          is_disabled: false,
          must_change_password: false,
          created_at: "2026-01-01T00:00:00Z",
          is_pending: true,
          invite_expires_at: "2026-01-08T00:00:00Z",
          invite_email_available: true,
        },
        token: "invite-token-abc",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await accountsApi.createInvite("token-123", "kid", "kid@example.com");

    expect(result).toEqual({
      account: {
        id: 2,
        username: "kid",
        isAdmin: false,
        isDisabled: false,
        mustChangePassword: false,
        createdAt: "2026-01-01T00:00:00Z",
        isPending: true,
        inviteExpiresAt: "2026-01-08T00:00:00Z",
        inviteEmailAvailable: true,
      },
      token: "invite-token-abc",
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/accounts/invite",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ username: "kid", email: "kid@example.com" }),
      }),
    );
  });

  it("throws an explanation on a duplicate username", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(409, { error: { code: "username_taken", message: "username is already taken" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(accountsApi.createInvite("token-123", "admin", "")).rejects.toMatchObject({
      code: "username_taken",
    });
  });
});

describe("accountsApi.reissueInvite", () => {
  it("replaces the invite and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        account: {
          id: 2,
          username: "kid",
          is_admin: false,
          is_disabled: false,
          must_change_password: false,
          created_at: "2026-01-01T00:00:00Z",
          is_pending: true,
          invite_expires_at: "2026-01-15T00:00:00Z",
          invite_email_available: false,
        },
        token: "invite-token-def",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await accountsApi.reissueInvite("token-123", 2);

    expect(result.token).toBe("invite-token-def");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/accounts/2/invite/reissue",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws an explanation when the account has no outstanding invite", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(409, {
        error: { code: "not_pending", message: "account does not have an outstanding invite" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(accountsApi.reissueInvite("token-123", 2)).rejects.toMatchObject({
      code: "not_pending",
    });
  });
});

describe("accountsApi.cancelInvite", () => {
  it("deletes the pending account", async () => {
    const fetchMock = vi.fn().mockResolvedValue(noContentResponse());
    vi.stubGlobal("fetch", fetchMock);

    await accountsApi.cancelInvite("token-123", 2);

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/accounts/2/invite",
      expect.objectContaining({
        method: "DELETE",
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws an explanation when the account has no outstanding invite", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(409, {
        error: { code: "not_pending", message: "account does not have an outstanding invite" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(accountsApi.cancelInvite("token-123", 2)).rejects.toMatchObject({
      code: "not_pending",
    });
  });
});

describe("accountsApi.sendInviteEmail", () => {
  it("sends the link", async () => {
    const fetchMock = vi.fn().mockResolvedValue(noContentResponse());
    vi.stubGlobal("fetch", fetchMock);

    await accountsApi.sendInviteEmail("token-123", 2, "https://example.com/accept-invite?token=abc");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/accounts/2/invite/email",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ link: "https://example.com/accept-invite?token=abc" }),
      }),
    );
  });

  it("throws an explanation when no email is on file", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(409, {
        error: { code: "no_invite_email", message: "no email on file for this invite" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      accountsApi.sendInviteEmail("token-123", 2, "https://example.com/accept-invite?token=abc"),
    ).rejects.toMatchObject({ code: "no_invite_email" });
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
        is_pending: false,
        invite_email_available: false,
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
      isPending: false,
      inviteExpiresAt: null,
      inviteEmailAvailable: false,
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

describe("accountsApi.renameUsername", () => {
  it("sends the username and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 2,
        username: "alicia",
        is_admin: false,
        is_disabled: false,
        must_change_password: false,
        created_at: "2026-01-01T00:00:00Z",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await accountsApi.renameUsername("token-123", 2, "alicia");

    expect(result.username).toBe("alicia");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/accounts/2/username",
      expect.objectContaining({
        method: "PUT",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ username: "alicia" }),
      }),
    );
  });

  it("throws username_taken on a conflict", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(409, { error: { code: "username_taken", message: "username is already taken" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(accountsApi.renameUsername("token-123", 2, "bob")).rejects.toMatchObject({
      code: "username_taken",
    });
  });
});

describe("accountsApi.usernameImpact", () => {
  it("sends the bearer token and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { app_password_count: 3 }));
    vi.stubGlobal("fetch", fetchMock);

    const result = await accountsApi.usernameImpact("token-123", 2);

    expect(result).toEqual({ appPasswordCount: 3 });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/accounts/2/username-impact",
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

    await expect(accountsApi.usernameImpact("token-123", 99)).rejects.toMatchObject({
      code: "not_found",
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
