import { afterEach, describe, expect, it, vi } from "vitest";
import { usersApi } from "./usersApi";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("usersApi.directory", () => {
  it("sends the bearer token and returns the directory", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, [{ id: 2, username: "bob" }]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const users = await usersApi.directory("token-123");

    expect(users).toEqual([{ id: 2, username: "bob" }]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/users/",
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

    await expect(usersApi.directory("token-123")).rejects.toMatchObject({
      code: "unauthorized",
      status: 401,
    });
  });
});
