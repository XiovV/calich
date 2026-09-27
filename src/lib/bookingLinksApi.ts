import { authedFetch, errorFromResponse } from "./apiClient";
import { workspaceHeaders } from "./workspaceHeaders";

// Booking Links (#322, ADR-0084, ADR-0087): a named, slug-addressed offer to
// be booked, scoped to the active Workspace like a Task List.

export type BookingLinkVisibility = "public" | "private" | "paused";

export interface BookingLink {
  id: number;
  title: string;
  slug: string;
  durationMinutes: number;
  visibility: BookingLinkVisibility;
  availabilityScheduleId: number;
  bookIntoCalendarId: string;
  location: string;
  description: string;
  minimumNoticeMinutes: number;
  bookingHorizonDays: number;
  tasksInConflictSet: boolean;
  conflictCalendarIds: string[];
}

// BookingLinkWrite is Create and Update's shared body shape — Update, like
// Create, always takes every field together.
export interface BookingLinkWrite {
  title: string;
  slug: string;
  durationMinutes: number;
  visibility: BookingLinkVisibility;
  availabilityScheduleId: number;
  bookIntoCalendarId: string;
  location: string;
  description: string;
  minimumNoticeMinutes: number;
  bookingHorizonDays: number;
  tasksInConflictSet: boolean;
}

export const bookingLinksApi = {
  // Every Booking Link the caller owns in the active Workspace.
  async list(accessToken: string): Promise<BookingLink[]> {
    const response = await authedFetch(accessToken, "/api/booking-links/", {
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as BookingLink[];
  },

  // Creates a new Booking Link. Its Conflict set is seeded automatically —
  // every Calendar the caller owns in the active Workspace, plus the
  // Book-into Calendar itself, pinned.
  async create(accessToken: string, write: BookingLinkWrite): Promise<BookingLink> {
    const response = await authedFetch(accessToken, "/api/booking-links/", {
      method: "POST",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify(write),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as BookingLink;
  },

  // Replaces id's fields wholesale.
  async update(accessToken: string, id: number, write: BookingLinkWrite): Promise<BookingLink> {
    const response = await authedFetch(accessToken, `/api/booking-links/${id}`, {
      method: "PATCH",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify(write),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as BookingLink;
  },

  // Deletes id outright.
  async remove(accessToken: string, id: number): Promise<void> {
    const response = await authedFetch(accessToken, `/api/booking-links/${id}`, {
      method: "DELETE",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  // Clones id into a new Booking Link with a fresh Slug and Title, copying
  // its Conflict set verbatim.
  async duplicate(accessToken: string, id: number): Promise<BookingLink> {
    const response = await authedFetch(accessToken, `/api/booking-links/${id}/duplicate`, {
      method: "POST",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as BookingLink;
  },

  // Puts calendarId into id's Conflict set (ADR-0087). PUT, not POST —
  // re-adding a Calendar already in the set is a no-op, not a conflict.
  async addConflictCalendar(accessToken: string, id: number, calendarId: string): Promise<void> {
    const response = await authedFetch(accessToken, `/api/booking-links/${id}/conflict-set/calendars/${calendarId}`, {
      method: "PUT",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  // Takes calendarId out of id's Conflict set. Refused by the backend for
  // the link's own Book-into Calendar — permanently in the set.
  async removeConflictCalendar(accessToken: string, id: number, calendarId: string): Promise<void> {
    const response = await authedFetch(accessToken, `/api/booking-links/${id}/conflict-set/calendars/${calendarId}`, {
      method: "DELETE",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
