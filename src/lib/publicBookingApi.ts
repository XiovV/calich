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

export interface BookingConfirmation {
  start: Date;
  end: Date;
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

  // POST /api/public/{handle}/{slug}/book: confirms start into a Busy Event
  // on the host's Book-into Calendar (#326, ADR-0087). visitorName and
  // visitorEmail are the booking form's only two fields, both required. A
  // slot no longer bookable — raced by another visitor, or simply stale —
  // answers 409 slot_taken (isSlotTakenError below); the caller re-fetches
  // slots() and asks the visitor to pick again.
  async book(handle: string, slug: string, start: Date, visitorName: string, visitorEmail: string): Promise<BookingConfirmation> {
    const response = await fetch(`/api/public/${encodeURIComponent(handle)}/${encodeURIComponent(slug)}/book`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ start: start.toISOString(), visitorName, visitorEmail }),
    });
    if (!response.ok) throw await errorFromResponse(response);
    const body = (await response.json()) as { start: string; end: string };
    return { start: new Date(body.start), end: new Date(body.end) };
  },

  // POST /api/public/cancel-booking: the signed cancel link's own
  // destination (#327, ADR-0087). Deletes the booking's Event and frees the
  // slot immediately; idempotent, so following the link twice never errors
  // on the second attempt.
  async cancel(token: string): Promise<void> {
    const response = await fetch(`/api/public/cancel-booking`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token }),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};

export function isNotFoundError(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404;
}

// isSlotTakenError reports whether error is book's own 409 for a slot that
// was just taken — by another visitor racing the same one, or because it
// simply isn't bookable any more (#326, ADR-0087).
export function isSlotTakenError(error: unknown): boolean {
  return error instanceof ApiError && error.code === "slot_taken";
}

// isBookingAlreadyStartedError reports whether error is cancel's own 409
// for a booking whose own start has already passed (#327, ADR-0087) — a
// stale link cannot delete something that already happened.
export function isBookingAlreadyStartedError(error: unknown): boolean {
  return error instanceof ApiError && error.code === "booking_already_started";
}

// isInvalidCancelTokenError reports whether error is cancel's own 400 for a
// token that doesn't parse or verify (#327, ADR-0087) — the only other
// client error cancel() can answer besides isBookingAlreadyStartedError
// above, so a plain status check is unambiguous here.
export function isInvalidCancelTokenError(error: unknown): boolean {
  return error instanceof ApiError && error.status === 400;
}
