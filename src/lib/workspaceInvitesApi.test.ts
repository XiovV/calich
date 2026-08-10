import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { workspaceInvitesApi } from "./workspaceInvitesApi";
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

describe("workspaceInvitesApi.create", () => {
  it("sends the email and returns the invite with its token", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(201, {
        id: 1,
        workspace_id: 7,
        email: "bob@example.com",
        invite_expires_at: "2026-01-08T00:00:00Z",
        token: "the-plaintext-token",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await workspaceInvitesApi.create("token-123", "bob@example.com");

    expect(result).toEqual({
      id: 1,
      workspaceId: 7,
      email: "bob@example.com",
      inviteExpiresAt: "2026-01-08T00:00:00Z",
      token: "the-plaintext-token",
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/workspaces/7/invites",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({ email: "bob@example.com" }),
      }),
    );
  });

  it("throws when an invite already exists for that email", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(409, {
        error: { code: "invite_exists", message: "workspace already has an outstanding invite for this email" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(workspaceInvitesApi.create("token-123", "bob@example.com")).rejects.toMatchObject({
      code: "invite_exists",
    });
  });
});

describe("workspaceInvitesApi.list", () => {
  it("maps outstanding invites without a token", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, [
        { id: 1, workspace_id: 7, email: "bob@example.com", invite_expires_at: "2026-01-08T00:00:00Z" },
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await workspaceInvitesApi.list("token-123");

    expect(result).toEqual([
      { id: 1, workspaceId: 7, email: "bob@example.com", inviteExpiresAt: "2026-01-08T00:00:00Z" },
    ]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/workspaces/7/invites",
      expect.objectContaining({ credentials: "include" }),
    );
  });
});

describe("workspaceInvitesApi.reissue", () => {
  it("returns the reissued invite with a fresh token", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        id: 1,
        workspace_id: 7,
        email: "bob@example.com",
        invite_expires_at: "2026-01-15T00:00:00Z",
        token: "a-new-token",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await workspaceInvitesApi.reissue("token-123", 1);

    expect(result.token).toBe("a-new-token");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/workspaces/invites/1/reissue",
      expect.objectContaining({ method: "POST", credentials: "include" }),
    );
  });
});

describe("workspaceInvitesApi.cancel", () => {
  it("sends a DELETE with the bearer token", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await workspaceInvitesApi.cancel("token-123", 1);

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/workspaces/invites/1",
      expect.objectContaining({ method: "DELETE", credentials: "include" }),
    );
  });

  it("throws when the invite doesn't exist", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "invite not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(workspaceInvitesApi.cancel("token-123", 999)).rejects.toMatchObject({ code: "not_found" });
  });
});
