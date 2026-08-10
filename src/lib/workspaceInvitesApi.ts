import { authedFetch, errorFromResponse } from "./apiClient";
import { requireActiveWorkspaceId } from "./workspacesStore";

// Workspace Invites (#154, #165): issuing, listing, reissuing, and canceling
// an outstanding invite to join a Workspace.

export interface OutstandingWorkspaceInvite {
  id: number;
  workspaceId: number;
  email: string;
  inviteExpiresAt: string;
}

interface OutstandingWorkspaceInviteWire {
  id: number;
  workspace_id: number;
  email: string;
  invite_expires_at: string;
}

function fromOutstandingWire(wire: OutstandingWorkspaceInviteWire): OutstandingWorkspaceInvite {
  return {
    id: wire.id,
    workspaceId: wire.workspace_id,
    email: wire.email,
    inviteExpiresAt: wire.invite_expires_at,
  };
}

// CreatedWorkspaceInvite is CreateInvite's/ReissueInvite's response: the
// resulting Invite alongside the plaintext token, which is never retrievable
// again once shown (ADR-0044).
export interface CreatedWorkspaceInvite extends OutstandingWorkspaceInvite {
  token: string;
}

interface CreatedWorkspaceInviteWire extends OutstandingWorkspaceInviteWire {
  token: string;
}

function fromCreatedWire(wire: CreatedWorkspaceInviteWire): CreatedWorkspaceInvite {
  return { ...fromOutstandingWire(wire), token: wire.token };
}

export const workspaceInvitesApi = {
  // Scoped to the currently active Workspace (#153, #165).
  async create(accessToken: string, email: string): Promise<CreatedWorkspaceInvite> {
    const response = await authedFetch(accessToken, `/api/workspaces/${requireActiveWorkspaceId()}/invites`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromCreatedWire((await response.json()) as CreatedWorkspaceInviteWire);
  },

  async list(accessToken: string): Promise<OutstandingWorkspaceInvite[]> {
    const response = await authedFetch(accessToken, `/api/workspaces/${requireActiveWorkspaceId()}/invites`, {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as OutstandingWorkspaceInviteWire[];
    return body.map(fromOutstandingWire);
  },

  // Overwrites inviteId's outstanding Invite with a fresh token and a reset
  // 7-day expiry, invalidating whichever token came before it.
  async reissue(accessToken: string, inviteId: number): Promise<CreatedWorkspaceInvite> {
    const response = await authedFetch(accessToken, `/api/workspaces/invites/${inviteId}/reissue`, {
      method: "POST",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromCreatedWire((await response.json()) as CreatedWorkspaceInviteWire);
  },

  async cancel(accessToken: string, inviteId: number): Promise<void> {
    const response = await authedFetch(accessToken, `/api/workspaces/invites/${inviteId}`, {
      method: "DELETE",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
