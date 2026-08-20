import { authedFetch, errorFromResponse } from "./apiClient";
import type { Calendar } from "./calendar";
import type { Reminder } from "./event";
import { workspaceHeaders } from "./workspaceHeaders";

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
  // Name is a display-only label (ADR-0047); Email is the identifier used
  // to grant a Share.
  name: string;
  email: string;
  role: Role;
  createdAt: string;
}

// GroupShare is a Group-targeted Share (#159, ADR-0045): the grant binding
// one Calendar to one Group with one Role, mirroring Share.
export interface GroupShare {
  groupId: number;
  groupName: string;
  role: Role;
  createdAt: string;
}

// ShareTargetUser and ShareTargetGroup are the share dialog's Workspace-
// scoped picker entries (#159, ADR-0045) — every other Member and every
// Group of the Calendar's own Workspace.
export interface ShareTargetUser {
  userId: number;
  name: string;
  email: string;
}

export interface ShareTargetGroup {
  groupId: number;
  name: string;
}

export interface ShareTargets {
  users: ShareTargetUser[];
  groups: ShareTargetGroup[];
}

// DefaultReminders is the caller's own Default reminders on a Calendar
// (ADR-0064): two independent lists, since an all-day Reminder's offset
// counts back from 09:00 rather than midnight.
export interface DefaultReminders {
  timed: Reminder[];
  allDay: Reminder[];
}

export const calendarsApi = {
  async list(accessToken: string): Promise<Calendar[]> {
    const response = await authedFetch(accessToken, "/api/calendars/", {
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Calendar[];
  },

  async create(
    accessToken: string,
    calendar: { id: string; name: string; color: string },
  ): Promise<Calendar> {
    const response = await authedFetch(accessToken, "/api/calendars/", {
      method: "POST",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
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
    const response = await authedFetch(accessToken, `/api/calendars/${id}`, {
      method: "PATCH",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(changes),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Calendar;
  },

  // updateColor sets the caller's personal colour on id (ADR-0038): an
  // Owner's write lands on the Calendar's own colour, and anyone else's with
  // Access lands as their own override, over this same PATCH endpoint (#122)
  // — the app never chooses which happens, the server does. Sends only
  // color, never name/url/keepAlarms, since those remain Owner-only on this
  // endpoint and a non-Owner's request must not appear to touch them.
  async updateColor(
    accessToken: string,
    id: string,
    color: string,
  ): Promise<Calendar> {
    const response = await authedFetch(accessToken, `/api/calendars/${id}`, {
      method: "PATCH",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ color }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Calendar;
  },

  async remove(accessToken: string, id: string): Promise<void> {
    const response = await authedFetch(accessToken, `/api/calendars/${id}`, {
      method: "DELETE",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  async get(accessToken: string, id: string): Promise<Calendar> {
    const response = await authedFetch(accessToken, `/api/calendars/${id}`, {
      credentials: "include",
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
    const response = await authedFetch(accessToken, `/api/calendars/${id}/refresh`, {
      method: "POST",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as RefreshResult;
  },

  // previewSubscription fetches and parses url without writing anything
  // (#83). It shares the Workspace-scoped /subscribe endpoint with subscribe
  // below, so it asserts the active Workspace the same way — omitting that
  // is what made every URL fail identically (#225).
  async previewSubscription(
    accessToken: string,
    url: string,
  ): Promise<SubscriptionPreview> {
    const response = await authedFetch(accessToken, "/api/calendars/subscribe?dryRun=1", {
      method: "POST",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
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
    const response = await authedFetch(accessToken, "/api/calendars/subscribe?dryRun=0", {
      method: "POST",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify(subscription),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Calendar;
  },

  // listShares returns every Share on id, Owner-only (#113, ADR-0034).
  async listShares(accessToken: string, id: string): Promise<Share[]> {
    const response = await authedFetch(accessToken, `/api/calendars/${id}/shares`, {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Share[];
  },

  // share grants id a Share to the User named by email with role, or
  // changes an existing Share's role if they already have one (#113,
  // ADR-0034, ADR-0047). Owner-only.
  async share(
    accessToken: string,
    id: string,
    email: string,
    role: Role,
  ): Promise<Share> {
    const response = await authedFetch(accessToken, `/api/calendars/${id}/shares`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email, role }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Share;
  },

  // revokeShare removes userId's Share on id (#113, ADR-0034). Owner-only.
  async revokeShare(accessToken: string, id: string, userId: number): Promise<void> {
    const response = await authedFetch(accessToken, `/api/calendars/${id}/shares/${userId}`, {
      method: "DELETE",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  // leave renounces the caller's own Share on id (#114, ADR-0034) — the
  // non-Owner counterpart to remove, which only an Owner may call.
  async leave(accessToken: string, id: string): Promise<void> {
    const response = await authedFetch(accessToken, `/api/calendars/${id}/leave`, {
      method: "POST",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  // shareTargets returns every User and Group of id's own Workspace the
  // share dialog may offer as a target (#159, ADR-0045). Owner-only.
  async shareTargets(accessToken: string, id: string): Promise<ShareTargets> {
    const response = await authedFetch(accessToken, `/api/calendars/${id}/share-targets`, {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as ShareTargets;
  },

  // listGroupShares returns every Group Share on id, Owner-only (#159,
  // ADR-0045).
  async listGroupShares(accessToken: string, id: string): Promise<GroupShare[]> {
    const response = await authedFetch(accessToken, `/api/calendars/${id}/group-shares`, {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as GroupShare[];
  },

  // shareWithGroup grants id a Share to groupId with role, or changes an
  // existing Group Share's role if groupId already has one (#159,
  // ADR-0045). Owner-only.
  async shareWithGroup(
    accessToken: string,
    id: string,
    groupId: number,
    role: Role,
  ): Promise<GroupShare> {
    const response = await authedFetch(accessToken, `/api/calendars/${id}/group-shares`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ groupId, role }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as GroupShare;
  },

  // revokeGroupShare removes groupId's Share on id (#159, ADR-0045).
  // Owner-only.
  async revokeGroupShare(accessToken: string, id: string, groupId: number): Promise<void> {
    const response = await authedFetch(accessToken, `/api/calendars/${id}/group-shares/${groupId}`, {
      method: "DELETE",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  // getDefaultReminders returns the caller's own Default reminders on id
  // (ADR-0064) — empty lists, not an error, if they've never set either.
  // Open to any User with Access, not Owner-only, same posture as the
  // colour override.
  async getDefaultReminders(accessToken: string, id: string): Promise<DefaultReminders> {
    const response = await authedFetch(accessToken, `/api/calendars/${id}/default-reminders`, {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    // Both lists are normalised to arrays: a server that serves null for a
    // list nobody has set yet (every Calendar, until someone saves a default)
    // must not reach a caller that maps over it.
    const wire = (await response.json()) as Partial<DefaultReminders> | null;
    return { timed: wire?.timed ?? [], allDay: wire?.allDay ?? [] };
  },

  // setDefaultReminders replaces the caller's own default Reminder list —
  // timed or all-day, whichever allDay names — wholesale (ADR-0064). Never
  // touches the other list or another User's rows on the same Calendar.
  async setDefaultReminders(
    accessToken: string,
    id: string,
    allDay: boolean,
    reminders: Reminder[],
  ): Promise<Reminder[]> {
    const response = await authedFetch(accessToken, `/api/calendars/${id}/default-reminders`, {
      method: "PUT",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ allDay, reminders }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Reminder[];
  },
};
