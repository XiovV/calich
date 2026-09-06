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
  // importedHere is true when a Linked Calendar for this Provider calendar
  // already exists in the Workspace the picker was opened in (#295) — the
  // re-run's default-checked state, and the row whose uncheck deletes.
  importedHere: boolean;
  // importedElsewhere is true when one exists in a *different* Workspace of
  // this User — the picker shows this Workspace's state, so the row stays
  // unchecked here, with a quiet note explaining why (#295).
  importedElsewhere: boolean;
  // localCalendarId is this app's own Calendar id, set only when
  // importedHere — what an uncheck deletes.
  localCalendarId?: string;
  // shareCount is how many Shares that Linked Calendar carries, set only
  // when importedHere — so the delete confirmation can say so explicitly
  // when other people would lose the Calendar (#295).
  shareCount: number;
}

// DisconnectDisposition is what should happen to a Connection's Linked
// Calendars when it is disconnected (#295): keep them as ordinary owned
// Calendars, or delete them and their Events. No default — deletion is
// unrecoverable.
export type DisconnectDisposition = "keep" | "delete";

// DisconnectImpactCalendar is one Linked Calendar a disconnect would touch —
// enough for the confirmation to name what "delete" costs and warn when
// others hold a Share.
export interface DisconnectImpactCalendar {
  id: string;
  name: string;
  shareCount: number;
}

export interface DisconnectImpact {
  linkedCalendars: DisconnectImpactCalendar[];
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

  // disconnectImpact is what disconnecting would affect (#295): every Linked
  // Calendar the Connection produced and how many Shares each carries, so
  // the confirmation can name the cost before the User picks a disposition.
  async disconnectImpact(accessToken: string, id: number): Promise<DisconnectImpact> {
    const response = await authedFetch(accessToken, `/api/connections/${id}/impact`, {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as DisconnectImpact;
  },

  // disconnect removes the Connection, applying disposition to its Linked
  // Calendars first (#295). The disposition is required — the server refuses
  // a disconnect that doesn't name one, since deletion is unrecoverable.
  async disconnect(
    accessToken: string,
    id: number,
    disposition: DisconnectDisposition,
  ): Promise<void> {
    const response = await authedFetch(
      accessToken,
      `/api/connections/${id}?disposition=${disposition}`,
      {
        method: "DELETE",
        credentials: "include",
      },
    );
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
