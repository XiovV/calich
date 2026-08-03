// Shared building blocks for the small subset of iCalendar RRULE this app
// authors (ADR-0016). Weekdays are indexed by JS `Date.getDay()` (0 = Sunday)
// throughout — deliberately NOT rrule.js's Monday-based `Weekday.weekday` — so
// weekday values round-trip through our own build/parse without conversion.

/** RRULE two-letter weekday codes indexed by `Date.getDay()` (0 = Sunday). */
export const RRULE_WEEKDAYS = ["SU", "MO", "TU", "WE", "TH", "FR", "SA"] as const;

/** Short weekday display names indexed by `Date.getDay()` (0 = Sunday). */
export const WEEKDAY_SHORT_NAMES = [
  "Sun",
  "Mon",
  "Tue",
  "Wed",
  "Thu",
  "Fri",
  "Sat",
] as const;

/** Ordinal words for the nth weekday of a month (index 0 = "first"). */
export const ORDINALS = ["first", "second", "third", "fourth", "fifth"] as const;

/** The RRULE weekday code for a date's weekday (e.g. a Tuesday → "TU"). */
export function weekdayCode(date: Date): string {
  return RRULE_WEEKDAYS[date.getDay()];
}

/** The `Date.getDay()` index for an RRULE weekday code, or -1 if unknown. */
export function weekdayIndex(code: string): number {
  return RRULE_WEEKDAYS.indexOf(code as (typeof RRULE_WEEKDAYS)[number]);
}

/** 1-based position of `date`'s weekday within its month (e.g. the 3rd Tuesday). */
export function nthWeekdayOfMonth(date: Date): number {
  return Math.floor((date.getDate() - 1) / 7) + 1;
}
