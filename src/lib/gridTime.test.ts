import { describe, expect, it } from "vitest";
import { durationToHeight, timeToY, yToTime } from "./gridTime";

describe("timeToY", () => {
  it("returns 0 at midnight", () => {
    const result = timeToY(new Date(2026, 7, 3, 0, 0), 60);
    expect(result).toBe(0);
  });

  it("scales by pixelsPerHour for a whole-hour time", () => {
    const result = timeToY(new Date(2026, 7, 3, 3, 0), 60);
    expect(result).toBe(180);
  });

  it("accounts for minutes within the hour", () => {
    const result = timeToY(new Date(2026, 7, 3, 3, 30), 60);
    expect(result).toBe(210);
  });
});

describe("durationToHeight", () => {
  it("computes height for a one-hour event", () => {
    const start = new Date(2026, 7, 3, 9, 0);
    const end = new Date(2026, 7, 3, 10, 0);
    expect(durationToHeight(start, end, 60)).toBe(60);
  });

  it("computes height for a 90-minute event", () => {
    const start = new Date(2026, 7, 3, 9, 0);
    const end = new Date(2026, 7, 3, 10, 30);
    expect(durationToHeight(start, end, 60)).toBe(90);
  });
});

describe("yToTime", () => {
  it("returns midnight of the reference day at y=0", () => {
    const referenceDay = new Date(2026, 7, 3);
    const result = yToTime(0, referenceDay, 60);
    expect(result).toEqual(new Date(2026, 7, 3, 0, 0));
  });

  it("converts a pixel offset back into hours and minutes", () => {
    const referenceDay = new Date(2026, 7, 3);
    const result = yToTime(210, referenceDay, 60);
    expect(result).toEqual(new Date(2026, 7, 3, 3, 30));
  });
});
