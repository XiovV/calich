import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { fromZonedTime } from "date-fns-tz";
import { expandOccurrences } from "./expandOccurrences";
import type { Event } from "./event";

/** The wall-clock hour `instant` reads as in `zone`, for zone-aware assertions
 * that don't depend on the test runner's ambient timezone. */
function hourIn(instant: Date, zone: string): number {
  return Number(
    new Intl.DateTimeFormat("en-US", {
      hour: "numeric",
      hour12: false,
      timeZone: zone,
    }).format(instant),
  );
}

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

  it("holds the Anchor zone's wall-clock across DST regardless of the viewer's zone (ADR-0019)", () => {
    // Europe/Berlin, not the ambient America/New_York the test file pins —
    // proves expansion follows the Event's tzid, not the viewer's zone.
    // EU spring-forward 2026-03-29: Mondays Mar 2/9/16/23 are CET (10:00
    // local = 09:00 UTC); Mar 30 is CEST (10:00 local = 08:00 UTC).
    const event = makeEvent({
      start: fromZonedTime(new Date(2026, 2, 2, 10, 0), "Europe/Berlin"),
      end: fromZonedTime(new Date(2026, 2, 2, 10, 30), "Europe/Berlin"),
      rrule: "FREQ=WEEKLY;BYDAY=MO",
      tzid: "Europe/Berlin",
    });

    const occurrences = expandOccurrences(
      [event],
      new Date(2026, 2, 1),
      new Date(2026, 2, 31),
    ).sort((a, b) => a.start.getTime() - b.start.getTime());

    expect(occurrences).toHaveLength(5);
    for (const o of occurrences) {
      expect(hourIn(o.start, "Europe/Berlin")).toBe(10);
    }
    // The UTC hour actually shifts once the offset changes, proving DST was
    // applied rather than the wall-clock silently drifting.
    const utcHours = new Set(occurrences.map((o) => o.start.getUTCHours()));
    expect(utcHours.size).toBe(2);
  });

  it("expands an Etc/UTC event with no DST drift, even while the viewer's zone observes DST", () => {
    // Weekly Tuesday 09:00 UTC across the same America/New_York
    // spring-forward window the wall-clock-hold test above uses.
    const event = makeEvent({
      start: new Date(Date.UTC(2026, 2, 3, 9, 0)),
      end: new Date(Date.UTC(2026, 2, 3, 9, 30)),
      rrule: "FREQ=WEEKLY;BYDAY=TU",
      tzid: "Etc/UTC",
    });

    const occurrences = expandOccurrences(
      [event],
      new Date(2026, 2, 1),
      new Date(2026, 2, 25),
    );

    expect(occurrences).toHaveLength(4);
    for (const o of occurrences) {
      expect(o.start.getUTCHours()).toBe(9);
      expect(o.start.getUTCMinutes()).toBe(0);
    }
  });

  it("resolves a nonexistent wall-clock (spring-forward gap) and an ambiguous one (fall-back overlap) without skipping or duplicating a day", () => {
    // America/New_York: 2026-03-08 has no 02:30 (clocks skip 02:00->03:00);
    // 2026-11-01 has two (clocks repeat 01:00-02:00). date-fns-tz's defaults
    // — forward-shift the nonexistent instant, take the earlier offset for
    // the ambiguous one — resolve both to a single instant per day, so a
    // daily series never skips or duplicates an Occurrence (ADR-0019).
    const event = makeEvent({
      start: fromZonedTime(new Date(2026, 2, 6, 2, 30), "America/New_York"),
      end: fromZonedTime(new Date(2026, 2, 6, 3, 0), "America/New_York"),
      rrule: "FREQ=DAILY",
      tzid: "America/New_York",
    });

    const springGap = expandOccurrences(
      [event],
      new Date(2026, 2, 7),
      new Date(2026, 2, 10),
    );
    expect(springGap).toHaveLength(3);
    expect(new Set(springGap.map((o) => o.start.getTime())).size).toBe(3);

    const fallOverlap = expandOccurrences(
      [event],
      new Date(2026, 9, 30),
      new Date(2026, 10, 3),
    );
    expect(fallOverlap).toHaveLength(4);
    expect(new Set(fallOverlap.map((o) => o.start.getTime())).size).toBe(4);
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

  it("suppresses an excepted Occurrence's Reminders along with the Occurrence itself (ADR-0020)", () => {
    // An Exception drops the Occurrence outright — there's no Occurrence for
    // the firing engine to compute a due Reminder against, so a Master's
    // Reminders never reach a cancelled Occurrence.
    const event = makeEvent({
      rrule: "FREQ=WEEKLY;BYDAY=TU",
      exdates: [new Date(2026, 0, 13, 9, 0)],
      reminders: [{ offsetMinutes: 10, channel: "notification" }],
    });
    const occurrences = expandOccurrences(
      [event],
      new Date(2026, 0, 1),
      new Date(2026, 0, 27),
    );
    expect(occurrences.some((o) => o.start.getTime() === new Date(2026, 0, 13, 9, 0).getTime())).toBe(
      false,
    );
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

  it("renders an Override moved onto another day, whose replaced slot is outside the window", () => {
    // The Tuesday series whose Aug 18 Occurrence was moved onto Wed Aug 19:
    // Day view on the 19th has to show it even though the slot it replaces —
    // the only thing that used to emit it — sits outside that day's window.
    const master = makeEvent({
      start: new Date(2026, 7, 18, 9, 0),
      end: new Date(2026, 7, 18, 9, 30),
      rrule: "FREQ=WEEKLY;BYDAY=TU",
    });
    const override = makeEvent({
      id: "evt-2",
      title: "Standup (moved)",
      parentId: master.id,
      recurrenceId: new Date(2026, 7, 18, 9, 0),
      start: new Date(2026, 7, 19, 9, 0),
      end: new Date(2026, 7, 19, 9, 30),
    });

    const wed = expandOccurrences(
      [master, override],
      new Date(2026, 7, 19),
      new Date(2026, 7, 20),
    );
    expect(wed).toHaveLength(1);
    expect(wed[0].event).toBe(override);
    expect(wed[0].start).toEqual(override.start);
    expect(wed[0].end).toEqual(override.end);

    // And the day it was moved off stays empty: the Master's slot is still
    // suppressed there, rather than the Occurrence rendering on both days.
    const tue = expandOccurrences(
      [master, override],
      new Date(2026, 7, 18),
      new Date(2026, 7, 19),
    );
    expect(tue).toHaveLength(0);
  });

  it("puts an Override moved across a week boundary in the week containing its own start only", () => {
    // The Sunday series whose Aug 23 Occurrence was moved onto Mon Aug 24.
    // With Monday-start weeks, Aug 23 closes one week and Aug 24 opens the
    // next, so the two windows disagree about which one the Occurrence is in.
    const master = makeEvent({
      start: new Date(2026, 7, 23, 9, 0),
      end: new Date(2026, 7, 23, 9, 30),
      rrule: "FREQ=WEEKLY;BYDAY=SU",
    });
    const override = makeEvent({
      id: "evt-2",
      parentId: master.id,
      recurrenceId: new Date(2026, 7, 23, 9, 0),
      start: new Date(2026, 7, 24, 9, 0),
      end: new Date(2026, 7, 24, 9, 30),
    });

    const weekOfAug17 = expandOccurrences(
      [master, override],
      new Date(2026, 7, 17),
      new Date(2026, 7, 24),
    );
    expect(weekOfAug17).toHaveLength(0);

    const weekOfAug24 = expandOccurrences(
      [master, override],
      new Date(2026, 7, 24),
      new Date(2026, 7, 31),
    );
    // The Override plus the untouched Sun Aug 30 Occurrence.
    expect(weekOfAug24).toHaveLength(2);
    expect(weekOfAug24.filter((o) => o.event === override)).toHaveLength(1);
  });

  it("puts an Override moved across a month boundary in the month containing its own start only", () => {
    // A Monday series starting Aug 31, whose first Occurrence moved onto
    // Tue Sep 1 — the same split as the week case, one grid up.
    const master = makeEvent({
      start: new Date(2026, 7, 31, 9, 0),
      end: new Date(2026, 7, 31, 9, 30),
      rrule: "FREQ=WEEKLY;BYDAY=MO",
    });
    const override = makeEvent({
      id: "evt-2",
      parentId: master.id,
      recurrenceId: new Date(2026, 7, 31, 9, 0),
      start: new Date(2026, 8, 1, 9, 0),
      end: new Date(2026, 8, 1, 9, 30),
    });

    const august = expandOccurrences(
      [master, override],
      new Date(2026, 7, 1),
      new Date(2026, 8, 1),
    );
    expect(august).toHaveLength(0);

    const september = expandOccurrences(
      [master, override],
      new Date(2026, 8, 1),
      new Date(2026, 9, 1),
    );
    // The Override plus the Mondays Sep 7, 14, 21 and 28.
    expect(september).toHaveLength(5);
    expect(september.filter((o) => o.event === override)).toHaveLength(1);
  });

  it("renders an Override moved backwards, whose replaced slot is after the window", () => {
    // The mirror of the week case: a Monday series whose Aug 24 Occurrence
    // was moved back onto Sun Aug 23, so the slot it replaces is past the end
    // of the window its own start is in rather than before its start.
    const master = makeEvent({
      start: new Date(2026, 7, 24, 9, 0),
      end: new Date(2026, 7, 24, 9, 30),
      rrule: "FREQ=WEEKLY;BYDAY=MO",
    });
    const override = makeEvent({
      id: "evt-2",
      parentId: master.id,
      recurrenceId: new Date(2026, 7, 24, 9, 0),
      start: new Date(2026, 7, 23, 9, 0),
      end: new Date(2026, 7, 23, 9, 30),
    });

    const weekOfAug17 = expandOccurrences(
      [master, override],
      new Date(2026, 7, 17),
      new Date(2026, 7, 24),
    );
    expect(weekOfAug17).toHaveLength(1);
    expect(weekOfAug17[0].event).toBe(override);

    const weekOfAug24 = expandOccurrences(
      [master, override],
      new Date(2026, 7, 24),
      new Date(2026, 7, 31),
    );
    // Nothing: the only Monday this window holds is Aug 24, whose slot the
    // Override consumed on its way out of the week.
    expect(weekOfAug24).toHaveLength(0);
  });

  it("emits a moved Override once when its own start and its replaced slot are both in the window", () => {
    const master = makeEvent({
      start: new Date(2026, 7, 18, 9, 0),
      end: new Date(2026, 7, 18, 9, 30),
      rrule: "FREQ=WEEKLY;BYDAY=TU",
    });
    const override = makeEvent({
      id: "evt-2",
      parentId: master.id,
      recurrenceId: new Date(2026, 7, 18, 9, 0),
      start: new Date(2026, 7, 19, 9, 0),
      end: new Date(2026, 7, 19, 9, 30),
    });

    // A Monday-start week holding both Tue Aug 18 and Wed Aug 19.
    const occurrences = expandOccurrences(
      [master, override],
      new Date(2026, 7, 17),
      new Date(2026, 7, 24),
    );
    expect(occurrences).toHaveLength(1);
    expect(occurrences[0].event).toBe(override);
  });

  it("keeps an excepted Occurrence cancelled even when its Override moved into the window", () => {
    // An Exception on the slot suppresses it wherever the Override sits, so
    // which window is being expanded can't change the answer.
    const master = makeEvent({
      start: new Date(2026, 7, 18, 9, 0),
      end: new Date(2026, 7, 18, 9, 30),
      rrule: "FREQ=WEEKLY;BYDAY=TU",
      exdates: [new Date(2026, 7, 18, 9, 0)],
    });
    const override = makeEvent({
      id: "evt-2",
      parentId: master.id,
      recurrenceId: new Date(2026, 7, 18, 9, 0),
      start: new Date(2026, 7, 19, 9, 0),
      end: new Date(2026, 7, 19, 9, 30),
    });

    const occurrences = expandOccurrences(
      [master, override],
      new Date(2026, 7, 19),
      new Date(2026, 7, 20),
    );
    expect(occurrences).toHaveLength(0);
  });
});

describe("expandOccurrences all-day", () => {
  it("yields a whole-date Occurrence for a non-recurring all-day Event", () => {
    const event = makeEvent({
      allDay: true,
      start: new Date(2026, 7, 4),
      end: new Date(2026, 7, 5),
    });
    const occurrences = expandOccurrences(
      [event],
      new Date(2026, 7, 1),
      new Date(2026, 7, 8),
    );
    expect(occurrences).toHaveLength(1);
    expect(occurrences[0].start).toEqual(new Date(2026, 7, 4));
    expect(occurrences[0].end).toEqual(new Date(2026, 7, 5));
  });

  it("expands a recurring all-day Event into whole-date Occurrences", () => {
    const event = makeEvent({
      allDay: true,
      start: new Date(2026, 0, 6),
      end: new Date(2026, 0, 7),
      rrule: "FREQ=WEEKLY;BYDAY=TU",
    });
    const occurrences = expandOccurrences(
      [event],
      new Date(2026, 0, 1),
      new Date(2026, 0, 27),
    );
    const starts = occurrences
      .map((o) => o.start)
      .sort((a, b) => a.getTime() - b.getTime());
    expect(starts).toEqual([
      new Date(2026, 0, 6),
      new Date(2026, 0, 13),
      new Date(2026, 0, 20),
    ]);
    for (const o of occurrences) {
      expect(o.end).toEqual(new Date(o.start.getFullYear(), o.start.getMonth(), o.start.getDate() + 1));
    }
  });

  // America/New_York springs forward on 2026-03-08; a ms-based duration would
  // land the end 23 hours after start instead of at the next date's midnight.
  it("keeps a recurring all-day Occurrence's end at the next whole date across a DST transition", () => {
    const event = makeEvent({
      allDay: true,
      start: new Date(2026, 2, 6),
      end: new Date(2026, 2, 7),
      rrule: "FREQ=DAILY",
    });
    const occurrences = expandOccurrences(
      [event],
      new Date(2026, 2, 6),
      new Date(2026, 2, 10),
    );
    const marchEighth = occurrences.find((o) => o.start.getDate() === 8);
    expect(marchEighth).toBeDefined();
    expect(marchEighth!.end).toEqual(new Date(2026, 2, 9));
  });
});
