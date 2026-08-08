import { authedFetch, errorFromResponse } from "./apiClient";
import type { Notification } from "./notification";

interface NotificationWire {
  id: number;
  eventId: string;
  title: string;
  occurrenceStart: string;
  firedAt: string;
  seen: boolean;
}

function fromWire(wire: NotificationWire): Notification {
  return {
    id: wire.id,
    eventId: wire.eventId,
    title: wire.title,
    occurrenceStart: new Date(wire.occurrenceStart),
    firedAt: new Date(wire.firedAt),
    seen: wire.seen,
  };
}

export const notificationsApi = {
  async list(accessToken: string): Promise<Notification[]> {
    const response = await authedFetch(accessToken, "/api/notifications/", {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    const wire = (await response.json()) as NotificationWire[];
    return wire.map(fromWire);
  },

  async markSeen(accessToken: string): Promise<void> {
    const response = await authedFetch(accessToken, "/api/notifications/seen", {
      method: "POST",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
