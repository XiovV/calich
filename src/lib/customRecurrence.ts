import { RRule } from "rrule";
import { toFloating, viewerZone } from "./floatingTime";
import {
  ORDINALS,
  RRULE_WEEKDAYS,
  WEEKDAY_SHORT_NAMES,
  nthWeekdayOfMonth,
  weekdayCode,
  weekdayIndex,
} from "./rruleParts";

// The fields behind the Custom recurrence dialog (Google-style), and pure
// build/parse between them and an iCalendar RRULE (ADR-0016). Everything is
// anchored by the Event's start: monthly/yearly derive their day from it, and a
// weekly rule defaults to the start's weekday when no chip is selected.

export type RecurrenceUnit = "day" | "week" | "month" | "year";

/** Monthly: repeat on the same day-of-month, or on the same nth weekday. */
export type MonthlyMode = "day" | "weekday";

export type CustomEnd =
  | { type: "never" }
  | { type: "on"; date: Date } // local calendar date (time ignored)
  | { type: "after"; count: number };

export interface CustomRecurrence {
  /** "Repeat every N". Always >= 1. */
  interval: number;
  unit: RecurrenceUnit;
  /** Weekday chips for the weekly unit, as `Date.getDay()` indices (0 = Sunday). */
  weekdays: number[];
  /** Which monthly rule to build; only meaningful when `unit` is "month". */
  monthlyMode: MonthlyMode;
  end: CustomEnd;
}

const FREQ_BY_UNIT: Record<RecurrenceUnit, string> = {
  day: "DAILY",
  week: "WEEKLY",
  month: "MONTHLY",
  year: "YEARLY",
};

const UNIT_BY_FREQ: Record<string, RecurrenceUnit> = {
  DAILY: "day",
  WEEKLY: "week",
  MONTHLY: "month",
  YEARLY: "year",
};

/** The dialog's starting fields for a not-yet-recurring Event. */
export function defaultCustomRecurrence(start: Date): CustomRecurrence {
  return {
    interval: 1,
    unit: "week",
    weekdays: [start.getDay()],
    monthlyMode: "weekday",
    end: { type: "never" },
  };
}

/** The weekly BYDAY set: the selected chips, or the start's weekday if none. */
function weeklyDays(weekdays: number[], start: Date): number[] {
  const days = weekdays.length > 0 ? weekdays : [start.getDay()];
  return [...new Set(days)].sort((a, b) => a - b);
}

/** The FREQ/INTERVAL/BY… portion of the rule, without any end condition. */
function baseRuleParts(fields: CustomRecurrence, start: Date): string[] {
  const parts = [`FREQ=${FREQ_BY_UNIT[fields.unit]}`];
  if (fields.interval > 1) parts.push(`INTERVAL=${fields.interval}`);

  if (fields.unit === "week") {
    const codes = weeklyDays(fields.weekdays, start).map((d) => RRULE_WEEKDAYS[d]);
    parts.push(`BYDAY=${codes.join(",")}`);
  } else if (fields.unit === "month") {
    if (fields.monthlyMode === "day") {
      parts.push(`BYMONTHDAY=${start.getDate()}`);
    } else {
      // e.g. the 5th Tuesday → BYDAY=+5TU; months without a 5th are simply
      // skipped by expansion (no special handling needed).
      parts.push(`BYDAY=+${nthWeekdayOfMonth(start)}${weekdayCode(start)}`);
    }
  }
  // "day" and "year" carry no BY… part — they are anchored purely by the start.

  return parts;
}

/** An RRULE `UNTIL` value as end-of-day in the floating frame: `YYYYMMDDT235959Z`. */
function untilFromYMD(year: number, month0: number, day: number): string {
  const mm = String(month0 + 1).padStart(2, "0");
  const dd = String(day).padStart(2, "0");
  return `${year}${mm}${dd}T235959Z`;
}

/** The floating-frame start of the `count`-th Occurrence, or null if none.
 * `start` is a wall-clock the dialog picked, not yet a stored Event's UTC
 * instant, so it has no real Anchor zone to convert from — it's read
 * directly as the Viewer zone's wall-clock, same as every other field this
 * not-yet-saved dialog works with (ADR-0019). */
function nthOccurrence(
  fields: CustomRecurrence,
  start: Date,
  count: number,
): Date | null {
  const rule = new RRule({
    ...RRule.parseString(baseRuleParts(fields, start).join(";")),
    dtstart: toFloating(start, viewerZone()),
    count: Math.max(1, count),
  });
  const all = rule.all();
  return all.length > 0 ? all[all.length - 1] : null;
}

/**
 * Builds an RRULE from the dialog fields, anchored at `start`. "After N
 * occurrences" is normalized to an `UNTIL` end-date — the date of the Nth
 * Occurrence — so later series splits never do count arithmetic (ADR-0016).
 * Both "On {date}" and "After N" produce an end-of-day `UNTIL`, which keeps
 * build → parse → build stable.
 */
export function buildCustomRule(fields: CustomRecurrence, start: Date): string {
  const parts = baseRuleParts(fields, start);

  if (fields.end.type === "on") {
    const { date } = fields.end;
    parts.push(
      `UNTIL=${untilFromYMD(date.getFullYear(), date.getMonth(), date.getDate())}`,
    );
  } else if (fields.end.type === "after") {
    const nth = nthOccurrence(fields, start, fields.end.count);
    if (nth) {
      parts.push(
        `UNTIL=${untilFromYMD(nth.getUTCFullYear(), nth.getUTCMonth(), nth.getUTCDate())}`,
      );
    }
  }

  return parts.join(";");
}

function parseParts(rrule: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const segment of rrule.split(";")) {
    const [key, value] = segment.split("=");
    if (key && value !== undefined) out[key.toUpperCase()] = value;
  }
  return out;
}

/** A local calendar date from an RRULE `UNTIL` value (its time is ignored). */
function dateFromUntil(until: string): Date {
  const year = Number(until.slice(0, 4));
  const month = Number(until.slice(4, 6));
  const day = Number(until.slice(6, 8));
  return new Date(year, month - 1, day);
}

function parseEnd(parts: Record<string, string>): CustomEnd {
  if (parts.UNTIL) return { type: "on", date: dateFromUntil(parts.UNTIL) };
  if (parts.COUNT) {
    return { type: "after", count: Math.max(1, Number(parts.COUNT)) };
  }
  return { type: "never" };
}

/**
 * Parses a stored RRULE back into dialog fields so editing re-opens the dialog
 * with everything set. `start` supplies the defaults the rule leaves implicit
 * (e.g. the weekday of a rule with no BYDAY).
 */
export function parseCustomRule(rrule: string, start: Date): CustomRecurrence {
  const parts = parseParts(rrule);
  const unit = UNIT_BY_FREQ[parts.FREQ] ?? "week";
  const interval = parts.INTERVAL ? Math.max(1, Number(parts.INTERVAL)) : 1;

  let weekdays: number[] = [];
  let monthlyMode: MonthlyMode = "weekday";

  if (unit === "week") {
    const parsed = parts.BYDAY
      ? parts.BYDAY.split(",")
          .map(weekdayIndex)
          .filter((index) => index >= 0)
      : [];
    weekdays = parsed.length > 0 ? parsed : [start.getDay()];
  } else if (unit === "month") {
    monthlyMode = parts.BYMONTHDAY ? "day" : "weekday";
  }

  return { interval, unit, weekdays, monthlyMode, end: parseEnd(parts) };
}

/** A short human summary of a custom rule for the Repeat dropdown label. */
export function summarizeCustomRule(rrule: string, start: Date): string {
  const fields = parseCustomRule(rrule, start);
  const { interval, unit } = fields;
  const every =
    interval > 1
      ? `Every ${interval} ${unit}s`
      : { day: "Daily", week: "Weekly", month: "Monthly", year: "Annually" }[unit];

  let detail = "";
  if (unit === "week") {
    const names = weeklyDays(fields.weekdays, start).map(
      (d) => WEEKDAY_SHORT_NAMES[d],
    );
    detail = ` on ${names.join(", ")}`;
  } else if (unit === "month") {
    detail =
      fields.monthlyMode === "day"
        ? ` on day ${start.getDate()}`
        : ` on the ${ORDINALS[nthWeekdayOfMonth(start) - 1]} ${WEEKDAY_SHORT_NAMES[start.getDay()]}`;
  }

  let ending = "";
  if (fields.end.type === "on") {
    ending = ` until ${fields.end.date.toLocaleDateString()}`;
  } else if (fields.end.type === "after") {
    ending = `, ${fields.end.count} times`;
  }

  return `${every}${detail}${ending}`;
}
