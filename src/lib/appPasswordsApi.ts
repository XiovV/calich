import { authHeader, errorFromResponse } from "./apiClient";

export interface AppPassword {
  id: number;
  label: string;
  createdAt: string;
  lastUsedAt: string | null;
}

export interface CreatedAppPassword extends AppPassword {
  secret: string;
}

interface AppPasswordWire {
  id: number;
  label: string;
  created_at: string;
  last_used_at: string | null;
}

function fromWire(wire: AppPasswordWire): AppPassword {
  return {
    id: wire.id,
    label: wire.label,
    createdAt: wire.created_at,
    lastUsedAt: wire.last_used_at,
  };
}

export const appPasswordsApi = {
  async list(accessToken: string): Promise<AppPassword[]> {
    const response = await fetch("/api/app-passwords/", {
      credentials: "include",
      headers: authHeader(accessToken),
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as AppPasswordWire[];
    return body.map(fromWire);
  },

  async create(accessToken: string, label: string): Promise<CreatedAppPassword> {
    const response = await fetch("/api/app-passwords/", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify({ label }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as AppPasswordWire & { secret: string };
    return { ...fromWire(body), secret: body.secret };
  },

  async revoke(accessToken: string, id: number): Promise<void> {
    const response = await fetch(`/api/app-passwords/${id}`, {
      method: "DELETE",
      credentials: "include",
      headers: authHeader(accessToken),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
