import { authedFetch, errorFromResponse } from "./apiClient";
import { workspaceHeaders } from "./workspaceHeaders";

// Calendar Sets (#301, #302, ADR-0082): a named, private selection of the
// active Workspace's Calendars belonging to one User. Scoped to the
// currently active Workspace via workspaceHeaders, the same as every other
// Workspace-scoped call.

export interface CalendarSet {
  id: number;
  name: string;
  // calendarIds is this Set's membership, embedded on every List response
  // (ADR-0082's REST surface) rather than a separate members endpoint —
  // the membership dialog needs it the moment it has the Set itself.
  calendarIds: string[];
}

export const calendarSetsApi = {
  // Every Calendar Set the caller owns in the active Workspace.
  async list(accessToken: string): Promise<CalendarSet[]> {
    const response = await authedFetch(accessToken, "/api/calendar-sets/", {
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as CalendarSet[];
  },

  // Creates a new Calendar Set named name in the active Workspace.
  // Membership is added separately.
  async create(accessToken: string, name: string): Promise<CalendarSet> {
    const response = await authedFetch(accessToken, "/api/calendar-sets/", {
      method: "POST",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ name }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as CalendarSet;
  },

  // Renames id.
  async rename(accessToken: string, id: number, name: string): Promise<CalendarSet> {
    const response = await authedFetch(accessToken, `/api/calendar-sets/${id}`, {
      method: "PATCH",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ name }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as CalendarSet;
  },

  // Deletes id outright. Destroys no Calendar and no Event.
  async remove(accessToken: string, id: number): Promise<void> {
    const response = await authedFetch(accessToken, `/api/calendar-sets/${id}`, {
      method: "DELETE",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  // Puts calendarId into id's membership (#302). PUT, not POST: re-adding an
  // already-member Calendar is a no-op, not a conflict.
  async addCalendar(accessToken: string, id: number, calendarId: string): Promise<void> {
    const response = await authedFetch(accessToken, `/api/calendar-sets/${id}/calendars/${calendarId}`, {
      method: "PUT",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  // Takes calendarId out of id's membership (#302).
  async removeCalendar(accessToken: string, id: number, calendarId: string): Promise<void> {
    const response = await authedFetch(accessToken, `/api/calendar-sets/${id}/calendars/${calendarId}`, {
      method: "DELETE",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
