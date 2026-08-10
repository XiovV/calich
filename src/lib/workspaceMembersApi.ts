import { authedFetch, errorFromResponse } from "./apiClient";
import { requireActiveWorkspaceId } from "./workspacesStore";
import type { CalendarDisposition, CalendarDispositionChoice, CalendarImpact } from "./accountApi";

// Workspace member management (#156, #160, #165): the Owner/Admin screen
// that lists Members, grants/revokes Admin, and removes a Member — mirroring
// the retired instance-wide Admin account list (#145), scoped to one
// Workspace's membership instead of the whole instance.

export type WorkspaceRole = "owner" | "admin" | "member";

export interface WorkspaceMember {
  userId: number;
  name: string;
  email: string;
  role: WorkspaceRole;
  createdAt: string;
}

interface WorkspaceMemberWire {
  user_id: number;
  name: string;
  email: string;
  role: WorkspaceRole;
  created_at: string;
}

function fromWorkspaceMemberWire(wire: WorkspaceMemberWire): WorkspaceMember {
  return {
    userId: wire.user_id,
    name: wire.name,
    email: wire.email,
    role: wire.role,
    createdAt: wire.created_at,
  };
}

// setRole's response is narrower than WorkspaceMember — the backend doesn't
// join against the User for this call, since the caller already has it from
// their own list() call.
export interface WorkspaceMemberRole {
  userId: number;
  role: WorkspaceRole;
  createdAt: string;
}

interface WorkspaceMemberRoleWire {
  user_id: number;
  role: WorkspaceRole;
  created_at: string;
}

function fromWorkspaceMemberRoleWire(wire: WorkspaceMemberRoleWire): WorkspaceMemberRole {
  return { userId: wire.user_id, role: wire.role, createdAt: wire.created_at };
}

export interface RemoveMemberImpact {
  calendars: CalendarImpact[];
}

interface TransferCandidateWire {
  id: number;
  name: string;
}

interface CalendarImpactWire {
  id: string;
  name: string;
  workspace_id: number;
  workspace_name: string;
  share_count: number;
  transfer_candidates: TransferCandidateWire[];
}

interface RemoveMemberImpactWire {
  calendars: CalendarImpactWire[];
}

function fromRemoveMemberImpactWire(wire: RemoveMemberImpactWire): RemoveMemberImpact {
  return {
    calendars: wire.calendars.map((c) => ({
      id: c.id,
      name: c.name,
      workspaceId: c.workspace_id,
      workspaceName: c.workspace_name,
      shareCount: c.share_count,
      transferCandidates: c.transfer_candidates.map((t) => ({ id: t.id, name: t.name })),
    })),
  };
}

export const workspaceMembersApi = {
  // Scoped to the currently active Workspace (#153, #165) — switching
  // Workspaces changes which Workspace's membership every call below reads
  // or writes.
  async list(accessToken: string): Promise<WorkspaceMember[]> {
    const response = await authedFetch(accessToken, `/api/workspaces/${requireActiveWorkspaceId()}/members`, {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as WorkspaceMemberWire[];
    return body.map(fromWorkspaceMemberWire);
  },

  // Grants or revokes the Admin Role on userId (#156) — Owner-only.
  async setRole(accessToken: string, userId: number, role: "admin" | "member"): Promise<WorkspaceMemberRole> {
    const response = await authedFetch(
      accessToken,
      `/api/workspaces/${requireActiveWorkspaceId()}/members/${userId}/role`,
      {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ role }),
      },
    );
    if (!response.ok) throw await errorFromResponse(response);

    return fromWorkspaceMemberRoleWire((await response.json()) as WorkspaceMemberRoleWire);
  },

  // Reports which Calendars userId owns within the active Workspace before a
  // removal (#160) — the preview a removal-confirmation UI shows.
  async removeImpact(accessToken: string, userId: number): Promise<RemoveMemberImpact> {
    const response = await authedFetch(
      accessToken,
      `/api/workspaces/${requireActiveWorkspaceId()}/members/${userId}/remove-impact`,
      { credentials: "include" },
    );
    if (!response.ok) throw await errorFromResponse(response);

    return fromRemoveMemberImpactWire((await response.json()) as RemoveMemberImpactWire);
  },

  // Ends userId's Membership in the active Workspace (#156, #160), requiring
  // an explicit transfer-or-delete disposition for every Calendar they own
  // within it.
  async remove(accessToken: string, userId: number, calendars: CalendarDispositionChoice[]): Promise<void> {
    const response = await authedFetch(
      accessToken,
      `/api/workspaces/${requireActiveWorkspaceId()}/members/${userId}`,
      {
        method: "DELETE",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          calendars: calendars.map((c) => ({
            calendar_id: c.calendarId,
            disposition: c.disposition,
            ...(c.transferTo !== undefined ? { transfer_to: c.transferTo } : {}),
          })),
        }),
      },
    );
    if (!response.ok) throw await errorFromResponse(response);
  },
};

export type { CalendarDisposition, CalendarDispositionChoice, CalendarImpact };
