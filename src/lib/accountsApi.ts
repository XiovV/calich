import { authHeader, errorFromResponse } from "./apiClient";

// Account administration (ADR-0037), Admin-only. #119 covers listing and
// creation; #120 adds reset password, admin grant/revoke, and disable —
// delete is #121.
export interface Account {
  id: number;
  username: string;
  isAdmin: boolean;
  isDisabled: boolean;
  mustChangePassword: boolean;
  createdAt: string;
}

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
};
