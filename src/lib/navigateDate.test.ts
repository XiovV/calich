import { describe, expect, it } from "vitest";
import { navigateDate } from "./navigateDate";

describe("navigateDate", () => {
  it("shifts forward by one day in day view", () => {
    const result = navigateDate(new Date(2026, 7, 2), "day", "next");
    expect(result).toEqual(new Date(2026, 7, 3));
  });

  it("shifts backward by one day in day view", () => {
    const result = navigateDate(new Date(2026, 7, 2), "day", "prev");
    expect(result).toEqual(new Date(2026, 7, 1));
  });

  it("shifts forward by one week in week view", () => {
    const result = navigateDate(new Date(2026, 7, 2), "week", "next");
    expect(result).toEqual(new Date(2026, 7, 9));
  });

  it("shifts backward by one week in week view", () => {
    const result = navigateDate(new Date(2026, 7, 2), "week", "prev");
    expect(result).toEqual(new Date(2026, 6, 26));
  });

  it("shifts forward by one month in month view", () => {
    const result = navigateDate(new Date(2026, 7, 2), "month", "next");
    expect(result).toEqual(new Date(2026, 8, 2));
  });

  it("shifts backward by one month in month view", () => {
    const result = navigateDate(new Date(2026, 7, 2), "month", "prev");
    expect(result).toEqual(new Date(2026, 6, 2));
  });

  it("shifts forward by one year in year view", () => {
    const result = navigateDate(new Date(2026, 7, 2), "year", "next");
    expect(result).toEqual(new Date(2027, 7, 2));
  });

  it("shifts backward by one year in year view", () => {
    const result = navigateDate(new Date(2026, 7, 2), "year", "prev");
    expect(result).toEqual(new Date(2025, 7, 2));
  });
});
