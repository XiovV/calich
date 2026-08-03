import { describe, expect, it } from "vitest";
import { layoutOverlappingEvents } from "./layoutOverlappingEvents";
import type { Occurrence } from "./occurrence";

function makeOccurrence(
  id: string,
  start: [hour: number, minute: number],
  end: [hour: number, minute: number],
): Occurrence {
  const startDate = new Date(2026, 7, 3, start[0], start[1]);
  const endDate = new Date(2026, 7, 3, end[0], end[1]);
  return {
    event: {
      id,
      calendarId: "personal",
      title: id,
      start: startDate,
      end: endDate,
    },
    start: startDate,
    end: endDate,
  };
}

describe("layoutOverlappingEvents", () => {
  it("returns an empty array for no occurrences", () => {
    expect(layoutOverlappingEvents([])).toEqual([]);
  });

  it("gives a single occurrence column 0 with columnCount 1", () => {
    const a = makeOccurrence("a", [9, 0], [10, 0]);
    const result = layoutOverlappingEvents([a]);
    expect(result).toEqual([{ occurrence: a, column: 0, columnCount: 1 }]);
  });

  it("gives two non-overlapping occurrences each column 0, columnCount 1", () => {
    const a = makeOccurrence("a", [9, 0], [10, 0]);
    const b = makeOccurrence("b", [11, 0], [12, 0]);
    const result = layoutOverlappingEvents([a, b]);
    expect(result).toEqual([
      { occurrence: a, column: 0, columnCount: 1 },
      { occurrence: b, column: 0, columnCount: 1 },
    ]);
  });

  it("splits two overlapping occurrences into separate columns", () => {
    const a = makeOccurrence("a", [9, 0], [10, 0]);
    const b = makeOccurrence("b", [9, 30], [10, 30]);
    const result = layoutOverlappingEvents([a, b]);
    expect(result).toEqual([
      { occurrence: a, column: 0, columnCount: 2 },
      { occurrence: b, column: 1, columnCount: 2 },
    ]);
  });

  it("splits three mutually overlapping occurrences into three columns", () => {
    const a = makeOccurrence("a", [9, 0], [11, 0]);
    const b = makeOccurrence("b", [9, 30], [10, 30]);
    const c = makeOccurrence("c", [9, 45], [10, 45]);
    const result = layoutOverlappingEvents([a, b, c]);
    expect(result).toEqual([
      { occurrence: a, column: 0, columnCount: 3 },
      { occurrence: b, column: 1, columnCount: 3 },
      { occurrence: c, column: 2, columnCount: 3 },
    ]);
  });

  it("treats a chain of overlaps (A-B, B-C, but not A-C) as one cluster, reusing freed columns", () => {
    const a = makeOccurrence("a", [9, 0], [10, 0]);
    const b = makeOccurrence("b", [9, 30], [10, 30]);
    const c = makeOccurrence("c", [10, 15], [11, 0]);
    const result = layoutOverlappingEvents([a, b, c]);
    // a and c don't actually overlap (a ends 10:00, c starts 10:15), so c
    // reuses column 0 once a has ended — the cluster only ever needs 2
    // simultaneous columns, even though all three share one cluster.
    expect(result).toEqual([
      { occurrence: a, column: 0, columnCount: 2 },
      { occurrence: b, column: 1, columnCount: 2 },
      { occurrence: c, column: 0, columnCount: 2 },
    ]);
  });

  it("starts a new cluster once a gap fully separates occurrences", () => {
    const a = makeOccurrence("a", [9, 0], [10, 0]);
    const b = makeOccurrence("b", [9, 30], [10, 30]);
    const c = makeOccurrence("c", [12, 0], [13, 0]);
    const result = layoutOverlappingEvents([a, b, c]);
    expect(result).toEqual([
      { occurrence: a, column: 0, columnCount: 2 },
      { occurrence: b, column: 1, columnCount: 2 },
      { occurrence: c, column: 0, columnCount: 1 },
    ]);
  });
});
