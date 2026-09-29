// What produced a Notification (ADR-0061, ADR-0081): a fired
// Notification-Channel Reminder, being made an Attendee of an Event, or a
// Write-back push permanently failing on a writable Linked Calendar.
export type NotificationKind = "reminder" | "invite" | "writeback_failed";

interface NotificationBase {
  id: number;
  title: string;
  firedAt: Date;
  seen: boolean;
}

// The persistent in-app record of something that concerns one User
// (ADR-0021, ADR-0061, ADR-0081, CONTEXT.md). Distinct from a toast, which
// is a transient client-only UI message. A discriminated union on kind,
// rather than always-present nullable eventId/calendarId/occurrenceStart,
// so each variant's own fields are known non-null wherever kind has already
// been checked — reminder and invite concern one Event (an invite the whole
// series, so it has no occurrenceStart of its own), while writeback_failed
// concerns a Calendar instead (coalesced there, not on any one Event) and
// has neither.
export type Notification =
  | (NotificationBase & { kind: "reminder"; eventId: string; occurrenceStart: Date })
  | (NotificationBase & { kind: "invite"; eventId: string; occurrenceStart: null })
  | (NotificationBase & { kind: "writeback_failed"; calendarId: string; occurrenceStart: null });
