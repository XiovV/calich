import { authedFetch, errorFromResponse } from "./apiClient";

// Workspace is the account-management and billing boundary above User
// (ADR-0044): a named container the caller belongs to. The switcher's data
// source — list() returns only the Workspaces the caller is a Member of.
export interface Workspace {
  id: number;
  name: string;
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
