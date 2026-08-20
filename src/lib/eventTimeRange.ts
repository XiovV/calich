import { isSameDay, setHours, setMinutes, startOfDay } from "date-fns";

/** `day` at the wall-clock `HH:mm` in `time`. */
export function timeStringToDate(day: Date, time: string): Date {
  const [hours, minutes] = time.split(":").map(Number);
  return setMinutes(setHours(day, hours), minutes);
}

/** `day` as a native `<input type="date">` value (always `yyyy-MM-dd`,
 * regardless of locale). */
export function dateToDateInputValue(day: Date): string {
  const year = day.getFullYear();
  const month = String(day.getMonth() + 1).padStart(2, "0");
  const date = String(day.getDate()).padStart(2, "0");
  return `${year}-${month}-${date}`;
}

/** The inverse of `dateToDateInputValue`, parsed as local wall-clock
 * midnight rather than `new Date(value)`'s UTC midnight — the latter shifts
 * a day backward for any viewer west of UTC. */
export function dateInputValueToDate(value: string): Date {
  const [year, month, date] = value.split("-").map(Number);
  return new Date(year, month - 1, date);
}

export interface TimeRangeValidation {
  valid: boolean;
  /** Names what's wrong, or null when `valid`. Two distinct messages
   * (issue #231): an end date before the start date is a date problem and
   * is named as one, never described as a time issue. */
  message: string | null;
}

const END_DATE_BEFORE_START = "End date must be on or after the start date.";
const END_TIME_BEFORE_START = "End time must be after start time.";

/**
 * Validates a timed Event's Start/End date + time fields (issue #231). An
 * end date earlier than the start date is refused outright and named by
 * date. An end date equal to the start date still needs its end time after
 * the start time — the same-day rule #230 left in place. Any later end date
 * is always valid regardless of clock times: the date already establishes
 * the order, so a night shift's 23:00 -> 01:00 the next day is a real
 * two-hour Event, not an invalid one.
 */
export function validateEventTimeRange(
  startDay: Date,
  startTime: string,
  endDay: Date,
  endTime: string,
): TimeRangeValidation {
  if (startOfDay(endDay).getTime() < startOfDay(startDay).getTime()) {
    return { valid: false, message: END_DATE_BEFORE_START };
  }

  if (isSameDay(endDay, startDay)) {
    const start = timeStringToDate(startDay, startTime);
    const end = timeStringToDate(endDay, endTime);
    if (end <= start) {
      return { valid: false, message: END_TIME_BEFORE_START };
    }
  }

  return { valid: true, message: null };
}
