import { describe, expect, it } from "vitest";
import {
  buildCustomRule,
  defaultCustomRecurrence,
  parseCustomRule,
  type CustomRecurrence,
} from "./customRecurrence";
import { expandOccurrences } from "./expandOccurrences";
import type { Event } from "./event";

// 2026-08-18 is a Tuesday and the third Tuesday of August 2026.
const TUESDAY = new Date(2026, 7, 18, 9, 0);

function fields(overrides: Partial<CustomRecurrence> = {}): CustomRecurrence {
  return { ...defaultCustomRecurrence(TUESDAY), ...overrides };
}

describe("buildCustomRule", () => {
  it("builds a plain weekly rule from the start's weekday when no chips are set", () => {
    expect(buildCustomRule(fields({ unit: "week", weekdays: [] }), TUESDAY)).toBe(
      "FREQ=WEEKLY;BYDAY=TU",
    );
  });

  it("includes INTERVAL only when greater than one, and sorts weekday chips", () => {
    expect(
      buildCustomRule(
        fields({ unit: "week", interval: 2, weekdays: [5, 1, 3] }),
        TUESDAY,
      ),
    ).toBe("FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR");
  });

  it("builds the two monthly modes from the start date", () => {
    expect(
      buildCustomRule(fields({ unit: "month", monthlyMode: "day" }), TUESDAY),
    ).toBe("FREQ=MONTHLY;BYMONTHDAY=18");
    expect(
      buildCustomRule(fields({ unit: "month", monthlyMode: "weekday" }), TUESDAY),
    ).toBe("FREQ=MONTHLY;BYDAY=+3TU");
  });

  it("normalizes 'After N occurrences' to an UNTIL end-date (not COUNT)", () => {
    const rule = buildCustomRule(
      fields({ unit: "week", weekdays: [2], end: { type: "after", count: 3 } }),
      TUESDAY,
    );
    // Tuesdays: Aug 18, 25, Sep 1 — the 3rd is 2026-09-01.
    expect(rule).toContain("UNTIL=20260901T235959Z");
    expect(rule).not.toContain("COUNT");
  });

  it("builds an UNTIL from an explicit end date", () => {
    const rule = buildCustomRule(
      fields({ unit: "day", end: { type: "on", date: new Date(2026, 8, 30) } }),
      TUESDAY,
    );
    expect(rule).toBe("FREQ=DAILY;UNTIL=20260930T235959Z");
  });
});

describe("parseCustomRule", () => {
  it("re-populates fields from a stored rule", () => {
    const parsed = parseCustomRule("FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR", TUESDAY);
    expect(parsed).toEqual({
      interval: 2,
      unit: "week",
      weekdays: [1, 3, 5],
      monthlyMode: "weekday",
      end: { type: "never" },
    });
  });

  it("reads a monthly day-of-month rule as the 'day' mode", () => {
    expect(parseCustomRule("FREQ=MONTHLY;BYMONTHDAY=18", TUESDAY).monthlyMode).toBe(
      "day",
    );
  });

  it("reads UNTIL back as an 'on' end date", () => {
    const parsed = parseCustomRule("FREQ=DAILY;UNTIL=20260930T235959Z", TUESDAY);
    expect(parsed.end).toEqual({ type: "on", date: new Date(2026, 8, 30) });
  });

  it("reads COUNT as an 'after' end (robustness for externally-authored rules)", () => {
    expect(parseCustomRule("FREQ=DAILY;COUNT=5", TUESDAY).end).toEqual({
      type: "after",
      count: 5,
    });
  });
});

describe("build → parse → build is stable", () => {
  const cases: CustomRecurrence[] = [
    fields({ unit: "day" }),
    fields({ unit: "day", interval: 3 }),
    fields({ unit: "week", weekdays: [1, 3, 5] }),
    fields({ unit: "week", interval: 2, weekdays: [0, 6] }),
    fields({ unit: "month", monthlyMode: "day" }),
    fields({ unit: "month", monthlyMode: "weekday" }),
    fields({ unit: "year" }),
    fields({ unit: "week", weekdays: [2], end: { type: "after", count: 4 } }),
    fields({ unit: "day", end: { type: "on", date: new Date(2026, 11, 31) } }),
  ];

  it.each(cases)("round-trips %o", (input) => {
    const built = buildCustomRule(input, TUESDAY);
    const reBuilt = buildCustomRule(parseCustomRule(built, TUESDAY), TUESDAY);
    expect(reBuilt).toBe(built);
  });
});

describe("BYDAY=5TU (5th weekday) skips months that lack it", () => {
  it("only produces an Occurrence in months with a 5th Tuesday", () => {
    // 2026-09-29 is the 5th Tuesday of September 2026.
    const fifthTuesday = new Date(2026, 8, 29, 9, 0);
    const rule = buildCustomRule(
      fields({ unit: "month", monthlyMode: "weekday" }),
      fifthTuesday,
    );
    expect(rule).toBe("FREQ=MONTHLY;BYDAY=+5TU");

    const event: Event = {
      id: "evt-1",
      calendarId: "cal-1",
      title: "5th Tuesday",
      start: fifthTuesday,
      end: new Date(2026, 8, 29, 9, 30),
      rrule: rule,
    };
    // Sep–Dec 2026: only Sep (29th) and Dec (29th) have a 5th Tuesday;
    // Oct and Nov have only four, so they are skipped.
    const occurrences = expandOccurrences(
      [event],
      new Date(2026, 8, 1),
      new Date(2027, 0, 1),
    ).sort((a, b) => a.start.getTime() - b.start.getTime());
    expect(occurrences.map((o) => `${o.start.getMonth()}-${o.start.getDate()}`)).toEqual([
      "8-29",
      "11-29",
    ]);
  });
});
