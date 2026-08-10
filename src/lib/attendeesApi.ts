import { authedFetch, errorFromResponse } from "./apiClient";

// Attendee invites (#161, #162, ADR-0046): an Event organizer/Editor can
// invite a User or a Group (expanded to individual rows at invite time) as
// Attendees, independent of Calendar Access — the invite itself grants
// visibility to that one Event. An Attendee sets their own Response;
// nobody else may set it for them.

export type AttendeeResponse = "needs-action" | "accepted" | "declined" | "tentative";

// The wire and domain shapes are identical here (no date/enum coercion
// needed), so — like groupsApi.ts's Group — there's no separate wire type
// or mapper.
export interface Attendee {
  userId: number;
  // Absent only in AddGroupAttendee's response, which the backend doesn't
  // join against the User for (mirrors WorkspaceMemberRole's own narrower
  // response shape).
  name?: string;
  response: AttendeeResponse;
}

export const attendeesApi = {
  // Every Attendee of eventId, open to anyone who can see eventId at all —
  // existing Calendar Access, or being an Attendee themselves.
  async list(accessToken: string, eventId: string): Promise<Attendee[]> {
    const response = await authedFetch(accessToken, `/api/events/${eventId}/attendees`, {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Attendee[];
  },

  // Invites userId as an Attendee of eventId — Editor (or Owner) of
  // eventId's Calendar only.
  async add(accessToken: string, eventId: string, userId: number): Promise<Attendee> {
    const response = await authedFetch(accessToken, `/api/events/${eventId}/attendees`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ userId }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Attendee;
  },

  // Invites every current member of groupId as an individual Attendee of
  // eventId — a one-time snapshot expansion, not a dynamic Group Share
  // (#162, ADR-0046). Same caller requirement as add.
  async addGroup(accessToken: string, eventId: string, groupId: number): Promise<Attendee[]> {
    const response = await authedFetch(accessToken, `/api/events/${eventId}/attendees/group`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ groupId }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Attendee[];
  },

  // Revokes userId's Attendee invite to eventId — same caller requirement
  // as add.
  async remove(accessToken: string, eventId: string, userId: number): Promise<void> {
    const response = await authedFetch(accessToken, `/api/events/${eventId}/attendees/${userId}`, {
      method: "DELETE",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  // Sets the caller's own Response to eventId — there is no way to target a
  // different User, so an organizer/Editor can never set someone else's
  // Response through this.
  async setResponse(
    accessToken: string,
    eventId: string,
    response: AttendeeResponse,
  ): Promise<Attendee> {
    const res = await authedFetch(accessToken, `/api/events/${eventId}/attendees/response`, {
      method: "PUT",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ response }),
    });
    if (!res.ok) throw await errorFromResponse(res);

    return (await res.json()) as Attendee;
  },
};
