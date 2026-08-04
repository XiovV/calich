/**
 * The fixed set of Reminder-offset presets offered in the event modal's
 * Reminders section. Each maps to an `offsetMinutes` (minutes before the
 * Occurrence start; see ADR-0020) plus a "Custom…" entry for anything else,
 * mirroring `recurrencePresets.ts`'s Repeat dropdown.
 *
 * All-day Events measure the offset from the 09:00 anchor instead of
 * midnight (ADR-0020), so they offer a coarser, day-granular preset set with
 * labels naming the anchor time rather than the minute presets timed Events use.
 */
import type { ReminderChannel } from "./event";

export type ReminderPreset =
  | "at-start"
  | "5-minutes"
  | "10-minutes"
  | "15-minutes"
  | "30-minutes"
  | "1-hour"
  | "1-day"
  | "2-days"
  | "1-week";

const TIMED_PRESETS: ReminderPreset[] = [
  "at-start",
  "5-minutes",
  "10-minutes",
  "15-minutes",
  "30-minutes",
  "1-hour",
  "1-day",
  "1-week",
];

const ALL_DAY_PRESETS: ReminderPreset[] = ["at-start", "1-day", "2-days", "1-week"];

const PRESET_OFFSET_MINUTES: Record<ReminderPreset, number> = {
  "at-start": 0,
  "5-minutes": 5,
  "10-minutes": 10,
  "15-minutes": 15,
  "30-minutes": 30,
  "1-hour": 60,
  "1-day": 1440,
  "2-days": 2880,
  "1-week": 10080,
};

/** The presets offered for `allDay`, in display order. */
export function reminderPresets(allDay: boolean): ReminderPreset[] {
  return allDay ? ALL_DAY_PRESETS : TIMED_PRESETS;
}

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

/** The `offsetMinutes` a preset stores, regardless of `allDay` — only the
 * available preset set and labels differ by `allDay`, not the encoding. */
export function presetOffsetMinutes(preset: ReminderPreset): number {
  return PRESET_OFFSET_MINUTES[preset];
}

/** The human label for a preset, naming the 09:00 anchor for all-day Events. */
export function reminderPresetLabel(preset: ReminderPreset, allDay: boolean): string {
  if (allDay) {
    switch (preset) {
      case "at-start":
        return "On the day, 9:00 AM";
      case "1-day":
        return "1 day before, 9:00 AM";
      case "2-days":
        return "2 days before, 9:00 AM";
      case "1-week":
        return "1 week before, 9:00 AM";
      default:
        return reminderPresetLabel(preset, false);
    }
  }
  switch (preset) {
    case "at-start":
      return "At time of event";
    case "5-minutes":
      return "5 minutes before";
    case "10-minutes":
      return "10 minutes before";
    case "15-minutes":
      return "15 minutes before";
    case "30-minutes":
      return "30 minutes before";
    case "1-hour":
      return "1 hour before";
    case "1-day":
      return "1 day before";
    case "2-days":
      return "2 days before";
    case "1-week":
      return "1 week before";
  }
}

/** The preset whose offset exactly equals `offsetMinutes` among the set
 * offered for `allDay`, or null when none matches — i.e. a custom offset. */
export function matchPreset(offsetMinutes: number, allDay: boolean): ReminderPreset | null {
  for (const preset of reminderPresets(allDay)) {
    if (PRESET_OFFSET_MINUTES[preset] === offsetMinutes) return preset;
  }
  return null;
}

/** A custom offset's input unit — minutes and up, matching the Custom
 * recurrence dialog's day/week/month/year interval+unit pattern. */
export type ReminderOffsetUnit = "minutes" | "hours" | "days" | "weeks";

const UNIT_MINUTES: Record<ReminderOffsetUnit, number> = {
  minutes: 1,
  hours: 60,
  days: 1440,
  weeks: 10080,
};

/** Normalizes a custom offset entry to minutes before the anchor. Negative or
 * fractional amounts clamp to a whole, non-negative number of minutes. */
export function normalizeCustomOffset(amount: number, unit: ReminderOffsetUnit): number {
  return Math.max(0, Math.round(amount)) * UNIT_MINUTES[unit];
}

/** Splits `offsetMinutes` back into an amount + the largest unit that divides
 * it evenly, so a custom offset's fields display naturally (e.g. 2880 as "2
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
