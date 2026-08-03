import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { expandOccurrences } from "./expandOccurrences";
import type { Event } from "./event";

// Pin the timezone to one that observes DST so the wall-clock hold test is
// meaningful; Node applies TZ to subsequently-created Dates. Scoped to this file
// so other suites keep the ambient timezone.
beforeAll(() => {
  vi.stubEnv("TZ", "America/New_York");
});
afterAll(() => {
  vi.unstubAllEnvs();
});

function makeEvent(overrides: Partial<Event>): Event {
  return {
    id: "evt-1",
    calendarId: "cal-1",
    title: "Standup",
    start: new Date(2026, 0, 6, 9, 0),
    end: new Date(2026, 0, 6, 9, 30),
    ...overrides,
  };
}

describe("expandOccurrences", () => {
  it("yields a single Occurrence for a non-recurring Event in the window", () => {
    const event = makeEvent({});
    const occurrences = expandOccurrences(
      [event],
      new Date(2026, 0, 1),
      new Date(2026, 0, 8),
    );
    expect(occurrences).toHaveLength(1);
    expect(occurrences[0].event).toBe(event);
    expect(occurrences[0].start).toEqual(event.start);
  });

  it("omits a non-recurring Event that falls outside the window", () => {
    const event = makeEvent({});
    const occurrences = expandOccurrences(
      [event],
      new Date(2026, 1, 1),
      new Date(2026, 1, 8),
    );
    expect(occurrences).toHaveLength(0);
  });

  it("expands a weekly series to each in-window occurrence", () => {
    // 2026-01-06 is a Tuesday.
    const event = makeEvent({ rrule: "FREQ=WEEKLY;BYDAY=TU" });
    const occurrences = expandOccurrences(
      [event],
      new Date(2026, 0, 1),
      new Date(2026, 0, 27),
    );
    const starts = occurrences
      .map((o) => o.start)
      .sort((a, b) => a.getTime() - b.getTime());
    // Tuesdays Jan 6, 13, 20 fall in [Jan 1, Jan 27); Jan 27 is excluded.
    expect(starts).toHaveLength(3);
    expect(starts.map((d) => d.getDate())).toEqual([6, 13, 20]);
    // Every occurrence inherits the master's 30-minute duration.
    for (const o of occurrences) {
      expect(o.end.getTime() - o.start.getTime()).toBe(30 * 60 * 1000);
    }
  });

  it("respects a bounded series' COUNT even when the window is larger", () => {
    const event = makeEvent({ rrule: "FREQ=DAILY;COUNT=3" });
    const occurrences = expandOccurrences(
      [event],
      new Date(2026, 0, 1),
      new Date(2026, 0, 31),
    );
    expect(occurrences).toHaveLength(3);
  });

  it("bounds an unbounded ('Never ends') series to the window without hanging", () => {
    const event = makeEvent({ rrule: "FREQ=DAILY" });
    // A 5-day window over an infinite daily series must return exactly 5.
    const occurrences = expandOccurrences(
      [event],
      new Date(2026, 0, 6),
      new Date(2026, 0, 11),
    );
    expect(occurrences).toHaveLength(5);
  });

  it("holds the wall-clock time across a DST transition (09:00 stays 09:00)", () => {
    // US spring-forward is 2026-03-08. 2026-03-03 is a Tuesday at 09:00 EST;
    // 2026-03-10 and 2026-03-17 are Tuesdays at 09:00 EDT.
    const event = makeEvent({
      start: new Date(2026, 2, 3, 9, 0),
      end: new Date(2026, 2, 3, 9, 30),
      rrule: "FREQ=WEEKLY;BYDAY=TU",
    });
    const occurrences = expandOccurrences(
      [event],
      new Date(2026, 2, 1),
      new Date(2026, 2, 25),
    ).sort((a, b) => a.start.getTime() - b.start.getTime());

    // Tuesdays Mar 3, 10, 17, 24 all fall in [Mar 1, Mar 25).
    expect(occurrences).toHaveLength(4);
    for (const o of occurrences) {
      expect(o.start.getHours()).toBe(9);
      expect(o.start.getMinutes()).toBe(0);
    }
    // The transition really happened between the first two occurrences.
    expect(occurrences[0].start.getDate()).toBe(3);
    expect(occurrences[1].start.getDate()).toBe(10);
  });

  it("drops an Occurrence whose start matches an Exception (exdate)", () => {
    const event = makeEvent({
      rrule: "FREQ=WEEKLY;BYDAY=TU",
      exdates: [new Date(2026, 0, 13, 9, 0)],
    });
    const occurrences = expandOccurrences(
      [event],
      new Date(2026, 0, 1),
      new Date(2026, 0, 27),
    );
    const dates = occurrences.map((o) => o.start.getDate()).sort((a, b) => a - b);
    expect(dates).toEqual([6, 20]);
  });

  it("substitutes an Override for the Occurrence it replaces", () => {
    const master = makeEvent({ rrule: "FREQ=WEEKLY;BYDAY=TU" });
    const override = makeEvent({
      id: "evt-2",
      title: "Standup (moved)",
      parentId: master.id,
      recurrenceId: new Date(2026, 0, 13, 9, 0),
      start: new Date(2026, 0, 13, 14, 0),
      end: new Date(2026, 0, 13, 14, 30),
    });

    const occurrences = expandOccurrences(
      [master, override],
      new Date(2026, 0, 1),
      new Date(2026, 0, 27),
    );

    expect(occurrences).toHaveLength(3);
    const jan13 = occurrences.find((o) => o.start.getDate() === 13);
    expect(jan13?.event).toBe(override);
    expect(jan13?.start).toEqual(override.start);
    expect(jan13?.end).toEqual(override.end);
  });

  it("does not expand an Override as its own series", () => {
    const master = makeEvent({ rrule: "FREQ=WEEKLY;BYDAY=TU" });
    const override = makeEvent({
      id: "evt-2",
      parentId: master.id,
      recurrenceId: new Date(2026, 0, 13, 9, 0),
      start: new Date(2026, 0, 13, 14, 0),
      end: new Date(2026, 0, 13, 14, 30),
    });

    const occurrences = expandOccurrences(
      [override],
      new Date(2026, 0, 1),
      new Date(2026, 0, 27),
    );

    expect(occurrences).toHaveLength(0);
  });

  it("omits an Override outside the window even though its parent's slot is inside it", () => {
    const master = makeEvent({ rrule: "FREQ=WEEKLY;BYDAY=TU" });
    const override = makeEvent({
      id: "evt-2",
      parentId: master.id,
      recurrenceId: new Date(2026, 0, 13, 9, 0),
      start: new Date(2026, 1, 20, 9, 0),
      end: new Date(2026, 1, 20, 9, 30),
    });

    const occurrences = expandOccurrences(
      [master, override],
      new Date(2026, 0, 1),
      new Date(2026, 0, 27),
    );

    expect(occurrences.some((o) => o.start.getDate() === 13)).toBe(false);
    expect(occurrences).toHaveLength(2);
  });
});
