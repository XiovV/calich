import type { Reminder } from "../lib/event";

/** A Reminder plus a local id, so its row keeps stable identity across
 * add/remove/reorder in a Reminders section (Reminder itself has no id —
 * it's just an offset + channel pair, ADR-0020). Shared by the Event
 * modal's own Reminders section and the Calendar modal's two Default
 * reminders lists (ADR-0064). */
export type ReminderDraft = Reminder & { draftId: string };

export function toReminderDrafts(reminders: Reminder[]): ReminderDraft[] {
  return reminders.map((reminder) => ({ ...reminder, draftId: crypto.randomUUID() }));
}

export function fromReminderDrafts(drafts: ReminderDraft[]): Reminder[] {
  return drafts.map(({ offsetMinutes, channel }) => ({ offsetMinutes, channel }));
}
