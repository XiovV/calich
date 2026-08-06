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
    changes: { name: string; color: string; keepAlarms?: boolean },
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
};
