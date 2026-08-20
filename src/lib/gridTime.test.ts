import { describe, expect, it } from "vitest";
import {
  computeMoveToDate,
  durationToHeight,
  resolveDragTargetDate,
  timeToY,
  yToTime,
} from "./gridTime";

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

describe("computeMoveToDate", () => {
  it("changes the date to the target date", () => {
    const start = new Date(2026, 7, 10, 9, 0);
    const end = new Date(2026, 7, 10, 10, 0);
    const target = new Date(2026, 7, 15);

    const result = computeMoveToDate(start, end, target);

    expect(result.start).toEqual(new Date(2026, 7, 15, 9, 0));
  });

  it("preserves the time-of-day", () => {
    const start = new Date(2026, 7, 10, 14, 30);
    const end = new Date(2026, 7, 10, 15, 0);
    const target = new Date(2026, 8, 2);

    const result = computeMoveToDate(start, end, target);

    expect(result.start).toEqual(new Date(2026, 8, 2, 14, 30));
  });

  it("preserves the duration", () => {
    const start = new Date(2026, 7, 10, 9, 0);
    const end = new Date(2026, 7, 10, 11, 30);
    const target = new Date(2026, 7, 20);

    const result = computeMoveToDate(start, end, target);

    expect(result.end.getTime() - result.start.getTime()).toBe(
      end.getTime() - start.getTime(),
    );
  });

  it("moves across a month boundary", () => {
    const start = new Date(2026, 6, 31, 8, 0);
    const end = new Date(2026, 6, 31, 9, 0);
    const target = new Date(2026, 7, 1);

    const result = computeMoveToDate(start, end, target);

    expect(result.start).toEqual(new Date(2026, 7, 1, 8, 0));
    expect(result.end).toEqual(new Date(2026, 7, 1, 9, 0));
  });
});

describe("resolveDragTargetDate", () => {
  it("returns the drop target unchanged when the drag started on the Occurrence's own start day", () => {
    const occurrenceStart = new Date(2026, 7, 14);
    const dragStartDay = new Date(2026, 7, 14);
    const targetDate = new Date(2026, 7, 20);

    expect(resolveDragTargetDate(occurrenceStart, dragStartDay, targetDate)).toEqual(
      new Date(2026, 7, 20),
    );
  });

  it("shifts the drop target back by the gap between the picked-up day and the true start (#232)", () => {
    // A 3-day all-day Occurrence spans Aug 14-17 (half-open). Dragging the
    // chip shown on its third day (the 16th) to the 20th should move the
    // Occurrence by the same +4 days the user visually dragged it (16 -> 20),
    // landing the true start on the 18th — not snap the true start straight
    // to the drop point.
    const occurrenceStart = new Date(2026, 7, 14);
    const dragStartDay = new Date(2026, 7, 16);
    const targetDate = new Date(2026, 7, 20);

    expect(resolveDragTargetDate(occurrenceStart, dragStartDay, targetDate)).toEqual(
      new Date(2026, 7, 18),
    );
  });

  it("composes with computeMoveToDate to move the whole span by the dragged distance", () => {
    const occurrence = { start: new Date(2026, 7, 14), end: new Date(2026, 7, 17) };
    const dragStartDay = new Date(2026, 7, 16);
    const targetDate = new Date(2026, 7, 20);

    const adjustedTarget = resolveDragTargetDate(occurrence.start, dragStartDay, targetDate);
    const result = computeMoveToDate(occurrence.start, occurrence.end, adjustedTarget);

    expect(result.start).toEqual(new Date(2026, 7, 18));
    expect(result.end).toEqual(new Date(2026, 7, 21));
  });
});
