import { describe, expect, it } from "vitest";
import { inScopeCalendars } from "./calendarSetScope";
import type { Calendar } from "./calendar";
import type { CalendarSet } from "./calendarSetsApi";

function makeCalendar(id: string): Calendar {
  return { id, name: id, color: "#12809CFF" };
}

function makeCalendarSet(id: number, calendarIds: string[]): CalendarSet {
  return { id, name: "Work", calendarIds };
}

describe("inScopeCalendars", () => {
  it("yields every Calendar when no Active Calendar Set is set", () => {
    const calendars = [makeCalendar("cal-1"), makeCalendar("cal-2")];

    expect(inScopeCalendars(calendars, null)).toEqual(calendars);
  });

  it("yields only an active Set's members", () => {
    const cal1 = makeCalendar("cal-1");
    const cal2 = makeCalendar("cal-2");
    const activeSet = makeCalendarSet(1, ["cal-1"]);

    expect(inScopeCalendars([cal1, cal2], activeSet)).toEqual([cal1]);
  });

  it("omits a member the caller can no longer see", () => {
    const cal1 = makeCalendar("cal-1");
    // cal-revoked is still on the Set's membership list, but has dropped out
    // of the caller's own Calendar list (e.g. a revoked Share) — no cascade
    // has run yet, or the client just hasn't refetched.
    const activeSet = makeCalendarSet(1, ["cal-1", "cal-revoked"]);

    expect(inScopeCalendars([cal1], activeSet)).toEqual([cal1]);
  });

  it("yields nothing for an empty Set", () => {
    const calendars = [makeCalendar("cal-1"), makeCalendar("cal-2")];
    const activeSet = makeCalendarSet(1, []);

    expect(inScopeCalendars(calendars, activeSet)).toEqual([]);
  });
});
