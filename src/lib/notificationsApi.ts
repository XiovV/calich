import { authedFetch, errorFromResponse } from "./apiClient";
import type { Notification, NotificationKind } from "./notification";

interface NotificationWire {
  id: number;
  eventId: string;
  kind: NotificationKind;
  title: string;
  occurrenceStart: string | null;
  firedAt: string;
  seen: boolean;
}

function fromWire(wire: NotificationWire): Notification {
  const base = {
    id: wire.id,
    eventId: wire.eventId,
    title: wire.title,
    firedAt: new Date(wire.firedAt),
    seen: wire.seen,
  };

  if (wire.kind === "invite") {
    return { ...base, kind: "invite", occurrenceStart: null };
  }
  // occurrenceStart is always set for a reminder Notification (server
  // invariant) — the one non-null assertion at this wire boundary is what
  // lets every other reader rely on the type instead of re-checking.
  return { ...base, kind: "reminder", occurrenceStart: new Date(wire.occurrenceStart!) };
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
