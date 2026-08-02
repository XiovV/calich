import { describe, expect, it } from "vitest";
import { computeResizedEventTimes } from "./gridTime";

describe("computeResizedEventTimes", () => {
  it("returns unchanged times when there is no offset", () => {
    const start = new Date(2026, 7, 3, 9, 0);
    const end = new Date(2026, 7, 3, 10, 0);
    const result = computeResizedEventTimes(start, end, "start", 0);
    expect(result).toEqual({ start, end });
  });

  it("resizes the start edge, keeping end fixed", () => {
    const start = new Date(2026, 7, 3, 9, 0);
    const end = new Date(2026, 7, 3, 10, 0);
    const result = computeResizedEventTimes(start, end, "start", 30);
    expect(result).toEqual({
      start: new Date(2026, 7, 3, 9, 30),
      end,
    });
  });

  it("resizes the end edge, keeping start fixed", () => {
    const start = new Date(2026, 7, 3, 9, 0);
    const end = new Date(2026, 7, 3, 10, 0);
    const result = computeResizedEventTimes(start, end, "end", 30);
    expect(result).toEqual({
      start,
      end: new Date(2026, 7, 3, 10, 30),
    });
  });

  it("snaps the resize offset to the nearest 15-minute increment", () => {
    const start = new Date(2026, 7, 3, 9, 0);
    const end = new Date(2026, 7, 3, 10, 0);
    const result = computeResizedEventTimes(start, end, "end", 22);
    expect(result).toEqual({
      start,
      end: new Date(2026, 7, 3, 10, 15),
    });
  });

  it("clamps the start edge so it can't cross past the minimum duration before end", () => {
    const start = new Date(2026, 7, 3, 9, 0);
    const end = new Date(2026, 7, 3, 10, 0);
    // Dragging start forward by 55 minutes would leave only 5 minutes before end.
    const result = computeResizedEventTimes(start, end, "start", 55);
    expect(result).toEqual({
      start: new Date(2026, 7, 3, 9, 45),
      end,
    });
  });

  it("clamps the end edge so it can't cross past the minimum duration after start", () => {
    const start = new Date(2026, 7, 3, 9, 0);
    const end = new Date(2026, 7, 3, 10, 0);
    // Dragging end backward by 55 minutes would leave only 5 minutes after start.
    const result = computeResizedEventTimes(start, end, "end", -55);
    expect(result).toEqual({
      start,
      end: new Date(2026, 7, 3, 9, 15),
    });
  });
});
