import { authHeader, errorFromResponse } from "./apiClient";
import type { Calendar } from "./calendar";

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
    changes: { name: string; color: string },
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
};
