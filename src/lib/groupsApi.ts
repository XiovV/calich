import { authedFetch, errorFromResponse } from "./apiClient";
import { workspaceHeaders } from "./workspaceHeaders";

// Groups management (#167, ADR-0045): the Owner/Admin screen that creates,
// renames, and deletes Groups, and adds/removes Workspace Members from them.
// Scoped to the currently active Workspace via workspaceHeaders, the same
// as every other Workspace-scoped call.

export interface Group {
  id: number;
  name: string;
}

export interface GroupMember {
  userId: number;
}

interface GroupMemberWire {
  userId: number;
}

export const groupsApi = {
  // Every Group of the active Workspace, open to any Member of it.
  async list(accessToken: string): Promise<Group[]> {
    const response = await authedFetch(accessToken, "/api/groups/", {
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Group[];
  },

  // Creates a new Group named name in the active Workspace — Owner/Admin
  // only.
  async create(accessToken: string, name: string): Promise<Group> {
    const response = await authedFetch(accessToken, "/api/groups/", {
      method: "POST",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ name }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Group;
  },

  // Renames id — Owner/Admin only.
  async rename(accessToken: string, id: number, name: string): Promise<Group> {
    const response = await authedFetch(accessToken, `/api/groups/${id}`, {
      method: "PATCH",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ name }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Group;
  },

  // Deletes id outright — Owner/Admin only.
  async remove(accessToken: string, id: number): Promise<void> {
    const response = await authedFetch(accessToken, `/api/groups/${id}`, {
      method: "DELETE",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  // Every GroupMember of groupId, open to any Member of its Workspace.
  async listMembers(accessToken: string, groupId: number): Promise<GroupMember[]> {
    const response = await authedFetch(accessToken, `/api/groups/${groupId}/members`, {
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as GroupMemberWire[];
    return body.map((m) => ({ userId: m.userId }));
  },

  // Adds userId to groupId — userId must already be a Member of the same
  // Workspace. Owner/Admin only.
  async addMember(accessToken: string, groupId: number, userId: number): Promise<void> {
    const response = await authedFetch(accessToken, `/api/groups/${groupId}/members`, {
      method: "POST",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ userId }),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  // Removes userId from groupId — Owner/Admin only.
  async removeMember(accessToken: string, groupId: number, userId: number): Promise<void> {
    const response = await authedFetch(accessToken, `/api/groups/${groupId}/members/${userId}`, {
      method: "DELETE",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
