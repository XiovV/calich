import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, authApi } from "./authApi";

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

describe("authApi.login", () => {
  it("returns the access token and must_change_password flag on success", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { access_token: "token-123", must_change_password: true }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await authApi.login("admin", "admin");

    expect(result).toEqual({ accessToken: "token-123", mustChangePassword: true });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/login",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({ username: "admin", password: "admin" }),
      }),
    );
  });

  it("throws an ApiError with the backend's code and message on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(401, { error: { code: "invalid_credentials", message: "invalid username or password" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.login("admin", "wrong")).rejects.toMatchObject({
      code: "invalid_credentials",
      message: "invalid username or password",
      status: 401,
    });
  });

  it("falls back to a generic error when the body isn't valid JSON", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response("not json", { status: 500 }));
    vi.stubGlobal("fetch", fetchMock);

    const error = await authApi.login("admin", "admin").catch((e: unknown) => e);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).status).toBe(500);
  });
});

describe("authApi.refresh", () => {
  it("sends no body and includes credentials", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { access_token: "new-token" }));
    vi.stubGlobal("fetch", fetchMock);

    const result = await authApi.refresh();

    expect(result).toEqual({ accessToken: "new-token" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/refresh",
      expect.objectContaining({ method: "POST", credentials: "include" }),
    );
  });

  it("throws on failure", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(401, { error: { code: "unauthorized", message: "missing refresh token" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.refresh()).rejects.toMatchObject({ code: "unauthorized" });
  });
});

describe("authApi.me", () => {
  it("sends the access token as a bearer header and maps the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { id: 1, username: "admin", must_change_password: false }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const user = await authApi.me("token-123");

    expect(user).toEqual({ id: 1, username: "admin", mustChangePassword: false });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/me",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws an ApiError with code password_change_required when blocked", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(403, { error: { code: "password_change_required", message: "password must be changed" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.me("token-123")).rejects.toMatchObject({
      code: "password_change_required",
      status: 403,
    });
  });
});

describe("authApi.logout", () => {
  it("resolves without a body on 204", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.logout()).resolves.toBeUndefined();
  });
});

describe("authApi.changePassword", () => {
  it("sends both passwords and the bearer token", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await authApi.changePassword("token-123", "old-pw", "new-pw");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/auth/change-password",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ current_password: "old-pw", new_password: "new-pw" }),
      }),
    );
  });

  it("throws on an invalid current password", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(401, { error: { code: "invalid_credentials", message: "current password is incorrect" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(authApi.changePassword("token-123", "wrong", "new-pw")).rejects.toMatchObject({
      code: "invalid_credentials",
    });
  });
});
