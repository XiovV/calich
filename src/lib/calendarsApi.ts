import { authHeader, errorFromResponse } from "./apiClient";
import type { Calendar } from "./calendar";

export interface SubscriptionPreview {
  name: string;
  color: string;
  eventCount: number;
  rangeStart?: string;
  rangeEnd?: string;
}

export interface RefreshResult {
  notModified: boolean;
  created: number;
  updated: number;
  tombstoned: number;
  unparseable: number;
  noOp: number;
}

// Role is what a Share permits (ADR-0034): Viewer or Editor.
export type Role = "viewer" | "editor";

// Share is a Share (ADR-0034): the grant binding one Calendar to one User
// with one Role, as an Owner's "who has Access to my Calendar" listing sees
// it (#113).
export interface Share {
  userId: number;
  username: string;
  role: Role;
  createdAt: string;
}

export const calendarsApi = {
  async list(accessToken: string): Promise<Calendar[]> {
    const response = await fetch("/api/calendars/", {
      credentials: "include",
      headers: authHeader(accessToken),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Calendar[];
  },

  async create(
    accessToken: string,
    calendar: { id: string; name: string; color: string },
  ): Promise<Calendar> {
    const response = await fetch("/api/calendars/", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify(calendar),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Calendar;
  },

  async update(
    accessToken: string,
    id: string,
    changes: {
      name: string;
      color: string;
      keepAlarms?: boolean;
      url?: string;
    },
  ): Promise<Calendar> {
    const response = await fetch(`/api/calendars/${id}`, {
      method: "PATCH",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify(changes),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Calendar;
  },

  async remove(accessToken: string, id: string): Promise<void> {
    const response = await fetch(`/api/calendars/${id}`, {
      method: "DELETE",
      credentials: "include",
      headers: authHeader(accessToken),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  async get(accessToken: string, id: string): Promise<Calendar> {
    const response = await fetch(`/api/calendars/${id}`, {
      credentials: "include",
      headers: authHeader(accessToken),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Calendar;
  },

  // refresh triggers an on-demand Refresh of a Subscribed Calendar (#85):
  // the server always forces it, bypassing the conditional-GET/content-hash
  // short-circuit, so the action is never a visible no-op. The response
  // carries only the refresh summary, not the updated Calendar (its
  // lastSyncedAt) — callers re-fetch that separately (see get above).
  async refresh(accessToken: string, id: string): Promise<RefreshResult> {
    const response = await fetch(`/api/calendars/${id}/refresh`, {
      method: "POST",
      credentials: "include",
      headers: authHeader(accessToken),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as RefreshResult;
  },

  async previewSubscription(
    accessToken: string,
    url: string,
  ): Promise<SubscriptionPreview> {
    const response = await fetch("/api/calendars/subscribe?dryRun=1", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify({ url }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as SubscriptionPreview;
  },

  async subscribe(
    accessToken: string,
    subscription: {
      url: string;
      name: string;
      color: string;
      keepAlarms: boolean;
    },
  ): Promise<Calendar> {
    const response = await fetch("/api/calendars/subscribe?dryRun=0", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify(subscription),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Calendar;
  },

  // listShares returns every Share on id, Owner-only (#113, ADR-0034).
  async listShares(accessToken: string, id: string): Promise<Share[]> {
    const response = await fetch(`/api/calendars/${id}/shares`, {
      credentials: "include",
      headers: authHeader(accessToken),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Share[];
  },

  // share grants id a Share to username with role, or changes an existing
  // Share's role if username already has one (#113, ADR-0034). Owner-only.
  async share(
    accessToken: string,
    id: string,
    username: string,
    role: Role,
  ): Promise<Share> {
    const response = await fetch(`/api/calendars/${id}/shares`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify({ username, role }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Share;
  },

  // revokeShare removes userId's Share on id (#113, ADR-0034). Owner-only.
  async revokeShare(accessToken: string, id: string, userId: number): Promise<void> {
    const response = await fetch(`/api/calendars/${id}/shares/${userId}`, {
      method: "DELETE",
      credentials: "include",
      headers: authHeader(accessToken),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  // leave renounces the caller's own Share on id (#114, ADR-0034) — the
  // non-Owner counterpart to remove, which only an Owner may call.
  async leave(accessToken: string, id: string): Promise<void> {
    const response = await fetch(`/api/calendars/${id}/leave`, {
      method: "POST",
      credentials: "include",
      headers: authHeader(accessToken),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
