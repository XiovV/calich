// What produced a Notification (ADR-0061): a fired Notification-Channel
// Reminder, or being made an Attendee of an Event.
export type NotificationKind = "reminder" | "invite";

interface NotificationBase {
  id: number;
  eventId: string;
  title: string;
  firedAt: Date;
  seen: boolean;
}

// The persistent in-app record of something that concerns one User
// (ADR-0021, ADR-0061, CONTEXT.md). Distinct from a toast, which is a
// transient client-only UI message. A discriminated union on kind, rather
// than an always-present nullable occurrenceStart, so a reminder's start is
// known non-null wherever kind has already been checked — an invite
// concerns the Event series rather than one Occurrence, so it has none.
export type Notification =
  | (NotificationBase & { kind: "reminder"; occurrenceStart: Date })
  | (NotificationBase & { kind: "invite"; occurrenceStart: null });
