import { describe, expect, it } from "vitest";
import {
  getDaySegments,
  occurrenceIntersectsDay,
  occurrenceSegmentForDay,
} from "./occurrenceSegments";
import type { Occurrence } from "./occurrence";

function makeOccurrence(id: string, start: Date, end: Date): Occurrence {
  return {
    event: { id, calendarId: "cal-1", title: id, start, end },
    start,
    end,
  };
}

describe("occurrenceIntersectsDay", () => {
  it("is true for an Occurrence wholly inside the day", () => {
    const occurrence = makeOccurrence(
      "a",
      new Date(2026, 7, 10, 9, 0),
      new Date(2026, 7, 10, 10, 0),
    );
    expect(occurrenceIntersectsDay(occurrence, new Date(2026, 7, 10))).toBe(true);
  });

  it("is false for an Occurrence on a different day entirely", () => {
    const occurrence = makeOccurrence(
      "a",
      new Date(2026, 7, 10, 9, 0),
      new Date(2026, 7, 10, 10, 0),
    );
    expect(occurrenceIntersectsDay(occurrence, new Date(2026, 7, 11))).toBe(false);
  });

  it("is true on both days for an Occurrence crossing midnight", () => {
    const occurrence = makeOccurrence(
      "a",
      new Date(2026, 7, 10, 23, 0),
      new Date(2026, 7, 11, 1, 0),
    );
    expect(occurrenceIntersectsDay(occurrence, new Date(2026, 7, 10))).toBe(true);
    expect(occurrenceIntersectsDay(occurrence, new Date(2026, 7, 11))).toBe(true);
  });

  it("is false on the day after an Occurrence that ends exactly at midnight", () => {
    const occurrence = makeOccurrence(
      "a",
      new Date(2026, 7, 10, 23, 0),
      new Date(2026, 7, 11, 0, 0),
    );
    expect(occurrenceIntersectsDay(occurrence, new Date(2026, 7, 10))).toBe(true);
    expect(occurrenceIntersectsDay(occurrence, new Date(2026, 7, 11))).toBe(false);
  });

  it("is true on every day an Occurrence spans more than two days", () => {
    const occurrence = makeOccurrence(
      "a",
      new Date(2026, 7, 10, 23, 0),
      new Date(2026, 7, 13, 1, 0),
    );
    expect(occurrenceIntersectsDay(occurrence, new Date(2026, 7, 10))).toBe(true);
    expect(occurrenceIntersectsDay(occurrence, new Date(2026, 7, 11))).toBe(true);
    expect(occurrenceIntersectsDay(occurrence, new Date(2026, 7, 12))).toBe(true);
    expect(occurrenceIntersectsDay(occurrence, new Date(2026, 7, 13))).toBe(true);
    expect(occurrenceIntersectsDay(occurrence, new Date(2026, 7, 14))).toBe(false);
  });
});

describe("occurrenceSegmentForDay", () => {
  it("returns the Occurrence's own bounds unclipped when it doesn't cross midnight", () => {
    const occurrence = makeOccurrence(
      "a",
      new Date(2026, 7, 10, 9, 0),
      new Date(2026, 7, 10, 10, 0),
    );
    const segment = occurrenceSegmentForDay(occurrence, new Date(2026, 7, 10));
    expect(segment.start).toEqual(occurrence.start);
    expect(segment.end).toEqual(occurrence.end);
    expect(segment.occurrence).toBe(occurrence);
  });

  it("clips the end to midnight on the start day of a midnight-crossing Occurrence", () => {
    const occurrence = makeOccurrence(
      "a",
      new Date(2026, 7, 10, 23, 0),
      new Date(2026, 7, 11, 1, 0),
    );
    const segment = occurrenceSegmentForDay(occurrence, new Date(2026, 7, 10));
    expect(segment.start).toEqual(new Date(2026, 7, 10, 23, 0));
    expect(segment.end).toEqual(new Date(2026, 7, 11, 0, 0));
  });

  it("clips the start to midnight on the end day of a midnight-crossing Occurrence", () => {
    const occurrence = makeOccurrence(
      "a",
      new Date(2026, 7, 10, 23, 0),
      new Date(2026, 7, 11, 1, 0),
    );
    const segment = occurrenceSegmentForDay(occurrence, new Date(2026, 7, 11));
    expect(segment.start).toEqual(new Date(2026, 7, 11, 0, 0));
    expect(segment.end).toEqual(new Date(2026, 7, 11, 1, 0));
  });

  it("clips both edges on a middle day of a 3+ day Occurrence, spanning the full day", () => {
    const occurrence = makeOccurrence(
      "a",
      new Date(2026, 7, 10, 23, 0),
      new Date(2026, 7, 13, 1, 0),
    );
    const segment = occurrenceSegmentForDay(occurrence, new Date(2026, 7, 11));
    expect(segment.start).toEqual(new Date(2026, 7, 11, 0, 0));
    expect(segment.end).toEqual(new Date(2026, 7, 12, 0, 0));
  });
});

describe("getDaySegments", () => {
  it("returns one segment per Occurrence that intersects the day", () => {
    const onDay = makeOccurrence(
      "on-day",
      new Date(2026, 7, 10, 9, 0),
      new Date(2026, 7, 10, 10, 0),
    );
    const otherDay = makeOccurrence(
      "other-day",
      new Date(2026, 7, 11, 9, 0),
      new Date(2026, 7, 11, 10, 0),
    );
    const result = getDaySegments([onDay, otherDay], new Date(2026, 7, 10));
    expect(result.map((segment) => segment.occurrence.event.id)).toEqual(["on-day"]);
  });

  it("splits a midnight-crossing Occurrence into a segment on each of its two days", () => {
    const spanning = makeOccurrence(
      "spanning",
      new Date(2026, 7, 10, 23, 0),
      new Date(2026, 7, 11, 1, 0),
    );

    const firstDay = getDaySegments([spanning], new Date(2026, 7, 10));
    expect(firstDay).toHaveLength(1);
    expect(firstDay[0].start).toEqual(new Date(2026, 7, 10, 23, 0));
    expect(firstDay[0].end).toEqual(new Date(2026, 7, 11, 0, 0));

    const secondDay = getDaySegments([spanning], new Date(2026, 7, 11));
    expect(secondDay).toHaveLength(1);
    expect(secondDay[0].start).toEqual(new Date(2026, 7, 11, 0, 0));
    expect(secondDay[0].end).toEqual(new Date(2026, 7, 11, 1, 0));
  });

  it("gives a segment on every day an Occurrence spanning more than two days touches", () => {
    const spanning = makeOccurrence(
      "spanning",
      new Date(2026, 7, 10, 23, 0),
      new Date(2026, 7, 13, 1, 0),
    );

    expect(getDaySegments([spanning], new Date(2026, 7, 10))).toHaveLength(1);
    expect(getDaySegments([spanning], new Date(2026, 7, 11))).toHaveLength(1);
    expect(getDaySegments([spanning], new Date(2026, 7, 12))).toHaveLength(1);
    expect(getDaySegments([spanning], new Date(2026, 7, 13))).toHaveLength(1);
    expect(getDaySegments([spanning], new Date(2026, 7, 14))).toHaveLength(0);

    const middleDay = getDaySegments([spanning], new Date(2026, 7, 11))[0];
    expect(middleDay.start).toEqual(new Date(2026, 7, 11, 0, 0));
    expect(middleDay.end).toEqual(new Date(2026, 7, 12, 0, 0));
  });
});
