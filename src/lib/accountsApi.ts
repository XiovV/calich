import { authHeader, errorFromResponse } from "./apiClient";

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
}

function fromWire(wire: AccountWire): Account {
  return {
    id: wire.id,
    username: wire.username,
    isAdmin: wire.is_admin,
    isDisabled: wire.is_disabled,
    mustChangePassword: wire.must_change_password,
    createdAt: wire.created_at,
  };
}

export const accountsApi = {
  async list(accessToken: string): Promise<Account[]> {
    const response = await fetch("/api/accounts/", {
      credentials: "include",
      headers: authHeader(accessToken),
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as AccountWire[];
    return body.map(fromWire);
  },

  async create(accessToken: string, username: string, password: string): Promise<Account> {
    const response = await fetch("/api/accounts/", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify({ username, password }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire(await response.json());
  },

  async resetPassword(accessToken: string, id: number, password: string): Promise<Account> {
    const response = await fetch(`/api/accounts/${id}/reset-password`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify({ password }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire(await response.json());
  },

  async setAdmin(accessToken: string, id: number, isAdmin: boolean): Promise<Account> {
    const response = await fetch(`/api/accounts/${id}/admin`, {
      method: "PUT",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify({ is_admin: isAdmin }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire(await response.json());
  },

  async setDisabled(accessToken: string, id: number, isDisabled: boolean): Promise<Account> {
    const response = await fetch(`/api/accounts/${id}/disabled`, {
      method: "PUT",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify({ is_disabled: isDisabled }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire(await response.json());
  },

  async deleteImpact(accessToken: string, id: number): Promise<DeleteImpact> {
    const response = await fetch(`/api/accounts/${id}/delete-impact`, {
      credentials: "include",
      headers: authHeader(accessToken),
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
    const response = await fetch(`/api/accounts/${id}`, {
      method: "DELETE",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify(
        disposition === "transfer"
          ? { owned_calendars: disposition, transfer_to: transferTo }
          : { owned_calendars: disposition },
      ),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
