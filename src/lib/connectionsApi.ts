import { authedFetch, errorFromResponse } from "./apiClient";

// A Connection (#285, ADR-0052): one User's authorized grant to one account
// at one Provider — Google is the only one this app speaks to. Status is
// whether the grant is currently usable; only "live" is reachable yet
// ("expired"/"revoked" are a later ticket's, once Refresh can detect them).
export type ConnectionStatus = "live" | "expired" | "revoked";

export interface Connection {
  id: number;
  provider: string;
  accountEmail: string;
  status: ConnectionStatus;
  createdAt: string;
}

interface ConnectionWire {
  id: number;
  provider: string;
  account_email: string;
  status: ConnectionStatus;
  created_at: string;
}

function fromWire(wire: ConnectionWire): Connection {
  return {
    id: wire.id,
    provider: wire.provider,
    accountEmail: wire.account_email,
    status: wire.status,
    createdAt: wire.created_at,
  };
}

export const connectionsApi = {
  async list(accessToken: string): Promise<Connection[]> {
    const response = await authedFetch(accessToken, "/api/connections/", {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as ConnectionWire[];
    return body.map(fromWire);
  },

  // Returns the URL to send the browser to, to consent to Google's OAuth
  // grant — the caller navigates there itself (window.location), since
  // connecting is a full-page round trip through Google and back, not
  // something this fetch can complete on its own.
  async connectGoogle(accessToken: string): Promise<string> {
    const response = await authedFetch(accessToken, "/api/connections/google/connect", {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as { url: string };
    return body.url;
  },

  async disconnect(accessToken: string, id: number): Promise<void> {
    const response = await authedFetch(accessToken, `/api/connections/${id}`, {
      method: "DELETE",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
