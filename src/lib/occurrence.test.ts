import { describe, expect, it } from "vitest";
import { occurrenceKey, seriesEditChanges, type Occurrence } from "./occurrence";
import type { Event } from "./event";

function makeEvent(overrides: Partial<Event> = {}): Event {
  return {
    id: "evt-1",
    calendarId: "cal-1",
    title: "Standup",
    start: new Date(2026, 7, 18, 9, 0),
    end: new Date(2026, 7, 18, 9, 30),
    ...overrides,
  };
}

describe("occurrenceKey", () => {
  it("combines the event id and the occurrence start", () => {
    const event = makeEvent();
    const occurrence: Occurrence = {
      event,
      start: new Date(2026, 7, 25, 9, 0),
      end: new Date(2026, 7, 25, 9, 30),
    };
    expect(occurrenceKey(occurrence)).toBe(
      `evt-1::${new Date(2026, 7, 25, 9, 0).toISOString()}`,
    );
  });

  it("distinguishes two Occurrences of the same Master", () => {
    const event = makeEvent();
    const a: Occurrence = {
      event,
      start: new Date(2026, 7, 18, 9, 0),
      end: new Date(2026, 7, 18, 9, 30),
    };
    const b: Occurrence = {
      event,
      start: new Date(2026, 7, 25, 9, 0),
      end: new Date(2026, 7, 25, 9, 30),
    };
    expect(occurrenceKey(a)).not.toBe(occurrenceKey(b));
  });
});

describe("seriesEditChanges", () => {
  it("moves a non-recurring event without adding a rule", () => {
    const event = makeEvent();
    const start = new Date(2026, 7, 19, 10, 0);
    const end = new Date(2026, 7, 19, 10, 30);
    expect(seriesEditChanges(event, start, end)).toEqual({
      start,
      end,
      rrule: undefined,
    });
  });

  it("re-anchors a recurring series' rule to the new start (whole series)", () => {
    // 2026-08-18 is a Tuesday; move to 2026-08-19, a Wednesday.
    const event = makeEvent({ rrule: "FREQ=WEEKLY;BYDAY=TU" });
    const start = new Date(2026, 7, 19, 9, 0);
    const end = new Date(2026, 7, 19, 9, 30);
    expect(seriesEditChanges(event, start, end)).toEqual({
      start,
      end,
      rrule: "FREQ=WEEKLY;BYDAY=WE",
    });
  });
});
