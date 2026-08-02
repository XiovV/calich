import { authHeader, errorFromResponse } from "./apiClient";
import type { Event } from "./event";

interface EventWire {
  id: string;
  calendarId: string;
  title: string;
  start: string;
  end: string;
}

function fromWire(wire: EventWire): Event {
  return {
    id: wire.id,
    calendarId: wire.calendarId,
    title: wire.title,
    start: new Date(wire.start),
    end: new Date(wire.end),
  };
}

export const eventsApi = {
  async list(accessToken: string): Promise<Event[]> {
    const response = await fetch("/api/events/", {
      credentials: "include",
      headers: authHeader(accessToken),
    });
    if (!response.ok) throw await errorFromResponse(response);

    const wire = (await response.json()) as EventWire[];
    return wire.map(fromWire);
  },

  async create(
    accessToken: string,
    event: { id: string; calendarId: string; title: string; start: Date; end: Date },
  ): Promise<Event> {
    const response = await fetch("/api/events/", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify({
        id: event.id,
        calendarId: event.calendarId,
        title: event.title,
        start: event.start.toISOString(),
        end: event.end.toISOString(),
      }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire((await response.json()) as EventWire);
  },

  async update(
    accessToken: string,
    id: string,
    changes: { calendarId: string; title: string; start: Date; end: Date },
  ): Promise<Event> {
    const response = await fetch(`/api/events/${id}`, {
      method: "PATCH",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify({
        calendarId: changes.calendarId,
        title: changes.title,
        start: changes.start.toISOString(),
        end: changes.end.toISOString(),
      }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire((await response.json()) as EventWire);
  },

  async remove(accessToken: string, id: string): Promise<void> {
    const response = await fetch(`/api/events/${id}`, {
      method: "DELETE",
      credentials: "include",
      headers: authHeader(accessToken),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
