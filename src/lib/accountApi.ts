import { authedFetch, errorFromResponse } from "./apiClient";

// Self-service account lifecycle (ADR-0044): a User disabling/re-activating
// or deleting their own account, and nobody else's — the instance-wide
// Admin this replaces (ADR-0037) is retired entirely.

export type CalendarDisposition = "transfer" | "delete";

export interface TransferCandidate {
  id: number;
  username: string;
}

// One Calendar the caller owns, across every Workspace they belong to —
// what deleteImpact reports before a transfer-or-delete choice is made for
// each one.
export interface CalendarImpact {
  id: string;
  name: string;
  workspaceId: number;
  workspaceName: string;
  shareCount: number;
  transferCandidates: TransferCandidate[];
}

export interface DeleteImpact {
  calendars: CalendarImpact[];
}

interface TransferCandidateWire {
  id: number;
  username: string;
}

interface CalendarImpactWire {
  id: string;
  name: string;
  workspace_id: number;
  workspace_name: string;
  share_count: number;
  transfer_candidates: TransferCandidateWire[];
}

interface DeleteImpactWire {
  calendars: CalendarImpactWire[];
}

function fromDeleteImpactWire(wire: DeleteImpactWire): DeleteImpact {
  return {
    calendars: wire.calendars.map((c) => ({
      id: c.id,
      name: c.name,
      workspaceId: c.workspace_id,
      workspaceName: c.workspace_name,
      shareCount: c.share_count,
      transferCandidates: c.transfer_candidates.map((t) => ({ id: t.id, username: t.username })),
    })),
  };
}

// A single Calendar's transfer-or-delete choice for delete() — self-Delete
// requires one per Calendar the caller owns.
export interface CalendarDispositionChoice {
  calendarId: string;
  disposition: CalendarDisposition;
  transferTo?: number;
}

export const accountApi = {
  // Disables or re-activates the caller's own account (ADR-0044). Refused
  // with a "sole_workspace_owner" ApiError while the caller is the sole
  // Owner of a Workspace that still has other Members in it.
  async setDisabled(accessToken: string, isDisabled: boolean): Promise<boolean> {
    const response = await authedFetch(accessToken, "/api/account/disabled", {
      method: "PUT",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ is_disabled: isDisabled }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as { is_disabled: boolean };
    return body.is_disabled;
  },

  // Reports every Calendar the caller owns, across every Workspace they
  // belong to, before they choose a disposition for each one.
  async deleteImpact(accessToken: string): Promise<DeleteImpact> {
    const response = await authedFetch(accessToken, "/api/account/delete-impact", {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromDeleteImpactWire((await response.json()) as DeleteImpactWire);
  },

  // Deletes the caller's own account outright, requiring an explicit
  // transfer-or-delete disposition for every Calendar they own.
  async delete(accessToken: string, calendars: CalendarDispositionChoice[]): Promise<void> {
    const response = await authedFetch(accessToken, "/api/account/", {
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
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
