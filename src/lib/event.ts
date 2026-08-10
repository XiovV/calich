// A Reminder's delivery method — Notification (shown in-app) or Email (sent
// to the User). See ADR-0020 and CONTEXT.md's Channel.
export type ReminderChannel = "notification" | "email";

// A cue attached to an Event that fires offsetMinutes before an Occurrence's
// start, on channel. Maps to one iCalendar VALARM. See ADR-0020.
export interface Reminder {
  offsetMinutes: number;
  channel: ReminderChannel;
}

// A file held against a Master, shown on every Occurrence of its series —
// an Override never carries its own (#132, ADR-0040).
export interface Attachment {
  id: string;
  filename: string;
  contentType: string;
  sizeBytes: number;
  // uploadedByName is who uploaded this Attachment, for display on a
  // shared Calendar — absent when the uploader's account has been deleted.
  uploadedByName?: string;
  createdAt: Date;
}

export interface Event {
  id: string;
  calendarId: string;
  title: string;
  start: Date;
  end: Date;
  // Flags this Event as occupying whole dates rather than a time range.
  // start/end still hold the half-open date range (start = the date, end =
  // the exclusive next day) and are wall-clock dates, never
  // timezone-converted. See ADR-0017 and CONTEXT.md's All-day Event.
  allDay?: boolean;
  // The iCalendar RRULE (RFC 5545) describing how this Event repeats, or
  // undefined when it does not recur. The Event's start/end define the first
  // Occurrence and the duration every Occurrence inherits. See ADR-0016.
  rrule?: string;
  // parentId/recurrenceId are set together, only on an Override: a standalone
  // Event that replaces one Occurrence of its parent's series, keyed to that
  // Occurrence's start (its RECURRENCE-ID). Undefined on a Master. See
  // ADR-0016 and CONTEXT.md.
  parentId?: string;
  recurrenceId?: Date;
  // Exdates lists a Master's cancelled Occurrence starts (Exceptions).
  // Undefined/empty on an Override.
  exdates?: Date[];
  // The Anchor zone: a named IANA zone, "Etc/UTC" for an absolute instant, or
  // undefined for a Floating Event, which renders/expands in the Viewer
  // zone. See ADR-0019 and CONTEXT.md's Anchor zone/Floating Event.
  tzid?: string;
  // This Event's Reminders. Undefined/empty means none. See ADR-0020.
  reminders?: Reminder[];
  // Free-text description and location, both optional.
  description?: string;
  location?: string;
  // This Event's own color override, in the same arbitrary-hex value space
  // as Calendar color. Absent means "inherit the Calendar's color" — wins
  // outright over it when set. Set by an Editor, seen identically by
  // everyone with Access (not per-viewer, unlike Calendar color's own
  // override). See ADR-0043.
  color?: string;
  // createdByName is who created this Event, for display in the Event
  // dialog on a Calendar where more than one person could have created it
  // (#118) — never consulted to decide what a User may do with an Event,
  // which resolves through its Calendar's Access instead (ADR-0034).
  // Undefined when the creator's account has been deleted, or the Event
  // predates this field.
  createdByName?: string;
  // This Event's Attachments (#132, ADR-0040). Undefined/empty on an
  // Override — it never carries its own — or when the Master has none.
  attachments?: Attachment[];
  // calendarId's Name and resolved display Color, for display only — never
  // consulted to decide what a User may do with an Event, which resolves
  // through Calendar Access instead. Present on every Event; the sole
  // reason it exists is so a User who can see this Event only as an
  // Attendee (ADR-0046) — with no Access to calendarId, and thus no entry
  // for it in calendarsStore — still has a Calendar name/color to render.
  calendarName?: string;
  calendarColor?: string;
}
