import { describe, expect, it } from "vitest";
import {
  dateInputValueToDate,
  dateToDateInputValue,
  timeStringToDate,
  validateEventTimeRange,
} from "./eventTimeRange";

describe("timeStringToDate", () => {
  it("combines the day with the HH:mm time", () => {
    const day = new Date(2026, 7, 20);
    expect(timeStringToDate(day, "23:15")).toEqual(new Date(2026, 7, 20, 23, 15));
  });
});

describe("dateToDateInputValue", () => {
  it("formats as yyyy-MM-dd", () => {
    expect(dateToDateInputValue(new Date(2026, 7, 3))).toBe("2026-08-03");
  });

  it("zero-pads a single-digit month and day", () => {
    expect(dateToDateInputValue(new Date(2026, 0, 5))).toBe("2026-01-05");
  });
});

describe("dateInputValueToDate", () => {
  it("parses as local wall-clock midnight, round-tripping dateToDateInputValue", () => {
    const day = new Date(2026, 7, 3);
    expect(dateInputValueToDate(dateToDateInputValue(day))).toEqual(day);
  });

  it("never shifts a day backward the way new Date(value) would west of UTC", () => {
    const result = dateInputValueToDate("2026-01-01");
    expect(result.getFullYear()).toBe(2026);
    expect(result.getMonth()).toBe(0);
    expect(result.getDate()).toBe(1);
  });
});

describe("validateEventTimeRange", () => {
  const day = new Date(2026, 7, 20);
  const nextDay = new Date(2026, 7, 21);
  const prevDay = new Date(2026, 7, 19);

  it("is valid when end time is after start time on the same date", () => {
    const result = validateEventTimeRange(day, "09:00", day, "10:00");
    expect(result).toEqual({ valid: true, message: null });
  });

  it("refuses end time equal to start time on the same date", () => {
    const result = validateEventTimeRange(day, "09:00", day, "09:00");
    expect(result.valid).toBe(false);
    expect(result.message).toBe("End time must be after start time.");
  });

  it("refuses end time before start time on the same date", () => {
    const result = validateEventTimeRange(day, "10:00", day, "09:00");
    expect(result.valid).toBe(false);
    expect(result.message).toBe("End time must be after start time.");
  });

  it("is valid for an end date after the start date, regardless of clock times", () => {
    // 23:00 -> 01:00 the next day: end time is numerically "before" start
    // time, but the later date makes it a real two-hour Event.
    const result = validateEventTimeRange(day, "23:00", nextDay, "01:00");
    expect(result).toEqual({ valid: true, message: null });
  });

  it("is valid for an end date well after the start date", () => {
    const threeDaysLater = new Date(2026, 7, 23);
    const result = validateEventTimeRange(day, "23:00", threeDaysLater, "01:00");
    expect(result).toEqual({ valid: true, message: null });
  });

  it("refuses an end date before the start date, naming the date not the time", () => {
    const result = validateEventTimeRange(day, "09:00", prevDay, "23:00");
    expect(result.valid).toBe(false);
    expect(result.message).toBe("End date must be on or after the start date.");
  });
});
