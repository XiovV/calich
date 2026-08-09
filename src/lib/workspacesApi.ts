import { authedFetch, errorFromResponse } from "./apiClient";

// DefaultSharePrivacy is a Workspace's configured default for a new Share's
// Role (#159, ADR-0045): "private" defaults to Viewer, "workspace" defaults
// to Editor — the share dialog's starting selection, not an enforced limit.
export type DefaultSharePrivacy = "private" | "workspace";

// Workspace is the account-management and billing boundary above User
// (ADR-0044): a named container the caller belongs to. The switcher's data
// source — list() returns only the Workspaces the caller is a Member of.
export interface Workspace {
  id: number;
  name: string;
  defaultSharePrivacy: DefaultSharePrivacy;
}

export const workspacesApi = {
  async list(accessToken: string): Promise<Workspace[]> {
    const response = await authedFetch(accessToken, "/api/workspaces/", {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Workspace[];
  },
};
