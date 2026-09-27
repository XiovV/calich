import { ApiError, errorFromResponse } from "./apiClient";

// The public Booking Link page's own API surface (#324, ADR-0084,
// ADR-0087): no Session, no Access token, no Workspace header — every call
// here goes straight through fetch, unlike every other *Api module, which
// routes through authedFetch.

export interface PublicBookingLink {
  hostName: string;
  hostTimezone: string;
  title: string;
  durationMinutes: number;
  location: string;
  description: string;
  bookingHorizonDays: number;
  // Paused collapses "explicitly Paused", "no SMTP configured on this
  // instance" and "the host lost Access to the Book-into Calendar" into one
  // answer (ADR-0087) — the page has one message for all three, and no way
  // to tell them apart.
  paused: boolean;
}

export const publicBookingApi = {
  // GET /api/public/{handle}/{slug}: host and offer details, regardless of
  // whether the link is Public or Private (ADR-0084's "a Private link is
  // reachable by its direct URL") — a 404 here (ApiError with status 404)
  // means an unknown Handle, an unknown Slug or a reserved-word Handle,
  // deliberately indistinguishable from one another.
  async get(handle: string, slug: string): Promise<PublicBookingLink> {
    const response = await fetch(`/api/public/${encodeURIComponent(handle)}/${encodeURIComponent(slug)}`);
    if (!response.ok) throw await errorFromResponse(response);
    return (await response.json()) as PublicBookingLink;
  },

  // GET /api/public/{handle}/{slug}/slots?year=&month=: every bookable slot
  // start within the given calendar month (month is 1-12), derived
  // server-side (#323) so this page never re-derives availability itself. A
  // Paused link answers an empty list rather than an error.
  async slots(handle: string, slug: string, year: number, month: number): Promise<Date[]> {
    const response = await fetch(
      `/api/public/${encodeURIComponent(handle)}/${encodeURIComponent(slug)}/slots?year=${year}&month=${month}`,
    );
    if (!response.ok) throw await errorFromResponse(response);
    const body = (await response.json()) as { slots: string[] };
    return body.slots.map((slot) => new Date(slot));
  },
};

export function isNotFoundError(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404;
}
