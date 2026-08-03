import { describe, expect, it } from "vitest";
import { buildDaysWithEvents, buildYearMonths, dayKey } from "./yearGrid";
import type { Event } from "./event";

describe("buildYearMonths", () => {
  it("returns twelve months for the selected year", () => {
    const months = buildYearMonths(new Date(2026, 7, 15));
    expect(months).toHaveLength(12);
  });

  it("returns January through December, each on the first of the month", () => {
    const months = buildYearMonths(new Date(2026, 7, 15));
    months.forEach((month, index) => {
      expect(month.getFullYear()).toBe(2026);
      expect(month.getMonth()).toBe(index);
      expect(month.getDate()).toBe(1);
    });
  });

  it("uses the selected date's year, not the month or day", () => {
    const months = buildYearMonths(new Date(2025, 11, 31));
    expect(months[0]).toEqual(new Date(2025, 0, 1));
    expect(months[11]).toEqual(new Date(2025, 11, 1));
  });
});

describe("dayKey", () => {
  it("formats a date as yyyy-MM-dd regardless of time of day", () => {
    expect(dayKey(new Date(2026, 7, 3, 23, 59))).toBe("2026-08-03");
    expect(dayKey(new Date(2026, 7, 3, 0, 0))).toBe("2026-08-03");
  });
});

describe("buildDaysWithEvents", () => {
  function makeEvent(id: string, calendarId: string, start: Date): Event {
    return {
      id,
      calendarId,
      title: id,
      start,
      end: new Date(start.getTime() + 60 * 60 * 1000),
    };
  }

  it("includes days that have an Event on a checked Calendar", () => {
    const events = [
      makeEvent("a", "cal-1", new Date(2026, 7, 10, 9, 0)),
      makeEvent("b", "cal-1", new Date(2026, 7, 12, 14, 0)),
    ];

    const result = buildDaysWithEvents(events, new Set(["cal-1"]));

    expect(result).toEqual(new Set(["2026-08-10", "2026-08-12"]));
  });

  it("excludes days whose only Events are on unchecked Calendars", () => {
    const events = [
      makeEvent("checked", "cal-1", new Date(2026, 7, 10, 9, 0)),
      makeEvent("unchecked", "cal-2", new Date(2026, 7, 20, 9, 0)),
    ];

    const result = buildDaysWithEvents(events, new Set(["cal-1"]));

    expect(result.has("2026-08-10")).toBe(true);
    expect(result.has("2026-08-20")).toBe(false);
  });

  it("still marks a day with at least one checked Event when another Event there is unchecked", () => {
    const events = [
      makeEvent("checked", "cal-1", new Date(2026, 7, 10, 9, 0)),
      makeEvent("unchecked", "cal-2", new Date(2026, 7, 10, 15, 0)),
    ];

    const result = buildDaysWithEvents(events, new Set(["cal-1"]));

    expect(result).toEqual(new Set(["2026-08-10"]));
  });

  it("collapses multiple Events on the same day to a single key", () => {
    const events = [
      makeEvent("morning", "cal-1", new Date(2026, 7, 10, 9, 0)),
      makeEvent("afternoon", "cal-1", new Date(2026, 7, 10, 14, 0)),
    ];

    const result = buildDaysWithEvents(events, new Set(["cal-1"]));

    expect(result.size).toBe(1);
    expect(result).toEqual(new Set(["2026-08-10"]));
  });

  it("returns an empty set when no Calendars are checked", () => {
    const events = [makeEvent("a", "cal-1", new Date(2026, 7, 10, 9, 0))];

    const result = buildDaysWithEvents(events, new Set());

    expect(result.size).toBe(0);
  });
});
