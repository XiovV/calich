import { format } from "date-fns";

/**
 * The fixed set of recurrence presets offered in the event modal's Repeat
 * dropdown. Each maps to a small subset of iCalendar RRULE (ADR-0016); the
 * stored rule is anchored by the Event's start (its DTSTART), so the rule string
 * itself carries only FREQ/BYDAY and derives month/day from the start.
 */
export type RecurrencePreset =
  | "none"
  | "daily"
  | "weekly"
  | "monthly"
  | "yearly"
  | "weekday";

export const RECURRENCE_PRESETS: RecurrencePreset[] = [
  "none",
  "daily",
  "weekly",
  "monthly",
  "yearly",
  "weekday",
];

// RRULE two-letter weekday codes indexed by JS Date.getDay() (0 = Sunday).
const RRULE_WEEKDAYS = ["SU", "MO", "TU", "WE", "TH", "FR", "SA"] as const;
const WEEKDAY_MON_TO_FRI = "MO,TU,WE,TH,FR";
const ORDINALS = ["first", "second", "third", "fourth", "fifth"] as const;

/** 1-based position of `date`'s weekday within its month (e.g. the 3rd Tuesday). */
function nthWeekdayOfMonth(date: Date): number {
  return Math.floor((date.getDate() - 1) / 7) + 1;
}

/**
 * The RRULE for a preset, anchored at `start`, or undefined for "Does not
 * repeat". Weekly/monthly presets read the weekday (and nth-weekday) from the
 * start date so changing the start changes the rule.
 */
export function buildRule(
  preset: RecurrencePreset,
  start: Date,
): string | undefined {
  const weekday = RRULE_WEEKDAYS[start.getDay()];
  switch (preset) {
    case "none":
      return undefined;
    case "daily":
      return "FREQ=DAILY";
    case "weekly":
      return `FREQ=WEEKLY;BYDAY=${weekday}`;
    case "monthly":
      return `FREQ=MONTHLY;BYDAY=+${nthWeekdayOfMonth(start)}${weekday}`;
    case "yearly":
      return "FREQ=YEARLY";
    case "weekday":
      // Unlike the self-deriving presets, this one is fixed to Mon–Fri and does
      // not read the start's weekday. A Mon–Fri series therefore has no weekend
      // instance: if anchored on a Saturday or Sunday it produces its first
      // Occurrence on the following Monday, not on the start date itself.
      return `FREQ=WEEKLY;BYDAY=${WEEKDAY_MON_TO_FRI}`;
  }
}

/**
 * The human label for a preset, regenerated from `start` — e.g. moving the start
 * from a Tuesday to a Wednesday turns "Weekly on Tuesday" into "Weekly on
 * Wednesday".
 */
export function presetLabel(preset: RecurrencePreset, start: Date): string {
  switch (preset) {
    case "none":
      return "Does not repeat";
    case "daily":
      return "Daily";
    case "weekly":
      return `Weekly on ${format(start, "EEEE")}`;
    case "monthly": {
      const ordinal = ORDINALS[nthWeekdayOfMonth(start) - 1];
      return `Monthly on the ${ordinal} ${format(start, "EEEE")}`;
    }
    case "yearly":
      return `Annually on ${format(start, "MMMM d")}`;
    case "weekday":
      return "Every weekday (Monday to Friday)";
  }
}

/**
 * Recovers the preset that produced an RRULE, so editing an existing Event
 * pre-selects the right dropdown option. Recognises only the presets this module
 * builds; anything else (or an absent rule) reads as "none".
 */
export function presetFromRule(rrule: string | undefined): RecurrencePreset {
  if (!rrule) return "none";
  if (rrule.includes("FREQ=DAILY")) return "daily";
  if (rrule.includes("FREQ=YEARLY")) return "yearly";
  if (rrule.includes("FREQ=MONTHLY")) return "monthly";
  if (rrule.includes("FREQ=WEEKLY")) {
    return rrule.includes(WEEKDAY_MON_TO_FRI) ? "weekday" : "weekly";
  }
  return "none";
}

/**
 * Re-anchors an existing rule to a new start date, keeping the same preset. Used
 * when a recurring Occurrence is dragged: because the interim behaviour edits the
 * whole series (ADR-0016), the series follows its new anchor — dragging a
 * "Weekly on Tuesday" event to a Wednesday makes it "Weekly on Wednesday" —
 * instead of leaving the rule's weekday out of sync with the master's start.
 * Returns undefined for a non-recurring event.
 */
export function reanchorRule(
  rrule: string | undefined,
  start: Date,
): string | undefined {
  if (!rrule) return undefined;
  return buildRule(presetFromRule(rrule), start);
}
