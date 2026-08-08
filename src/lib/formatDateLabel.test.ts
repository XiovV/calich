import { describe, expect, it } from "vitest";
import { formatDateLabel } from "./formatDateLabel";

describe("formatDateLabel", () => {
  it("formats a day-view date as a full date", () => {
    const result = formatDateLabel(new Date(2026, 7, 2), "day", 0);
    expect(result).toBe("August 2, 2026");
  });

  it("formats a month-view date as month and year", () => {
    const result = formatDateLabel(new Date(2026, 7, 2), "month", 0);
    expect(result).toBe("August 2026");
  });

  it("formats a year-view date as the year alone", () => {
    const result = formatDateLabel(new Date(2026, 7, 2), "year", 0);
    expect(result).toBe("2026");
  });

  it("formats a week-view date as a Sunday-start range within one month", () => {
    // Aug 2, 2026 is a Sunday; the week runs Aug 2 - Aug 8.
    const result = formatDateLabel(new Date(2026, 7, 2), "week", 0);
    expect(result).toBe("Aug 2 – 8, 2026");
  });

  it("formats a week-view range that spans two months", () => {
    // Jul 30, 2026 is a Thursday; the Sunday-start week runs Jul 26 - Aug 1.
    const result = formatDateLabel(new Date(2026, 6, 30), "week", 0);
    expect(result).toBe("Jul 26 – Aug 1, 2026");
  });

  it("respects a Monday weekStartsOn for the week-view range", () => {
    // Aug 2, 2026 is a Sunday; a Monday-start week runs Jul 27 - Aug 2.
    const result = formatDateLabel(new Date(2026, 7, 2), "week", 1);
    expect(result).toBe("Jul 27 – Aug 2, 2026");
  });
});
