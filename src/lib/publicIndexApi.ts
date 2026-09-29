import { errorFromResponse } from "./apiClient";

// The public index page's own API surface (#325, ADR-0084): no Session, no
// Access token, no Workspace header — a plain fetch, the same posture
// publicBookingApi takes toward /:handle/:slug.

export interface PublicIndexLink {
  slug: string;
  title: string;
  durationMinutes: number;
}

export interface PublicIndex {
  // Empty for an unknown Handle, a reserved-word Handle, and a Handle whose
  // links are all Private, Paused, or otherwise unreachable (ADR-0087) —
  // deliberately indistinguishable from one another (ADR-0084), so this page
  // never renders a name unless links is non-empty.
  hostName: string;
  links: PublicIndexLink[];
}

export const publicIndexApi = {
  // GET /api/public/{handle}: the owner's Name and every Public Booking
  // Link they hold, unioned across every Workspace they belong to. Always
  // 200 — there is no not-found case here, only an index with no links.
  async get(handle: string): Promise<PublicIndex> {
    const response = await fetch(`/api/public/${encodeURIComponent(handle)}`);
    if (!response.ok) throw await errorFromResponse(response);
    return (await response.json()) as PublicIndex;
  },
};
