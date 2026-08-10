import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { workspaceMembersApi } from "./workspaceMembersApi";
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

describe("workspaceMembersApi.list", () => {
  it("sends the bearer token and maps the response, scoped to the active workspace", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, [
        { user_id: 1, username: "alice", role: "owner", created_at: "2026-01-01T00:00:00Z" },
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await workspaceMembersApi.list("token-123");

    expect(result).toEqual([
      { userId: 1, username: "alice", role: "owner", createdAt: "2026-01-01T00:00:00Z" },
    ]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/workspaces/7/members",
      expect.objectContaining({
        credentials: "include",
        headers: { Authorization: "Bearer token-123" },
      }),
    );
  });

  it("throws an ApiError when the caller isn't a member", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(404, { error: { code: "not_found", message: "workspace not found" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(workspaceMembersApi.list("token-123")).rejects.toMatchObject({ code: "not_found" });
  });
});

describe("workspaceMembersApi.setRole", () => {
  it("sends the role and returns the updated member", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { user_id: 2, role: "admin", created_at: "2026-01-01T00:00:00Z" }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await workspaceMembersApi.setRole("token-123", 2, "admin");

    expect(result.role).toBe("admin");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/workspaces/7/members/2/role",
      expect.objectContaining({
        method: "PUT",
        credentials: "include",
        headers: expect.objectContaining({ Authorization: "Bearer token-123" }),
        body: JSON.stringify({ role: "admin" }),
      }),
    );
  });
});

describe("workspaceMembersApi.removeImpact", () => {
  it("maps calendars and transfer candidates", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        calendars: [
          {
            id: "cal-1",
            name: "Team",
            workspace_id: 7,
            workspace_name: "Acme",
            share_count: 2,
            transfer_candidates: [{ id: 3, username: "carol" }],
          },
        ],
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await workspaceMembersApi.removeImpact("token-123", 2);

    expect(result).toEqual({
      calendars: [
        {
          id: "cal-1",
          name: "Team",
          workspaceId: 7,
          workspaceName: "Acme",
          shareCount: 2,
          transferCandidates: [{ id: 3, username: "carol" }],
        },
      ],
    });
  });
});

describe("workspaceMembersApi.remove", () => {
  it("sends dispositions and issues a DELETE", async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204));
    vi.stubGlobal("fetch", fetchMock);

    await workspaceMembersApi.remove("token-123", 2, [
      { calendarId: "cal-1", disposition: "transfer", transferTo: 3 },
    ]);

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/workspaces/7/members/2",
      expect.objectContaining({
        method: "DELETE",
        credentials: "include",
        body: JSON.stringify({
          calendars: [{ calendar_id: "cal-1", disposition: "transfer", transfer_to: 3 }],
        }),
      }),
    );
  });

  it("throws when the actor is an admin removing another admin", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(403, { error: { code: "forbidden", message: "only the workspace owner can remove an admin" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(workspaceMembersApi.remove("token-123", 2, [])).rejects.toMatchObject({
      code: "forbidden",
    });
  });
});
