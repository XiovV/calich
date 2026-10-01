import { describe, expect, it } from "vitest";
import {
  AGENDA_WINDOW_DAYS,
  agendaEventCountLabel,
  agendaSecondLineText,
  buildAgenda,
  formatAgendaEmptyRunLabel,
  type AgendaDayGroup,
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
    // Only today (Oct 1) and Oct 10 have Occurrences; every day in between
    // and after must collapse into runs.
    const occurrence = makeOccurrence({
      start: new Date(2026, 9, 10, 9, 0),
      end: new Date(2026, 9, 10, 9, 30),
    });
    const items = buildAgenda([occurrence], CALENDARS, NOW, "HH:mm");

    // today (day group) + run Oct 2-9 (empty) + Oct 10 (day group) + run
    // Oct 11-15 (empty, the window's last day is Oct 15 for a 15-day span
    // starting Oct 1).
    expect(items.map((item) => item.kind)).toEqual(["day", "emptyRun", "day", "emptyRun"]);

    const [, firstRun, , secondRun] = items;
    if (firstRun.kind !== "emptyRun" || secondRun.kind !== "emptyRun") throw new Error("expected runs");
    expect(firstRun.startDate).toEqual(new Date(2026, 9, 2));
    expect(firstRun.endDate).toEqual(new Date(2026, 9, 9));
    expect(secondRun.startDate).toEqual(new Date(2026, 9, 11));
    expect(secondRun.endDate).toEqual(new Date(2026, 9, 15));
  });

  it("never folds today into a run even when both neighbors are empty", () => {
    const items = buildAgenda([], CALENDARS, NOW, "HH:mm");
    // today, then one run covering the remaining 14 days.
    expect(items.map((item) => item.kind)).toEqual(["day", "emptyRun"]);
  });

  it("spans exactly AGENDA_WINDOW_DAYS days", () => {
    const items = buildAgenda([], CALENDARS, NOW, "HH:mm");
    expect(dayGroups(items)).toHaveLength(1); // today, its own group even though empty
    const run = items.find((item) => item.kind === "emptyRun");
    if (run?.kind !== "emptyRun") throw new Error("expected a run");
    expect(run.endDate).toEqual(new Date(2026, 9, 1 + AGENDA_WINDOW_DAYS - 1));
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
