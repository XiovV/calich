import { describe, expect, it } from "vitest";
import {
  buildMonthGrid,
  computeCellDraft,
  computeChipCapacity,
  computeMoveToDate,
  getOccurrencesForDay,
} from "./monthGrid";
import type { Occurrence } from "./occurrence";

describe("buildMonthGrid", () => {
  it("returns 42 cells", () => {
    const grid = buildMonthGrid(new Date(2026, 7, 15));
    expect(grid).toHaveLength(42);
  });

  it("starts every row on a Sunday", () => {
    const grid = buildMonthGrid(new Date(2026, 7, 15));
    for (let row = 0; row < 6; row++) {
      expect(grid[row * 7].date.getDay()).toBe(0);
    }
  });

  it("marks leading/trailing days from a month that starts on Saturday", () => {
    // August 2026 starts on a Saturday, so the grid begins on Sunday, July 26.
    const grid = buildMonthGrid(new Date(2026, 7, 15));

    expect(grid[0].date).toEqual(new Date(2026, 6, 26));
    expect(grid[0].inCurrentMonth).toBe(false);

    // Leading days: July 26-31 (6 cells)
    for (let i = 0; i < 6; i++) {
      expect(grid[i].inCurrentMonth).toBe(false);
    }

    // In-month days: August 1-31 (31 cells)
    for (let i = 6; i < 37; i++) {
      expect(grid[i].date.getMonth()).toBe(7);
      expect(grid[i].inCurrentMonth).toBe(true);
    }

    // Trailing days: September 1-5 (5 cells)
    for (let i = 37; i < 42; i++) {
      expect(grid[i].date.getMonth()).toBe(8);
      expect(grid[i].inCurrentMonth).toBe(false);
    }
  });

  it("still fills six rows for a month that starts on Sunday", () => {
    // February 2026 starts on a Sunday and has 28 days, so it only spans
    // 4 calendar rows on its own — the grid still pads to six.
    const grid = buildMonthGrid(new Date(2026, 1, 10));

    expect(grid).toHaveLength(42);
    expect(grid[0].date).toEqual(new Date(2026, 1, 1));
    expect(grid[0].inCurrentMonth).toBe(true);
    expect(grid[27].date).toEqual(new Date(2026, 1, 28));
    expect(grid[27].inCurrentMonth).toBe(true);
    expect(grid[28].date).toEqual(new Date(2026, 2, 1));
    expect(grid[28].inCurrentMonth).toBe(false);
    expect(grid[41].date).toEqual(new Date(2026, 2, 14));
    expect(grid[41].inCurrentMonth).toBe(false);
  });

  it("handles a December -> January year boundary", () => {
    // December 2025 starts on a Monday, so the grid begins on Sunday,
    // November 30, and trails into January 2026.
    const grid = buildMonthGrid(new Date(2025, 11, 10));

    expect(grid[0].date).toEqual(new Date(2025, 10, 30));
    expect(grid[0].inCurrentMonth).toBe(false);

    expect(grid[1].date).toEqual(new Date(2025, 11, 1));
    expect(grid[1].inCurrentMonth).toBe(true);

    expect(grid[31].date).toEqual(new Date(2025, 11, 31));
    expect(grid[31].inCurrentMonth).toBe(true);

    expect(grid[32].date).toEqual(new Date(2026, 0, 1));
    expect(grid[32].inCurrentMonth).toBe(false);

    expect(grid[41].date).toEqual(new Date(2026, 0, 10));
    expect(grid[41].inCurrentMonth).toBe(false);
  });
});

describe("getOccurrencesForDay", () => {
  function makeOccurrence(id: string, start: Date, end: Date): Occurrence {
    return {
      event: { id, calendarId: "cal-1", title: id, start, end },
      start,
      end,
    };
  }

  it("returns only occurrences that fall on the given day", () => {
    const day = new Date(2026, 7, 10);
    const matching = makeOccurrence(
      "on-day",
      new Date(2026, 7, 10, 9, 0),
      new Date(2026, 7, 10, 10, 0),
    );
    const other = makeOccurrence(
      "other-day",
      new Date(2026, 7, 11, 9, 0),
      new Date(2026, 7, 11, 10, 0),
    );

    const result = getOccurrencesForDay([matching, other], day);

    expect(result).toEqual([matching]);
  });

  it("orders same-day occurrences by start time", () => {
    const day = new Date(2026, 7, 10);
    const late = makeOccurrence(
      "late",
      new Date(2026, 7, 10, 14, 0),
      new Date(2026, 7, 10, 15, 0),
    );
    const early = makeOccurrence(
      "early",
      new Date(2026, 7, 10, 8, 0),
      new Date(2026, 7, 10, 9, 0),
    );
    const middle = makeOccurrence(
      "middle",
      new Date(2026, 7, 10, 11, 0),
      new Date(2026, 7, 10, 12, 0),
    );

    const result = getOccurrencesForDay([late, early, middle], day);

    expect(result.map((o) => o.event.id)).toEqual(["early", "middle", "late"]);
  });
});

describe("computeChipCapacity", () => {
  it("shows every chip with no overflow when everything just fits", () => {
    const result = computeChipCapacity(3, 60, 20, 20);
    expect(result).toEqual({ visibleCount: 3, overflowCount: 0 });
  });

  it("reserves the last line for a +N more indicator when events overflow", () => {
    const result = computeChipCapacity(4, 60, 20, 20);
    expect(result).toEqual({ visibleCount: 2, overflowCount: 2 });
  });

  it("adapts the visible count to a taller available height", () => {
    const result = computeChipCapacity(4, 100, 20, 20);
    expect(result).toEqual({ visibleCount: 4, overflowCount: 0 });
  });

  it("never shows a negative visible count when the height is smaller than one chip", () => {
    const result = computeChipCapacity(2, 10, 20, 20);
    expect(result).toEqual({ visibleCount: 0, overflowCount: 2 });
  });

  it("shows no chips and no overflow when there are no events", () => {
    const result = computeChipCapacity(0, 60, 20, 20);
    expect(result).toEqual({ visibleCount: 0, overflowCount: 0 });
  });
});

describe("computeCellDraft", () => {
  it("rounds up to the next 15-minute slot, applied to the clicked date", () => {
    const cellDate = new Date(2026, 7, 20);
    const now = new Date(2026, 7, 3, 9, 5);

    const result = computeCellDraft(cellDate, now);

    expect(result.start).toEqual(new Date(2026, 7, 20, 9, 15));
  });

  it("gives the draft a 30-minute duration", () => {
    const cellDate = new Date(2026, 7, 20);
    const now = new Date(2026, 7, 3, 9, 5);

    const result = computeCellDraft(cellDate, now);

    expect(result.end).toEqual(new Date(2026, 7, 20, 9, 45));
  });

  it("lands at 00:00 on the clicked date when rounding pushes past midnight", () => {
    const cellDate = new Date(2026, 7, 20);
    const now = new Date(2026, 7, 3, 23, 50);

    const result = computeCellDraft(cellDate, now);

    expect(result.start).toEqual(new Date(2026, 7, 20, 0, 0));
    expect(result.end).toEqual(new Date(2026, 7, 20, 0, 30));
  });

  it("leaves an already-on-increment time-of-day unchanged", () => {
    const cellDate = new Date(2026, 7, 20);
    const now = new Date(2026, 7, 3, 14, 30);

    const result = computeCellDraft(cellDate, now);

    expect(result.start).toEqual(new Date(2026, 7, 20, 14, 30));
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
