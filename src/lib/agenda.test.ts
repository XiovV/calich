import { isSameDay } from "date-fns";
import { describe, expect, it } from "vitest";
import {
  AGENDA_WINDOW_DAYS,
  agendaDayLabelPrefix,
  agendaEventCountLabel,
  agendaSearchLimit,
  agendaSecondLineText,
  buildAgenda,
  findAgendaItemForDate,
  formatAgendaEmptyRunLabel,
  formatAgendaEndOfListLabel,
  type AgendaDayGroup,
  type AgendaItem,
} from "./agenda";
import type { Calendar } from "./calendar";
import type { Event } from "./event";
import type { Occurrence } from "./occurrence";

const NOW = new Date(2026, 9, 1, 9, 0); // Thu, Oct 1, 2026, 09:00

const WORK_CALENDAR: Calendar = { id: "cal-work", name: "Work", color: "#3F51B5FF" };
const CALENDARS = [WORK_CALENDAR];

function makeEvent(overrides: Partial<Event> = {}): Event {
  return {
    id: "evt-1",
    calendarId: "cal-work",
    title: "Standup",
    start: new Date(2026, 9, 1, 10, 0),
    end: new Date(2026, 9, 1, 10, 30),
    ...overrides,
  };
}

function makeOccurrence(overrides: Partial<Event> = {}): Occurrence {
  const event = makeEvent(overrides);
  return { event, start: event.start, end: event.end };
}

function dayGroups(items: ReturnType<typeof buildAgenda>): AgendaDayGroup[] {
  return items.filter((item): item is AgendaDayGroup => item.kind === "day");
}

function findDay(items: ReturnType<typeof buildAgenda>, date: Date): AgendaDayGroup | undefined {
  return dayGroups(items).find((group) => isSameDay(group.date, date));
}

describe("buildAgenda", () => {
  it("groups an Occurrence under the day its start falls on", () => {
    const occurrence = makeOccurrence();
    const items = buildAgenda([occurrence], CALENDARS, NOW, "HH:mm");

    const today = dayGroups(items).find((group) => group.isToday);
    expect(today?.rows).toHaveLength(1);
    expect(today?.rows[0].occurrence).toBe(occurrence);
  });

  it("always includes today as its own group, even with nothing scheduled", () => {
    const items = buildAgenda([], CALENDARS, NOW, "HH:mm");
    const today = dayGroups(items).find((group) => group.isToday);
    expect(today).toBeDefined();
    expect(today?.rows).toEqual([]);
    expect(today?.eventCount).toBe(0);
  });

  it("collapses a run of consecutive empty days into one item", () => {
    // Today (Oct 1), Oct 10 and Oct 15 (the window's last day) have
    // Occurrences; every day in between must collapse into runs. Closing the
    // window with content on its last day keeps this test about ordinary
    // in-window collapsing rather than #336's load-through, which is
    // covered separately below.
    const occurrences = [
      makeOccurrence({
        id: "middle",
        start: new Date(2026, 9, 10, 9, 0),
        end: new Date(2026, 9, 10, 9, 30),
      }),
      makeOccurrence({
        id: "closer",
        start: new Date(2026, 9, 15, 9, 0),
        end: new Date(2026, 9, 15, 9, 30),
      }),
    ];
    const items = buildAgenda(occurrences, CALENDARS, NOW, "HH:mm");

    // today (day group) + run Oct 2-9 (empty) + Oct 10 (day group) + run
    // Oct 11-14 (empty) + Oct 15 (day group, the window's last day).
    expect(items.map((item) => item.kind)).toEqual(["day", "emptyRun", "day", "emptyRun", "day"]);

    const [, firstRun, , secondRun] = items;
    if (firstRun.kind !== "emptyRun" || secondRun.kind !== "emptyRun") throw new Error("expected runs");
    expect(firstRun.startDate).toEqual(new Date(2026, 9, 2));
    expect(firstRun.endDate).toEqual(new Date(2026, 9, 9));
    expect(secondRun.startDate).toEqual(new Date(2026, 9, 11));
    expect(secondRun.endDate).toEqual(new Date(2026, 9, 14));
  });

  it("never folds today into a run even when both neighbors are empty", () => {
    // Closing the window with content on its last day isolates this test's
    // own rule from #336's load-through (covered separately below).
    const closer = makeOccurrence({
      start: new Date(2026, 9, 15, 9, 0),
      end: new Date(2026, 9, 15, 9, 30),
    });
    const items = buildAgenda([closer], CALENDARS, NOW, "HH:mm");
    // today, then one run covering the days between, then the closer.
    expect(items.map((item) => item.kind)).toEqual(["day", "emptyRun", "day"]);
  });

  it("defaults the window to AGENDA_WINDOW_DAYS", () => {
    const occurrence = makeOccurrence();
    const withDefault = buildAgenda([occurrence], CALENDARS, NOW, "HH:mm");
    const withExplicitWindow = buildAgenda([occurrence], CALENDARS, NOW, "HH:mm", AGENDA_WINDOW_DAYS);
    expect(withDefault).toEqual(withExplicitWindow);
  });

  it("orders all-day rows before timed rows on the same day", () => {
    const timed = makeOccurrence({
      id: "timed",
      start: new Date(2026, 9, 1, 8, 0),
      end: new Date(2026, 9, 1, 8, 30),
    });
    const allDay = makeOccurrence({
      id: "all-day",
      allDay: true,
      start: new Date(2026, 9, 1, 0, 0),
      end: new Date(2026, 9, 2, 0, 0),
    });
    const items = buildAgenda([timed, allDay], CALENDARS, NOW, "HH:mm");
    const today = dayGroups(items).find((group) => group.isToday);
    expect(today?.rows.map((row) => row.occurrence.event.id)).toEqual(["all-day", "timed"]);
  });

  it("orders timed rows by start then end", () => {
    const later = makeOccurrence({
      id: "later",
      start: new Date(2026, 9, 1, 11, 0),
      end: new Date(2026, 9, 1, 11, 30),
    });
    const shorter = makeOccurrence({
      id: "shorter",
      start: new Date(2026, 9, 1, 10, 0),
      end: new Date(2026, 9, 1, 10, 15),
    });
    const longer = makeOccurrence({
      id: "longer",
      start: new Date(2026, 9, 1, 10, 0),
      end: new Date(2026, 9, 1, 10, 45),
    });
    const items = buildAgenda([later, longer, shorter], CALENDARS, NOW, "HH:mm");
    const today = dayGroups(items).find((group) => group.isToday);
    expect(today?.rows.map((row) => row.occurrence.event.id)).toEqual(["shorter", "longer", "later"]);
  });

  it("labels an all-day row 'All day' and a timed row with its formatted range", () => {
    const allDay = makeOccurrence({ id: "all-day", allDay: true });
    const timed = makeOccurrence({
      id: "timed",
      start: new Date(2026, 9, 1, 14, 0),
      end: new Date(2026, 9, 1, 14, 30),
    });
    const items = buildAgenda([allDay, timed], CALENDARS, NOW, "HH:mm");
    const today = dayGroups(items).find((group) => group.isToday);
    const byId = Object.fromEntries(today!.rows.map((row) => [row.occurrence.event.id, row]));
    expect(byId["all-day"].timeLabel).toBe("All day");
    expect(byId["timed"].timeLabel).toBe("14:00 – 14:30");
  });

  it("formats the time label using the given time pattern", () => {
    const occurrence = makeOccurrence({
      start: new Date(2026, 9, 1, 14, 0),
      end: new Date(2026, 9, 1, 14, 30),
    });
    const items = buildAgenda([occurrence], CALENDARS, NOW, "h:mm a");
    const today = dayGroups(items).find((group) => group.isToday);
    expect(today?.rows[0].timeLabel).toBe("2:00 PM – 2:30 PM");
  });

  it("resolves the row's Calendar name and color from the Calendar store", () => {
    const occurrence = makeOccurrence();
    const items = buildAgenda([occurrence], CALENDARS, NOW, "HH:mm");
    const today = dayGroups(items).find((group) => group.isToday);
    expect(today?.rows[0].calendarName).toBe("Work");
    expect(today?.rows[0].color).toBe("#3F51B5FF");
  });

  it("falls back to the Event's own calendarName/calendarColor for an Attendee-only Event", () => {
    const occurrence = makeOccurrence({
      calendarId: "cal-unknown",
      calendarName: "Someone else's calendar",
      calendarColor: "#E2483DFF",
    });
    const items = buildAgenda([occurrence], CALENDARS, NOW, "HH:mm");
    const today = dayGroups(items).find((group) => group.isToday);
    expect(today?.rows[0].calendarName).toBe("Someone else's calendar");
    expect(today?.rows[0].color).toBe("#E2483DFF");
  });

  it("flags hasConferenceUrl only when the Event carries a Conference URL", () => {
    const withConference = makeOccurrence({ id: "with", conferenceUrl: "https://meet.example/abc" });
    const without = makeOccurrence({ id: "without" });
    const items = buildAgenda([withConference, without], CALENDARS, NOW, "HH:mm");
    const today = dayGroups(items).find((group) => group.isToday);
    const byId = Object.fromEntries(today!.rows.map((row) => [row.occurrence.event.id, row]));
    expect(byId.with.hasConferenceUrl).toBe(true);
    expect(byId.without.hasConferenceUrl).toBe(false);
  });

  it("counts events per day header", () => {
    const occurrences = [makeOccurrence({ id: "a" }), makeOccurrence({ id: "b" })];
    const items = buildAgenda(occurrences, CALENDARS, NOW, "HH:mm");
    const today = dayGroups(items).find((group) => group.isToday);
    expect(today?.eventCount).toBe(2);
  });

  describe("now-line", () => {
    it("places the now-line before the first row that hasn't started yet (all-upcoming)", () => {
      const first = makeOccurrence({
        id: "first",
        start: new Date(2026, 9, 1, 10, 0),
        end: new Date(2026, 9, 1, 10, 30),
      });
      const second = makeOccurrence({
        id: "second",
        start: new Date(2026, 9, 1, 11, 0),
        end: new Date(2026, 9, 1, 11, 30),
      });
      const items = buildAgenda([first, second], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      // NOW is 09:00 — both rows start after it, so the line sits before index 0.
      expect(today?.nowLine).toEqual({ index: 0, label: "09:00" });
    });

    it("places the now-line at the end of the group once every row has started (all-past)", () => {
      const past = makeOccurrence({
        id: "past",
        start: new Date(2026, 9, 1, 7, 0),
        end: new Date(2026, 9, 1, 7, 30),
      });
      const items = buildAgenda([past], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      expect(today?.nowLine).toEqual({ index: 1, label: "09:00" });
    });

    it("places the now-line between an in-progress row and the ones after it", () => {
      const inProgress = makeOccurrence({
        id: "in-progress",
        start: new Date(2026, 9, 1, 8, 30),
        end: new Date(2026, 9, 1, 9, 30),
      });
      const upcoming = makeOccurrence({
        id: "upcoming",
        start: new Date(2026, 9, 1, 11, 0),
        end: new Date(2026, 9, 1, 11, 30),
      });
      const items = buildAgenda([inProgress, upcoming], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      expect(today?.rows.map((row) => row.occurrence.event.id)).toEqual(["in-progress", "upcoming"]);
      expect(today?.nowLine).toEqual({ index: 1, label: "09:00" });
    });

    it("sits in the empty group when today has nothing scheduled", () => {
      const items = buildAgenda([], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      expect(today?.rows).toEqual([]);
      expect(today?.nowLine).toEqual({ index: 0, label: "09:00" });
    });

    it("formats the label using the given time pattern", () => {
      const items = buildAgenda([], CALENDARS, NOW, "h:mm a");
      const today = dayGroups(items).find((group) => group.isToday);
      expect(today?.nowLine?.label).toBe("9:00 AM");
    });

    it("is null for non-today groups", () => {
      const occurrence = makeOccurrence({
        start: new Date(2026, 9, 10, 9, 0),
        end: new Date(2026, 9, 10, 9, 30),
      });
      const items = buildAgenda([occurrence], CALENDARS, NOW, "HH:mm");
      const futureDay = dayGroups(items).find((group) => !group.isToday && group.rows.length > 0);
      expect(futureDay?.nowLine).toBeNull();
    });
  });

  describe("dimming", () => {
    it("dims a row whose end has passed", () => {
      const past = makeOccurrence({
        id: "past",
        start: new Date(2026, 9, 1, 7, 0),
        end: new Date(2026, 9, 1, 7, 30),
      });
      const items = buildAgenda([past], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      expect(today?.rows[0].dimmed).toBe(true);
    });

    it("does not dim an in-progress row (started, not yet ended)", () => {
      const inProgress = makeOccurrence({
        id: "in-progress",
        start: new Date(2026, 9, 1, 8, 30),
        end: new Date(2026, 9, 1, 9, 30),
      });
      const items = buildAgenda([inProgress], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      expect(today?.rows[0].dimmed).toBe(false);
    });

    it("does not dim an upcoming row", () => {
      const upcoming = makeOccurrence({
        id: "upcoming",
        start: new Date(2026, 9, 1, 11, 0),
        end: new Date(2026, 9, 1, 11, 30),
      });
      const items = buildAgenda([upcoming], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      expect(today?.rows[0].dimmed).toBe(false);
    });

    it("never dims today's all-day row, however long ago it started", () => {
      const allDay = makeOccurrence({
        id: "all-day",
        allDay: true,
        start: new Date(2026, 9, 1, 0, 0),
        end: new Date(2026, 9, 2, 0, 0),
      });
      const items = buildAgenda([allDay], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      expect(today?.rows[0].dimmed).toBe(false);
    });
  });

  describe("overlaps (#333)", () => {
    it("flags the second of two overlapping rows, not the first", () => {
      const first = makeOccurrence({
        id: "first",
        start: new Date(2026, 9, 1, 10, 0),
        end: new Date(2026, 9, 1, 11, 0),
      });
      const second = makeOccurrence({
        id: "second",
        start: new Date(2026, 9, 1, 10, 30),
        end: new Date(2026, 9, 1, 11, 30),
      });
      const items = buildAgenda([first, second], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      const byId = Object.fromEntries(today!.rows.map((row) => [row.occurrence.event.id, row]));
      expect(byId.first.overlaps).toBe(false);
      expect(byId.second.overlaps).toBe(true);
    });

    it("measures a three-row cluster against the latest end so far, not the row directly above", () => {
      // "wide" runs 10:00-12:00. "narrow" runs 10:30-10:45, nested entirely
      // inside "wide". "after" starts at 11:30 — before "wide"'s end (12:00)
      // but after "narrow"'s end (10:45), so it must be measured against the
      // latest end so far (wide's), not the row directly above it (narrow's).
      const wide = makeOccurrence({
        id: "wide",
        start: new Date(2026, 9, 1, 10, 0),
        end: new Date(2026, 9, 1, 12, 0),
      });
      const narrow = makeOccurrence({
        id: "narrow",
        start: new Date(2026, 9, 1, 10, 30),
        end: new Date(2026, 9, 1, 10, 45),
      });
      const after = makeOccurrence({
        id: "after",
        start: new Date(2026, 9, 1, 11, 30),
        end: new Date(2026, 9, 1, 12, 30),
      });
      const items = buildAgenda([wide, narrow, after], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      expect(today?.rows.map((row) => row.occurrence.event.id)).toEqual(["wide", "narrow", "after"]);
      const byId = Object.fromEntries(today!.rows.map((row) => [row.occurrence.event.id, row]));
      expect(byId.wide.overlaps).toBe(false);
      expect(byId.narrow.overlaps).toBe(true);
      expect(byId.after.overlaps).toBe(true);
    });

    it("does not flag back-to-back rows whose start equals the previous end", () => {
      const first = makeOccurrence({
        id: "first",
        start: new Date(2026, 9, 1, 10, 0),
        end: new Date(2026, 9, 1, 11, 0),
      });
      const second = makeOccurrence({
        id: "second",
        start: new Date(2026, 9, 1, 11, 0),
        end: new Date(2026, 9, 1, 12, 0),
      });
      const items = buildAgenda([first, second], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      const byId = Object.fromEntries(today!.rows.map((row) => [row.occurrence.event.id, row]));
      expect(byId.first.overlaps).toBe(false);
      expect(byId.second.overlaps).toBe(false);
    });

    it("flags a Free Event overlapping another row — Busy plays no part", () => {
      const busy = makeOccurrence({
        id: "busy",
        start: new Date(2026, 9, 1, 10, 0),
        end: new Date(2026, 9, 1, 11, 0),
      });
      const free = makeOccurrence({
        id: "free",
        busy: false,
        start: new Date(2026, 9, 1, 10, 30),
        end: new Date(2026, 9, 1, 11, 30),
      });
      const items = buildAgenda([busy, free], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      const byId = Object.fromEntries(today!.rows.map((row) => [row.occurrence.event.id, row]));
      expect(byId.free.overlaps).toBe(true);
    });

    it("never flags an all-day row, and all-day rows never count toward an overlap", () => {
      const allDay = makeOccurrence({
        id: "all-day",
        allDay: true,
        start: new Date(2026, 9, 1, 0, 0),
        end: new Date(2026, 9, 2, 0, 0),
      });
      const timed = makeOccurrence({
        id: "timed",
        start: new Date(2026, 9, 1, 10, 0),
        end: new Date(2026, 9, 1, 11, 0),
      });
      const items = buildAgenda([allDay, timed], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      const byId = Object.fromEntries(today!.rows.map((row) => [row.occurrence.event.id, row]));
      expect(byId["all-day"].overlaps).toBe(false);
      // "timed" is the first timed row of the day — nothing timed precedes
      // it, so it is unflagged regardless of the all-day row sitting above it.
      expect(byId.timed.overlaps).toBe(false);
    });
  });

  describe("gaps (#334)", () => {
    function gapLabels(group: AgendaDayGroup | undefined): string[] {
      return (group?.gaps ?? []).map((gap) => gap.label);
    }

    it("emits a plain Gap between two rows with a stretch between them", () => {
      const first = makeOccurrence({
        id: "first",
        start: new Date(2026, 9, 1, 10, 0),
        end: new Date(2026, 9, 1, 10, 30),
      });
      const second = makeOccurrence({
        id: "second",
        start: new Date(2026, 9, 1, 11, 30),
        end: new Date(2026, 9, 1, 12, 0),
      });
      // NOW is 09:00 — before both rows, so this Gap (10:30-11:30) has
      // nothing to do with "now".
      const items = buildAgenda([first, second], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      const byId = Object.fromEntries(today!.rows.map((row, index) => [row.occurrence.event.id, index]));
      expect(gapLabels(today)).toEqual(["1h free"]);
      expect(today?.gaps[0].beforeIndex).toBe(byId.second);
    });

    it("hides a Gap under the 30-minute floor and shows one at exactly 30 minutes", () => {
      const first = makeOccurrence({
        id: "first",
        start: new Date(2026, 9, 1, 10, 0),
        end: new Date(2026, 9, 1, 10, 30),
      });
      const secondAt29 = makeOccurrence({
        id: "second",
        start: new Date(2026, 9, 1, 10, 59),
        end: new Date(2026, 9, 1, 11, 29),
      });
      const hidden = buildAgenda([first, secondAt29], CALENDARS, NOW, "HH:mm");
      expect(gapLabels(dayGroups(hidden).find((group) => group.isToday))).toEqual([]);

      const secondAt30 = makeOccurrence({
        id: "second",
        start: new Date(2026, 9, 1, 11, 0),
        end: new Date(2026, 9, 1, 11, 30),
      });
      const shown = buildAgenda([first, secondAt30], CALENDARS, NOW, "HH:mm");
      expect(gapLabels(dayGroups(shown).find((group) => group.isToday))).toEqual(["30m free"]);
    });

    it("merges across overlapping rows, starting the Gap at the latest end so far", () => {
      // "wide" runs 10:00-12:00, "narrow" is nested inside it (10:30-10:45).
      // The Gap after them must start at 12:00 (wide's end), not 10:45.
      const wide = makeOccurrence({
        id: "wide",
        start: new Date(2026, 9, 1, 10, 0),
        end: new Date(2026, 9, 1, 12, 0),
      });
      const narrow = makeOccurrence({
        id: "narrow",
        start: new Date(2026, 9, 1, 10, 30),
        end: new Date(2026, 9, 1, 10, 45),
      });
      const after = makeOccurrence({
        id: "after",
        start: new Date(2026, 9, 1, 12, 45),
        end: new Date(2026, 9, 1, 13, 0),
      });
      const items = buildAgenda([wide, narrow, after], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      expect(gapLabels(today)).toEqual(["45m free"]);
    });

    it("treats a Free Event as bounding a Gap exactly like a Busy one", () => {
      const free = makeOccurrence({
        id: "free",
        busy: false,
        start: new Date(2026, 9, 1, 10, 0),
        end: new Date(2026, 9, 1, 10, 30),
      });
      const next = makeOccurrence({
        id: "next",
        start: new Date(2026, 9, 1, 11, 0),
        end: new Date(2026, 9, 1, 11, 30),
      });
      const items = buildAgenda([free, next], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      expect(gapLabels(today)).toEqual(["30m free"]);
    });

    it("never lets an all-day row bound a Gap", () => {
      const allDay = makeOccurrence({
        id: "all-day",
        allDay: true,
        start: new Date(2026, 9, 1, 0, 0),
        end: new Date(2026, 9, 2, 0, 0),
      });
      const first = makeOccurrence({
        id: "first",
        start: new Date(2026, 9, 1, 10, 0),
        end: new Date(2026, 9, 1, 10, 30),
      });
      const second = makeOccurrence({
        id: "second",
        start: new Date(2026, 9, 1, 11, 0),
        end: new Date(2026, 9, 1, 11, 30),
      });
      const items = buildAgenda([allDay, first, second], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      // Only the Gap between "first" and "second" — the all-day row, though
      // it sits above "first", never opens or closes one of its own.
      expect(gapLabels(today)).toEqual(["30m free"]);
    });

    it("never shows a Gap before a day's first row or after its last", () => {
      const only = makeOccurrence({
        id: "only",
        start: new Date(2026, 9, 1, 10, 0),
        end: new Date(2026, 9, 1, 10, 30),
      });
      const items = buildAgenda([only], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      // NOW (09:00) is well before "only" and the day runs well past its
      // 10:30 end, yet neither edge produces a Gap.
      expect(gapLabels(today)).toEqual([]);
    });

    it("shows the 'free until' Gap under the now-line when nothing is in progress", () => {
      const next = makeOccurrence({
        id: "next",
        start: new Date(2026, 9, 1, 11, 24),
        end: new Date(2026, 9, 1, 11, 54),
      });
      // An earlier, already-finished row establishes the day's first bound
      // so the Gap isn't suppressed as "before the day's first row".
      const earlier = makeOccurrence({
        id: "earlier",
        start: new Date(2026, 9, 1, 7, 0),
        end: new Date(2026, 9, 1, 7, 30),
      });
      const items = buildAgenda([earlier, next], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      // NOW is 09:00, "next" starts 11:24 — 2h 24m free until 11:24.
      expect(gapLabels(today)).toEqual(["2h 24m free until 11:24"]);
      expect(today?.nowLine?.index).toBe(1);
      expect(today?.gaps[0].beforeIndex).toBe(1);
    });

    it("shows no 'free until' Gap while something is in progress, running from its end as usual", () => {
      const inProgress = makeOccurrence({
        id: "in-progress",
        start: new Date(2026, 9, 1, 8, 30),
        end: new Date(2026, 9, 1, 9, 30),
      });
      const next = makeOccurrence({
        id: "next",
        start: new Date(2026, 9, 1, 11, 0),
        end: new Date(2026, 9, 1, 11, 30),
      });
      const items = buildAgenda([inProgress, next], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      // The Gap runs from the in-progress row's end (09:30) to "next"
      // (11:00) — plain wording, not "until", since it isn't anchored at now.
      expect(gapLabels(today)).toEqual(["1h 30m free"]);
    });
  });

  describe("midnight-crossing and multi-day Occurrences (#335)", () => {
    it("has no Day label for a same-day Occurrence", () => {
      const occurrence = makeOccurrence();
      const items = buildAgenda([occurrence], CALENDARS, NOW, "HH:mm");
      const today = dayGroups(items).find((group) => group.isToday);
      expect(today?.rows[0].dayLabel).toBeNull();
    });

    it("splits a two-day overnight Occurrence: full range on day 1, 'Until' on day 2", () => {
      const overnight = makeOccurrence({
        id: "overnight",
        start: new Date(2026, 9, 1, 22, 0), // today, 22:00
        end: new Date(2026, 9, 2, 2, 0), // tomorrow, 02:00
      });
      const items = buildAgenda([overnight], CALENDARS, NOW, "HH:mm");
      const day1 = findDay(items, new Date(2026, 9, 1));
      const day2 = findDay(items, new Date(2026, 9, 2));

      expect(day1?.rows).toHaveLength(1);
      expect(day1?.rows[0].timeLabel).toBe("22:00 – 02:00");
      expect(day1?.rows[0].dayLabel).toBe("Day 1 of 2");
      expect(day1?.eventCount).toBe(1);

      expect(day2?.rows).toHaveLength(1);
      expect(day2?.rows[0].timeLabel).toBe("Until 02:00");
      expect(day2?.rows[0].dayLabel).toBe("Day 2 of 2");
      expect(day2?.eventCount).toBe(1);
    });

    it("splits a three-day timed Occurrence across all three days with Day N of M labels", () => {
      const conference = makeOccurrence({
        id: "conference",
        start: new Date(2026, 9, 1, 9, 0),
        end: new Date(2026, 9, 3, 17, 0),
      });
      const items = buildAgenda([conference], CALENDARS, NOW, "HH:mm");
      const day1 = findDay(items, new Date(2026, 9, 1));
      const day2 = findDay(items, new Date(2026, 9, 2));
      const day3 = findDay(items, new Date(2026, 9, 3));

      expect(day1?.rows[0].timeLabel).toBe("09:00 – 17:00");
      expect(day1?.rows[0].dayLabel).toBe("Day 1 of 3");

      // The middle day reads "All day" but stays a timed row (#335) — it is
      // not an All-day Event, so it must keep sorting among timed rows.
      expect(day2?.rows[0].timeLabel).toBe("All day");
      expect(day2?.rows[0].dayLabel).toBe("Day 2 of 3");
      expect(day2?.rows[0].allDay).toBe(false);

      expect(day3?.rows[0].timeLabel).toBe("Until 17:00");
      expect(day3?.rows[0].dayLabel).toBe("Day 3 of 3");

      // Header counts include the Occurrence on every day it appears (#335).
      expect(day1?.eventCount).toBe(1);
      expect(day2?.eventCount).toBe(1);
      expect(day3?.eventCount).toBe(1);
    });

    it("sorts a multi-day timed Occurrence's middle-day row among timed rows, not with All-day Events", () => {
      const conference = makeOccurrence({
        id: "conference",
        start: new Date(2026, 9, 1, 9, 0),
        end: new Date(2026, 9, 3, 17, 0),
      });
      const allDayEvent = makeOccurrence({
        id: "all-day",
        allDay: true,
        start: new Date(2026, 9, 2, 0, 0),
        end: new Date(2026, 9, 3, 0, 0),
      });
      const items = buildAgenda([conference, allDayEvent], CALENDARS, NOW, "HH:mm");
      const day2 = findDay(items, new Date(2026, 9, 2));
      expect(day2?.rows.map((row) => row.occurrence.event.id)).toEqual(["all-day", "conference"]);
      expect(day2?.rows[1].timeLabel).toBe("All day");
      expect(day2?.rows[1].allDay).toBe(false);
    });

    it("repeats a three-day All-day Event in every day it touches with Day N of M", () => {
      const trip = makeOccurrence({
        id: "trip",
        allDay: true,
        start: new Date(2026, 9, 1, 0, 0),
        end: new Date(2026, 9, 4, 0, 0), // exclusive end: touches Oct 1-3
      });
      const items = buildAgenda([trip], CALENDARS, NOW, "HH:mm");
      const expectations: [Date, string][] = [
        [new Date(2026, 9, 1), "Day 1 of 3"],
        [new Date(2026, 9, 2), "Day 2 of 3"],
        [new Date(2026, 9, 3), "Day 3 of 3"],
      ];
      for (const [date, dayLabel] of expectations) {
        const group = findDay(items, date);
        expect(group?.rows).toHaveLength(1);
        expect(group?.rows[0].timeLabel).toBe("All day");
        expect(group?.rows[0].dayLabel).toBe(dayLabel);
        expect(group?.eventCount).toBe(1);
      }
    });

    it("computes a later day's Gaps and Overlaps from only that day's covered portion", () => {
      // The conference's third-day segment covers Wed 00:00-17:00 (#335): no
      // Gap can precede it (it's the day's first row), and a 10:00 meeting
      // that day starts inside its covered stretch, so it is badged
      // Overlaps rather than separated by a Gap.
      const conference = makeOccurrence({
        id: "conference",
        start: new Date(2026, 9, 1, 9, 0),
        end: new Date(2026, 9, 3, 17, 0),
      });
      const meeting = makeOccurrence({
        id: "meeting",
        start: new Date(2026, 9, 3, 10, 0),
        end: new Date(2026, 9, 3, 10, 30),
      });
      const items = buildAgenda([conference, meeting], CALENDARS, NOW, "HH:mm");
      const day3 = findDay(items, new Date(2026, 9, 3));

      expect(day3?.gaps).toEqual([]);
      const byId = Object.fromEntries(day3!.rows.map((row) => [row.occurrence.event.id, row]));
      expect(byId.conference.overlaps).toBe(false);
      expect(byId.meeting.overlaps).toBe(true);
    });

    it("never dims the overnight Occurrence's first-day row while today, no matter how late", () => {
      const overnight = makeOccurrence({
        id: "overnight",
        start: new Date(2026, 9, 1, 22, 0),
        end: new Date(2026, 9, 2, 2, 0),
      });
      const items = buildAgenda([overnight], CALENDARS, new Date(2026, 9, 1, 23, 30), "HH:mm");
      const day1 = findDay(items, new Date(2026, 9, 1));
      expect(day1?.rows[0].dimmed).toBe(false);
    });

    it("dims the overnight Occurrence's last-day row only once its true end has passed", () => {
      const overnight = makeOccurrence({
        id: "overnight",
        start: new Date(2026, 9, 1, 22, 0),
        end: new Date(2026, 9, 2, 2, 0),
      });
      const stillRunning = buildAgenda([overnight], CALENDARS, new Date(2026, 9, 2, 1, 0), "HH:mm");
      expect(findDay(stillRunning, new Date(2026, 9, 2))?.rows[0].dimmed).toBe(false);

      const ended = buildAgenda([overnight], CALENDARS, new Date(2026, 9, 2, 3, 0), "HH:mm");
      expect(findDay(ended, new Date(2026, 9, 2))?.rows[0].dimmed).toBe(true);
    });
  });

  describe("infinite scroll (#336)", () => {
    it("loads straight through an empty stretch to the next row, past the requested window", () => {
      // Only today and day 40 (well past the 15-day requested window) have
      // Occurrences — the empty stretch between them must still render as a
      // single row rather than stopping at the window boundary.
      const far = makeOccurrence({
        start: new Date(2026, 10, 10, 9, 0), // Nov 10 — 40 days after Oct 1
        end: new Date(2026, 10, 10, 9, 30),
      });
      const items = buildAgenda([far], CALENDARS, NOW, "HH:mm", AGENDA_WINDOW_DAYS);

      expect(items.map((item) => item.kind)).toEqual(["day", "emptyRun", "day"]);
      const [today, run, farDay] = items;
      if (run.kind !== "emptyRun" || farDay.kind !== "day") throw new Error("expected run then day");
      expect(today.kind).toBe("day");
      expect(run.startDate).toEqual(new Date(2026, 9, 2));
      expect(run.endDate).toEqual(new Date(2026, 10, 9));
      expect(farDay.date).toEqual(new Date(2026, 10, 10));
    });

    it("never produces two consecutive empty-run rows, however many requested-window boundaries it crosses", () => {
      // Content at today, day 10 and day 30 spans several 14-day chunks —
      // every empty stretch between them, whether inside or past the
      // requested window, must collapse to exactly one row each.
      const occurrences = [
        makeOccurrence({
          id: "ten",
          start: new Date(2026, 9, 10, 9, 0),
          end: new Date(2026, 9, 10, 9, 30),
        }),
        makeOccurrence({
          id: "thirty",
          start: new Date(2026, 9, 30, 9, 0),
          end: new Date(2026, 9, 30, 9, 30),
        }),
      ];
      const items = buildAgenda(occurrences, CALENDARS, NOW, "HH:mm", 45);

      // Nothing lies beyond day 30 within the year, so the run past it (also
      // crossing the 45-day window boundary) loads through all the way to
      // the end-of-list item rather than stopping as a dangling run.
      expect(items.map((item) => item.kind)).toEqual([
        "day",
        "emptyRun",
        "day",
        "emptyRun",
        "day",
        "endOfList",
      ]);
      for (let i = 0; i < items.length - 1; i++) {
        expect(items[i].kind === "emptyRun" && items[i + 1].kind === "emptyRun").toBe(false);
      }
    });

    it("renders identically whether a later row was already in the requested window or had to be loaded through to", () => {
      // Day 10 sits inside both an 11-day and a 20-day window; day 25 sits
      // past both, so both windows must load through the same stretch to
      // reach it. Either window size should land on the exact same list.
      const occurrences = [
        makeOccurrence({
          id: "ten",
          start: new Date(2026, 9, 10, 9, 0),
          end: new Date(2026, 9, 10, 9, 30),
        }),
        makeOccurrence({
          id: "closer",
          start: new Date(2026, 9, 25, 9, 0),
          end: new Date(2026, 9, 25, 9, 30),
        }),
      ];
      const narrow = buildAgenda(occurrences, CALENDARS, NOW, "HH:mm", 11);
      const wide = buildAgenda(occurrences, CALENDARS, NOW, "HH:mm", 20);
      expect(narrow).toEqual(wide);
    });

    it("ends the list when nothing lies within a year of the requested window, without a dangling run", () => {
      const items = buildAgenda([], CALENDARS, NOW, "HH:mm", AGENDA_WINDOW_DAYS);

      expect(items.map((item) => item.kind)).toEqual(["day", "endOfList"]);
      const [, endOfList] = items;
      if (endOfList.kind !== "endOfList") throw new Error("expected an end-of-list item");
      // windowEnd (Oct 16, 2026) + 1 year = Oct 16, 2027; the last day
      // actually searched is the day before that.
      expect(endOfList.afterDate).toEqual(new Date(2027, 9, 15));
    });

    it("keeps loading when something eventually lies within the year, rather than ending early", () => {
      const justInTime = makeOccurrence({
        start: new Date(2027, 9, 15, 9, 0), // the last day the lookahead reaches
        end: new Date(2027, 9, 15, 9, 30),
      });
      const items = buildAgenda([justInTime], CALENDARS, NOW, "HH:mm", AGENDA_WINDOW_DAYS);
      expect(items.map((item) => item.kind)).toEqual(["day", "emptyRun", "day"]);
      const last = items[items.length - 1];
      if (last.kind !== "day") throw new Error("expected a day group");
      expect(last.date).toEqual(new Date(2027, 9, 15));
    });
  });
});

describe("agendaEventCountLabel", () => {
  it("pluralizes correctly", () => {
    expect(agendaEventCountLabel(0)).toBe("0 events");
    expect(agendaEventCountLabel(1)).toBe("1 event");
    expect(agendaEventCountLabel(2)).toBe("2 events");
  });
});

describe("agendaSecondLineText", () => {
  it("is just the Calendar name when there is no Location", () => {
    expect(agendaSecondLineText({ calendarName: "Work" })).toBe("Work");
  });

  it("appends the Location when one is set", () => {
    expect(agendaSecondLineText({ calendarName: "Work", location: "Room 2" })).toBe("Work · Room 2");
  });
});

describe("agendaDayLabelPrefix", () => {
  it("is empty for a row with no Day label", () => {
    expect(agendaDayLabelPrefix({ dayLabel: null })).toBe("");
  });

  it("is the Day label followed by a separator", () => {
    expect(agendaDayLabelPrefix({ dayLabel: "Day 2 of 3" })).toBe("Day 2 of 3 ·");
  });
});

describe("formatAgendaEmptyRunLabel", () => {
  it("formats a single-day run with one date", () => {
    const label = formatAgendaEmptyRunLabel({
      kind: "emptyRun",
      startDate: new Date(2026, 9, 3),
      endDate: new Date(2026, 9, 3),
    });
    expect(label).toBe("Sat, Oct 3 · Nothing scheduled");
  });

  it("formats a multi-day run as a range", () => {
    const label = formatAgendaEmptyRunLabel({
      kind: "emptyRun",
      startDate: new Date(2026, 9, 3),
      endDate: new Date(2026, 9, 5),
    });
    expect(label).toBe("Sat, Oct 3 – Mon, Oct 5 · Nothing scheduled");
  });
});

describe("formatAgendaEndOfListLabel", () => {
  it("formats the terminal row's text", () => {
    const label = formatAgendaEndOfListLabel({ kind: "endOfList", afterDate: new Date(2027, 9, 15) });
    expect(label).toBe("Nothing scheduled after Oct 15, 2027");
  });
});

describe("findAgendaItemForDate (#336)", () => {
  function buildSample(): AgendaItem[] {
    const occurrences = [
      makeOccurrence({
        id: "ten",
        start: new Date(2026, 9, 10, 9, 0),
        end: new Date(2026, 9, 10, 9, 30),
      }),
      makeOccurrence({
        id: "closer",
        start: new Date(2026, 9, 15, 9, 0),
        end: new Date(2026, 9, 15, 9, 30),
      }),
    ];
    return buildAgenda(occurrences, CALENDARS, NOW, "HH:mm");
  }

  it("finds a date's own day group", () => {
    const items = buildSample();
    const found = findAgendaItemForDate(items, new Date(2026, 9, 10));
    expect(found?.kind).toBe("day");
  });

  it("finds the collapsed run a date without its own group falls inside", () => {
    const items = buildSample();
    const found = findAgendaItemForDate(items, new Date(2026, 9, 12));
    expect(found?.kind).toBe("emptyRun");
    if (found?.kind !== "emptyRun") throw new Error("expected a run");
    expect(found.startDate <= new Date(2026, 9, 12)).toBe(true);
    expect(found.endDate >= new Date(2026, 9, 12)).toBe(true);
  });

  it("returns undefined for a date outside every rendered item", () => {
    const items = buildSample();
    const found = findAgendaItemForDate(items, new Date(2030, 0, 1));
    expect(found).toBeUndefined();
  });

  it("falls back to the end-of-list item for a date inside the run that was discarded in favor of it", () => {
    // Nothing beyond today, so the list ends in endOfList rather than an
    // emptyRun — a mini-calendar click on, say, Dec 15 (grown into the
    // window but with no row of its own) must still resolve to something
    // scrollable (#336).
    const items = buildAgenda([], CALENDARS, NOW, "HH:mm", 76); // windowDays reaching Dec 15
    const last = items[items.length - 1];
    if (last.kind !== "endOfList") throw new Error("expected an end-of-list item");

    const found = findAgendaItemForDate(items, new Date(2026, 11, 15));
    expect(found).toBe(last);
  });
});

describe("agendaSearchLimit (#336)", () => {
  it("is one year past the requested window's end", () => {
    expect(agendaSearchLimit(NOW, AGENDA_WINDOW_DAYS)).toEqual(new Date(2027, 9, 16));
  });

  it("matches the endOfList item's afterDate plus one day, for the same window", () => {
    const items = buildAgenda([], CALENDARS, NOW, "HH:mm", AGENDA_WINDOW_DAYS);
    const last = items[items.length - 1];
    if (last.kind !== "endOfList") throw new Error("expected an end-of-list item");
    const searchLimit = agendaSearchLimit(NOW, AGENDA_WINDOW_DAYS);
    expect(last.afterDate.getTime()).toBe(searchLimit.getTime() - 86_400_000);
  });
});
