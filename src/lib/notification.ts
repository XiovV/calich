// The persistent in-app record created when a Notification-Channel Reminder
// fires (ADR-0021, CONTEXT.md). Distinct from a toast, which is a transient
// client-only UI message.
export interface Notification {
  id: number;
  eventId: string;
  title: string;
  occurrenceStart: Date;
  firedAt: Date;
  seen: boolean;
}
