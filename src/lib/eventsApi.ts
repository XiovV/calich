import { format } from "date-fns";
import { authedFetch, errorFromResponse } from "./apiClient";
import type { Attachment, Event, Reminder } from "./event";

interface EventWire {
  id: string;
  calendarId: string;
  title: string;
  start: string;
  end: string;
  // Absent (omitted by the backend) for a non-recurring event.
  allDay?: boolean;
  rrule?: string;
  // Present only on an Override (ADR-0016).
  parentId?: string;
  recurrenceId?: string;
  // A Master's cancelled Occurrence starts (Exceptions). Absent on an Override.
  exdates?: string[];
  // Absent/null for a Floating Event (ADR-0019).
  tzid?: string | null;
  // Absent/empty means no Reminders (ADR-0020).
  reminders?: Reminder[];
  description?: string;
  location?: string;
  // Absent means "inherit the Calendar's color" (ADR-0043).
  color?: string;
  // createdByName is absent when the creator's account has been
  // deleted or the Event predates this column (#118).
  createdByName?: string;
  // Present only on a Master with at least one Attachment (#132, ADR-0040).
  attachments?: AttachmentWire[];
  // calendarId's Name and resolved display Color (ADR-0046) — always
  // present, so an Attendee with no Access to calendarId still has
  // something to render for it.
  calendarName?: string;
  calendarColor?: string;
  attendeeCount?: number;
}

interface AttachmentWire {
  id: string;
  filename: string;
  contentType: string;
  sizeBytes: number;
  uploadedBy?: number;
  uploadedByName?: string;
  createdAt: string;
}

export function attachmentFromWire(wire: AttachmentWire): Attachment {
  return {
    id: wire.id,
    filename: wire.filename,
    contentType: wire.contentType,
    sizeBytes: wire.sizeBytes,
    uploadedByName: wire.uploadedByName || undefined,
    createdAt: new Date(wire.createdAt),
  };
}

/**
 * A local Date parsed from an all-day Event's date-only wire string
 * ("2026-08-04") as local midnight — never through `new Date(string)`, which
 * reads a bare date as UTC midnight and can land on the wrong local day
 * (ADR-0017).
 */
function fromDateOnly(value: string): Date {
  const [year, month, day] = value.split("-").map(Number);
  return new Date(year, month - 1, day);
}

/** `fromDateOnly`'s inverse: a date-only wire string from a local Date's own
 * calendar fields, never `toISOString()` (ADR-0017). */
function toDateOnly(date: Date): string {
  return format(date, "yyyy-MM-dd");
}

function parseEventTime(value: string, allDay: boolean | undefined): Date {
  return allDay ? fromDateOnly(value) : new Date(value);
}

function serializeEventTime(date: Date, allDay: boolean | undefined): string {
  return allDay ? toDateOnly(date) : date.toISOString();
}

function fromWire(wire: EventWire): Event {
  return {
    id: wire.id,
    calendarId: wire.calendarId,
    title: wire.title,
    start: parseEventTime(wire.start, wire.allDay),
    end: parseEventTime(wire.end, wire.allDay),
    allDay: wire.allDay || undefined,
    rrule: wire.rrule || undefined,
    parentId: wire.parentId,
    recurrenceId: wire.recurrenceId ? new Date(wire.recurrenceId) : undefined,
    exdates: wire.exdates?.map((exdate) => new Date(exdate)),
    tzid: wire.tzid ?? undefined,
    reminders: wire.reminders,
    description: wire.description || undefined,
    location: wire.location || undefined,
    color: wire.color ?? undefined,
    createdByName: wire.createdByName || undefined,
    attachments: wire.attachments?.map(attachmentFromWire),
    calendarName: wire.calendarName || undefined,
    calendarColor: wire.calendarColor || undefined,
    attendeeCount: wire.attendeeCount || undefined,
  };
}

export const eventsApi = {
  async list(accessToken: string): Promise<Event[]> {
    const response = await authedFetch(accessToken, "/api/events/", {
      credentials: "include",
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
      allDay?: boolean;
      rrule?: string;
      parentId?: string;
      recurrenceId?: Date;
      tzid?: string;
      reminders?: Reminder[];
      description?: string;
      location?: string;
      color?: string;
      // Attendees to invite as part of this create (#187, ADR-0055) — a
      // Group id expands to its current members server-side, inside the
      // same transaction as the Event row itself.
      attendeeUserIds?: number[];
      attendeeGroupIds?: number[];
    },
  ): Promise<Event> {
    const response = await authedFetch(accessToken, "/api/events/", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        id: event.id,
        calendarId: event.calendarId,
        title: event.title,
        start: serializeEventTime(event.start, event.allDay),
        end: serializeEventTime(event.end, event.allDay),
        allDay: event.allDay ?? false,
        rrule: event.rrule ?? "",
        parentId: event.parentId,
        recurrenceId: event.recurrenceId?.toISOString(),
        tzid: event.tzid,
        reminders: event.reminders,
        description: event.description,
        location: event.location,
        color: event.color,
        attendeeUserIds: event.attendeeUserIds,
        attendeeGroupIds: event.attendeeGroupIds,
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
      allDay?: boolean;
      rrule?: string;
      tzid?: string;
      reminders?: Reminder[];
      description?: string;
      location?: string;
      color?: string;
    },
  ): Promise<Event> {
    const response = await authedFetch(accessToken, `/api/events/${id}`, {
      method: "PATCH",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        calendarId: changes.calendarId,
        title: changes.title,
        start: serializeEventTime(changes.start, changes.allDay),
        end: serializeEventTime(changes.end, changes.allDay),
        allDay: changes.allDay ?? false,
        rrule: changes.rrule ?? "",
        tzid: changes.tzid,
        reminders: changes.reminders,
        description: changes.description,
        location: changes.location,
        color: changes.color,
      }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire((await response.json()) as EventWire);
  },

  async remove(accessToken: string, id: string): Promise<void> {
    const response = await authedFetch(accessToken, `/api/events/${id}`, {
      method: "DELETE",
      credentials: "include",
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
    const response = await authedFetch(accessToken, `/api/events/${parentId}/exceptions`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
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
    const response = await authedFetch(accessToken, `/api/events/${oldParentId}/reparent`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        newParentId,
        fromStart: fromStart.toISOString(),
      }),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
