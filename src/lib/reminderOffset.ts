/**
 * A Reminder's offset (minutes before the Occurrence start; see ADR-0020) and
 * Channel, as edited directly via an amount + unit entry — no preset list
 * (ADR-0022).
 */
import type { ReminderChannel } from "./event";

/** A Reminder Channel dropdown option. */
export interface ReminderChannelOption {
  value: ReminderChannel;
  label: string;
}

/** The Channel options offered in a Reminder row's Channel dropdown. Email is
 * only offered when `emailAvailable` — the user has an account email set
 * *and* the self-hoster has SMTP configured (ADR-0021, ADR-0010). */
export function reminderChannelOptions(emailAvailable: boolean): ReminderChannelOption[] {
  const options: ReminderChannelOption[] = [{ value: "notification", label: "Notification" }];
  if (emailAvailable) options.push({ value: "email", label: "Email" });
  return options;
}

/** A Reminder offset entry's input unit — minutes and up, matching the
 * Custom recurrence dialog's day/week/month/year interval+unit pattern. */
export type ReminderOffsetUnit = "minutes" | "hours" | "days" | "weeks";

/** The unit dropdown options for a Reminder offset entry, shared by
 * ReminderRow and ReminderOverrideControl so the two don't drift apart. */
export const REMINDER_OFFSET_UNIT_OPTIONS: { value: ReminderOffsetUnit; label: string }[] = [
  { value: "minutes", label: "minutes" },
  { value: "hours", label: "hours" },
  { value: "days", label: "days" },
  { value: "weeks", label: "weeks" },
];

const UNIT_MINUTES: Record<ReminderOffsetUnit, number> = {
  minutes: 1,
  hours: 60,
  days: 1440,
  weeks: 10080,
};

/** Normalizes an offset entry to minutes before the anchor. Negative or
 * fractional amounts clamp to a whole, non-negative number of minutes. */
export function normalizeCustomOffset(amount: number, unit: ReminderOffsetUnit): number {
  return Math.max(0, Math.round(amount)) * UNIT_MINUTES[unit];
}

/** Splits `offsetMinutes` back into an amount + the largest unit that divides
 * it evenly, so the offset entry's fields display naturally (e.g. 2880 as "2
 * days" rather than "2880 minutes") when an existing Reminder is reopened. */
export function splitCustomOffset(
  offsetMinutes: number,
): { amount: number; unit: ReminderOffsetUnit } {
  const units: ReminderOffsetUnit[] = ["weeks", "days", "hours", "minutes"];
  for (const unit of units) {
    const perUnit = UNIT_MINUTES[unit];
    if (offsetMinutes !== 0 && offsetMinutes % perUnit === 0) {
      return { amount: offsetMinutes / perUnit, unit };
    }
  }
  return { amount: offsetMinutes, unit: "minutes" };
}
