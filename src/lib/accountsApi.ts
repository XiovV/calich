import { authedFetch, errorFromResponse } from "./apiClient";

// Account administration (ADR-0037), Admin-only. #119 covers listing and
// creation, #120 adds reset password/admin grant-revoke/disable, and #121
// adds delete with a disposition for the Calendars the account owned.
export interface Account {
  id: number;
  username: string;
  isAdmin: boolean;
  isDisabled: boolean;
  mustChangePassword: boolean;
  createdAt: string;
  // isPending is whether this account was created via an Invite and hasn't
  // accepted it yet (ADR-0042) — a distinct state from isDisabled even
  // though both block login.
  isPending: boolean;
  // inviteExpiresAt is when the current outstanding Invite token stops being
  // acceptable; null once isPending is false.
  inviteExpiresAt: string | null;
  // inviteEmailAvailable is whether the "send email" invite action can be
  // offered for this row: it has an email on file and this deployment has
  // SMTP configured. Always false once isPending is false.
  inviteEmailAvailable: boolean;
}

// An issued or reissued Invite (ADR-0042): the resulting Pending account
// alongside the plaintext token, shown to the Admin exactly once — nothing
// retrieves it again once this response is handled.
export interface InviteResult {
  account: Account;
  token: string;
}

interface InviteResultWire {
  account: AccountWire;
  token: string;
}

// What deleting an account would affect (ADR-0037): every Calendar it owns
// that carries a Share, and how many distinct Users, across all of them,
// would lose Access. Fetched before the disposition is chosen so an Admin
// commits to DispositionDelete with the cost already in view.
export interface DeleteImpact {
  calendars: { id: string; name: string; shareCount: number }[];
  affectedUserCount: number;
}

interface DeleteImpactWire {
  calendars: { id: string; name: string; share_count: number }[];
  affected_user_count: number;
}

export type AccountDisposition = "transfer" | "delete";

interface AccountWire {
  id: number;
  username: string;
  is_admin: boolean;
  is_disabled: boolean;
  must_change_password: boolean;
  created_at: string;
  is_pending: boolean;
  invite_expires_at?: string;
  invite_email_available: boolean;
}

function fromWire(wire: AccountWire): Account {
  return {
    id: wire.id,
    username: wire.username,
    isAdmin: wire.is_admin,
    isDisabled: wire.is_disabled,
    mustChangePassword: wire.must_change_password,
    createdAt: wire.created_at,
    isPending: wire.is_pending,
    inviteExpiresAt: wire.invite_expires_at ?? null,
    inviteEmailAvailable: wire.invite_email_available,
  };
}

function fromInviteResultWire(wire: InviteResultWire): InviteResult {
  return { account: fromWire(wire.account), token: wire.token };
}

export const accountsApi = {
  async list(accessToken: string): Promise<Account[]> {
    const response = await authedFetch(accessToken, "/api/accounts/", {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as AccountWire[];
    return body.map(fromWire);
  },

  async create(accessToken: string, username: string, password: string): Promise<Account> {
    const response = await authedFetch(accessToken, "/api/accounts/", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire(await response.json());
  },

  // Invites an account instead of creating one with a temporary password
  // (ADR-0042): email is optional, and the token this returns is shown to
  // the Admin exactly once, to distribute however they choose.
  async createInvite(accessToken: string, username: string, email: string): Promise<InviteResult> {
    const response = await authedFetch(accessToken, "/api/accounts/invite", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, email }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromInviteResultWire(await response.json());
  },

  // Replaces id's outstanding Invite with a fresh token and a reset 7-day
  // expiry (ADR-0042), invalidating whichever token came before it.
  async reissueInvite(accessToken: string, id: number): Promise<InviteResult> {
    const response = await authedFetch(accessToken, `/api/accounts/${id}/invite/reissue`, {
      method: "POST",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromInviteResultWire(await response.json());
  },

  // Deletes id's Pending account outright (ADR-0042) — a Pending account
  // owns nothing, so there is no disposition to choose, unlike delete().
  async cancelInvite(accessToken: string, id: number): Promise<void> {
    const response = await authedFetch(accessToken, `/api/accounts/${id}/invite`, {
      method: "DELETE",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  // Emails id's Invite link to the address on file (ADR-0042). link is
  // built by the caller — this browser's own origin, not the server's
  // concern — and sent verbatim.
  async sendInviteEmail(accessToken: string, id: number, link: string): Promise<void> {
    const response = await authedFetch(accessToken, `/api/accounts/${id}/invite/email`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ link }),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  async resetPassword(accessToken: string, id: number, password: string): Promise<Account> {
    const response = await authedFetch(accessToken, `/api/accounts/${id}/reset-password`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ password }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire(await response.json());
  },

  async setAdmin(accessToken: string, id: number, isAdmin: boolean): Promise<Account> {
    const response = await authedFetch(accessToken, `/api/accounts/${id}/admin`, {
      method: "PUT",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ is_admin: isAdmin }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire(await response.json());
  },

  async setDisabled(accessToken: string, id: number, isDisabled: boolean): Promise<Account> {
    const response = await authedFetch(accessToken, `/api/accounts/${id}/disabled`, {
      method: "PUT",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ is_disabled: isDisabled }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire(await response.json());
  },

  async renameUsername(accessToken: string, id: number, username: string): Promise<Account> {
    const response = await authedFetch(accessToken, `/api/accounts/${id}/username`, {
      method: "PUT",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire(await response.json());
  },

  // How many App passwords id's account holds (#125) — shown before renaming
  // somebody else's account, since CalDAV auth is keyed by username and every
  // device already configured with the old one stops syncing until it's
  // reconfigured. Fetched separately from deleteImpact, which asks a
  // different question (Calendars and Shares).
  async usernameImpact(accessToken: string, id: number): Promise<{ appPasswordCount: number }> {
    const response = await authedFetch(accessToken, `/api/accounts/${id}/username-impact`, {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as { app_password_count: number };
    return { appPasswordCount: body.app_password_count };
  },

  async deleteImpact(accessToken: string, id: number): Promise<DeleteImpact> {
    const response = await authedFetch(accessToken, `/api/accounts/${id}/delete-impact`, {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as DeleteImpactWire;
    return {
      calendars: body.calendars.map((c) => ({ id: c.id, name: c.name, shareCount: c.share_count })),
      affectedUserCount: body.affected_user_count,
    };
  },

  async delete(
    accessToken: string,
    id: number,
    disposition: AccountDisposition,
    transferTo?: number,
  ): Promise<void> {
    const response = await authedFetch(accessToken, `/api/accounts/${id}`, {
      method: "DELETE",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(
        disposition === "transfer"
          ? { owned_calendars: disposition, transfer_to: transferTo }
          : { owned_calendars: disposition },
      ),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
