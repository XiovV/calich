import { authedFetch, errorFromResponse } from "./apiClient";
import type { Calendar } from "./calendar";
import { workspaceHeaders } from "./workspaceHeaders";

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

// PickerCalendar is one row the Calendar picker offers (#286): everything a
// Connection's account can see at Google — its own calendars, the ones it
// subscribed to, and the ones other people shared to it.
export interface PickerCalendar {
  id: string;
  name: string;
  color: string;
  // selected mirrors Google's own sidebar checkbox — the picker's default
  // checked state.
  selected: boolean;
  // writable reports whether Google's own ACL lets the account write to
  // this calendar there — rendered as a read-only badge when false. Not a
  // claim about what this app itself will let a User do: every Linked
  // Calendar is read-only here until write-back ships.
  writable: boolean;
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

  // listPickerCalendars is the Calendar picker's read side (#286):
  // everything Connection id's account can see at Google. Workspace-scoped
  // the same way calendarsApi.subscribe is, even though the picker itself
  // doesn't read the active Workspace's calendars — it's what
  // importCalendars below places the picked ones into, and the server's
  // RequireWorkspace gate applies to this route too.
  async listPickerCalendars(accessToken: string, id: number): Promise<PickerCalendar[]> {
    const response = await authedFetch(accessToken, `/api/connections/${id}/calendars`, {
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as PickerCalendar[];
  },

  // importCalendars is the Calendar picker's write side (#286): confirming
  // creates a Linked Calendar in the active Workspace for each of
  // calendarIds.
  async importCalendars(
    accessToken: string,
    id: number,
    calendarIds: string[],
  ): Promise<Calendar[]> {
    const response = await authedFetch(accessToken, `/api/connections/${id}/calendars`, {
      method: "POST",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ calendarIds }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Calendar[];
  },
};
