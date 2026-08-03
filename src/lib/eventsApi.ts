import { authHeader, errorFromResponse } from "./apiClient";
import type { Event } from "./event";

interface EventWire {
  id: string;
  calendarId: string;
  title: string;
  start: string;
  end: string;
  // Absent (omitted by the backend) for a non-recurring event.
  rrule?: string;
  // Present only on an Override (ADR-0016).
  parentId?: string;
  recurrenceId?: string;
  // A Master's cancelled Occurrence starts (Exceptions). Absent on an Override.
  exdates?: string[];
}

function fromWire(wire: EventWire): Event {
  return {
    id: wire.id,
    calendarId: wire.calendarId,
    title: wire.title,
    start: new Date(wire.start),
    end: new Date(wire.end),
    rrule: wire.rrule || undefined,
    parentId: wire.parentId,
    recurrenceId: wire.recurrenceId ? new Date(wire.recurrenceId) : undefined,
    exdates: wire.exdates?.map((exdate) => new Date(exdate)),
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
    event: {
      id: string;
      calendarId: string;
      title: string;
      start: Date;
      end: Date;
      rrule?: string;
      parentId?: string;
      recurrenceId?: Date;
    },
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
        rrule: event.rrule ?? "",
        parentId: event.parentId,
        recurrenceId: event.recurrenceId?.toISOString(),
      }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire((await response.json()) as EventWire);
  },

  async update(
    accessToken: string,
    id: string,
    changes: {
      calendarId: string;
      title: string;
      start: Date;
      end: Date;
      rrule?: string;
    },
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
        rrule: changes.rrule ?? "",
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

  /**
   * Cancels a single Occurrence of a recurring master (deleting "this event"
   * on a recurring Occurrence), stored as an iCalendar EXDATE (ADR-0016).
   */
  async addException(
    accessToken: string,
    parentId: string,
    occurrenceStart: Date,
  ): Promise<void> {
    const response = await fetch(`/api/events/${parentId}/exceptions`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify({ occurrenceStart: occurrenceStart.toISOString() }),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  /**
   * Moves every Override/Exception of oldParentId at-or-after fromStart to
   * belong to newParentId instead — the "this and following" split
   * reparenting overrides/exceptions at the boundary (ADR-0016).
   */
  async reparentSeries(
    accessToken: string,
    oldParentId: string,
    newParentId: string,
    fromStart: Date,
  ): Promise<void> {
    const response = await fetch(`/api/events/${oldParentId}/reparent`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify({
        newParentId,
        fromStart: fromStart.toISOString(),
      }),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
