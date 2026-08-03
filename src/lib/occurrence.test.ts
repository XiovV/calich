import { describe, expect, it } from "vitest";
import {
  isRecurringOccurrence,
  occurrenceKey,
  seriesEditChanges,
  type Occurrence,
} from "./occurrence";
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
    const occurrence: Occurrence = { event, start: event.start, end: event.end };
    const newStart = new Date(2026, 7, 19, 10, 0);
    const newEnd = new Date(2026, 7, 19, 10, 30);
    expect(seriesEditChanges(occurrence, newStart, newEnd)).toEqual({
      start: newStart,
      end: newEnd,
      rrule: undefined,
    });
  });

  it("re-anchors a recurring series' rule to the new start (whole series)", () => {
    // 2026-08-18 is a Tuesday; move to 2026-08-19, a Wednesday.
    const event = makeEvent({ rrule: "FREQ=WEEKLY;BYDAY=TU" });
    const occurrence: Occurrence = { event, start: event.start, end: event.end };
    const newStart = new Date(2026, 7, 19, 9, 0);
    const newEnd = new Date(2026, 7, 19, 9, 30);
    expect(seriesEditChanges(occurrence, newStart, newEnd)).toEqual({
      start: newStart,
      end: newEnd,
      rrule: "FREQ=WEEKLY;BYDAY=WE",
    });
  });

  it("shifts the Master's own anchor by the drag delta, not the dragged Occurrence's date", () => {
    // The Master starts on Aug 18; dragging the 3rd Occurrence (Sep 1, a later
    // Tuesday) forward by a week to Sep 8 must move the Master to Aug 25 — a
    // week later than its own original start — not jump to Sep 8 itself,
    // which would truncate every earlier Occurrence out of the series.
    const event = makeEvent({ rrule: "FREQ=WEEKLY;BYDAY=TU" });
    const draggedOccurrence: Occurrence = {
      event,
      start: new Date(2026, 8, 1, 9, 0),
      end: new Date(2026, 8, 1, 9, 30),
    };
    const newStart = new Date(2026, 8, 8, 9, 0);
    const newEnd = new Date(2026, 8, 8, 9, 30);

    expect(seriesEditChanges(draggedOccurrence, newStart, newEnd)).toEqual({
      start: new Date(2026, 7, 25, 9, 0),
      end: new Date(2026, 7, 25, 9, 30),
      rrule: "FREQ=WEEKLY;BYDAY=TU",
    });
  });

  it("resizing a later Occurrence's end only shifts the Master's own end, not its start", () => {
    const event = makeEvent({ rrule: "FREQ=WEEKLY;BYDAY=TU" });
    const draggedOccurrence: Occurrence = {
      event,
      start: new Date(2026, 8, 1, 9, 0),
      end: new Date(2026, 8, 1, 9, 30),
    };
    // Resize-end: the start edge is untouched, only the end moves.
    const newStart = draggedOccurrence.start;
    const newEnd = new Date(2026, 8, 1, 10, 0);

    expect(seriesEditChanges(draggedOccurrence, newStart, newEnd)).toEqual({
      start: event.start,
      end: new Date(2026, 7, 18, 10, 0),
      rrule: "FREQ=WEEKLY;BYDAY=TU",
    });
  });

  it("leaves a custom multi-weekday rule untouched when the drag stays on the same day", () => {
    // "reanchorRule" only understands the fixed presets, so rebuilding a
    // custom Mon/Wed/Fri rule on every resize (which never changes which days
    // repeat) would collapse it down to a single weekday.
    const event = makeEvent({ rrule: "FREQ=WEEKLY;BYDAY=MO,WE,FR" });
    const draggedOccurrence: Occurrence = {
      event,
      start: new Date(2026, 8, 2, 9, 0), // a Wednesday
      end: new Date(2026, 8, 2, 9, 30),
    };
    // Resize-end: same day, only the duration changes.
    const newStart = draggedOccurrence.start;
    const newEnd = new Date(2026, 8, 2, 10, 0);

    expect(seriesEditChanges(draggedOccurrence, newStart, newEnd).rrule).toBe(
      "FREQ=WEEKLY;BYDAY=MO,WE,FR",
    );
  });

  it("still rebuilds the rule from the nearest preset when a custom rule is moved to a different day", () => {
    const event = makeEvent({ rrule: "FREQ=WEEKLY;BYDAY=MO,WE,FR" });
    const draggedOccurrence: Occurrence = {
      event,
      start: new Date(2026, 8, 2, 9, 0), // a Wednesday
      end: new Date(2026, 8, 2, 9, 30),
    };
    const newStart = new Date(2026, 8, 3, 9, 0); // moved to Thursday
    const newEnd = new Date(2026, 8, 3, 9, 30);

    expect(seriesEditChanges(draggedOccurrence, newStart, newEnd).rrule).toBe(
      "FREQ=WEEKLY;BYDAY=TH",
    );
  });
});

describe("isRecurringOccurrence", () => {
  it("is false for a non-recurring Event's lone Occurrence", () => {
    const occurrence: Occurrence = {
      event: makeEvent(),
      start: makeEvent().start,
      end: makeEvent().end,
    };
    expect(isRecurringOccurrence(occurrence)).toBe(false);
  });

  it("is true for a Master with a rule", () => {
    const occurrence: Occurrence = {
      event: makeEvent({ rrule: "FREQ=DAILY" }),
      start: makeEvent().start,
      end: makeEvent().end,
    };
    expect(isRecurringOccurrence(occurrence)).toBe(true);
  });

  it("is true for an Override, even though it carries no rule of its own", () => {
    const occurrence: Occurrence = {
      event: makeEvent({ parentId: "master-1", recurrenceId: makeEvent().start }),
      start: makeEvent().start,
      end: makeEvent().end,
    };
    expect(isRecurringOccurrence(occurrence)).toBe(true);
  });
});
