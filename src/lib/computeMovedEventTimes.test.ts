import { describe, expect, it } from "vitest";
import { computeMovedEventTimes } from "./gridTime";

describe("computeMovedEventTimes", () => {
  it("returns unchanged times when there is no offset", () => {
    const start = new Date(2026, 7, 3, 9, 0);
    const end = new Date(2026, 7, 3, 10, 0);
    const result = computeMovedEventTimes(start, end, 0, 0);
    expect(result).toEqual({ start, end });
  });

  it("shifts both start and end by a same-day minute offset, preserving duration", () => {
    const start = new Date(2026, 7, 3, 9, 0);
    const end = new Date(2026, 7, 3, 10, 0);
    const result = computeMovedEventTimes(start, end, 0, 30);
    expect(result).toEqual({
      start: new Date(2026, 7, 3, 9, 30),
      end: new Date(2026, 7, 3, 10, 30),
    });
  });

  it("shifts the day when dragged into a different day column", () => {
    const start = new Date(2026, 7, 3, 9, 0);
    const end = new Date(2026, 7, 3, 10, 0);
    const result = computeMovedEventTimes(start, end, 2, 0);
    expect(result).toEqual({
      start: new Date(2026, 7, 5, 9, 0),
      end: new Date(2026, 7, 5, 10, 0),
    });
  });

  it("snaps the minute offset to the nearest 15-minute increment", () => {
    const start = new Date(2026, 7, 3, 9, 0);
    const end = new Date(2026, 7, 3, 10, 0);
    const result = computeMovedEventTimes(start, end, 0, 22);
    expect(result).toEqual({
      start: new Date(2026, 7, 3, 9, 15),
      end: new Date(2026, 7, 3, 10, 15),
    });
  });

  it("supports negative offsets (dragging backward in time or day)", () => {
    const start = new Date(2026, 7, 3, 9, 0);
    const end = new Date(2026, 7, 3, 10, 0);
    const result = computeMovedEventTimes(start, end, -1, -30);
    expect(result).toEqual({
      start: new Date(2026, 7, 2, 8, 30),
      end: new Date(2026, 7, 2, 9, 30),
    });
  });
});
