import { fromZonedTime } from "date-fns-tz";
import { describe, expect, it, vi } from "vitest";
import {
  compareTasksWithinBucket,
  bucketTasks,
  taskBucket,
  taskDeadlineFallsOnDay,
  taskFallsOnMonthDay,
  taskPlacement,
  type PlaceableTask,
  type SchedulableTask,
} from "./taskScheduling";

// Pin the ambient zone to one that observes DST, and away from the zones
// explicit in the test bodies below — every assertion here passes a
// `viewerZone` explicitly, so a test that only passes with the ambient zone
// left at its default would be proof the function ignores its own
// parameter (mirroring expandOccurrences.test.ts's own posture).
vi.stubEnv("TZ", "Pacific/Auckland");

function makeTask(overrides: Partial<SchedulableTask> = {}): SchedulableTask {
  return {
    due: null,
    start: null,
    priority: 0,
    createdAt: new Date(2026, 0, 1),
    ...overrides,
  };
}

describe("taskBucket", () => {
  it("is noDate when neither Deadline nor Time block is present", () => {
    const now = fromZonedTime(new Date(2026, 8, 15, 12, 0), "America/New_York");
    expect(taskBucket(makeTask(), now, "America/New_York")).toBe("noDate");
  });

  it("is today when the Deadline falls on the same calendar day as now", () => {
    const due = fromZonedTime(new Date(2026, 8, 15, 0, 0), "America/New_York");
    const now = fromZonedTime(new Date(2026, 8, 15, 18, 30), "America/New_York");
    expect(taskBucket(makeTask({ due }), now, "America/New_York")).toBe("today");
  });

  it("is overdue when the Deadline is before today", () => {
    const due = fromZonedTime(new Date(2026, 8, 14, 0, 0), "America/New_York");
    const now = fromZonedTime(new Date(2026, 8, 15, 9, 0), "America/New_York");
    expect(taskBucket(makeTask({ due }), now, "America/New_York")).toBe("overdue");
  });

  it("is upcoming when the Deadline is after today", () => {
    const due = fromZonedTime(new Date(2026, 8, 20, 0, 0), "America/New_York");
    const now = fromZonedTime(new Date(2026, 8, 15, 9, 0), "America/New_York");
    expect(taskBucket(makeTask({ due }), now, "America/New_York")).toBe("upcoming");
  });

  it("falls back to the Time block's start when there is no Deadline", () => {
    const start = fromZonedTime(new Date(2026, 8, 15, 14, 0), "America/New_York");
    const now = fromZonedTime(new Date(2026, 8, 15, 9, 0), "America/New_York");
    expect(taskBucket(makeTask({ start }), now, "America/New_York")).toBe("today");
  });

  it("uses noDate only when neither axis is present — an overdue Time block with no Deadline still buckets from it", () => {
    const start = fromZonedTime(new Date(2026, 8, 10, 14, 0), "America/New_York");
    const now = fromZonedTime(new Date(2026, 8, 15, 9, 0), "America/New_York");
    expect(taskBucket(makeTask({ start }), now, "America/New_York")).toBe("overdue");
  });

  // ADR-0083 / #311 AC: a Deadline always beats a Time block, even though
  // Time blocks themselves don't exist until a later ticket.
  it("prefers a Friday Deadline over a Tuesday Time block: upcoming, not today", () => {
    const now = fromZonedTime(new Date(2026, 8, 15, 9, 0), "America/New_York"); // Tuesday
    const start = fromZonedTime(new Date(2026, 8, 15, 14, 0), "America/New_York"); // Tuesday, blocked
    const due = fromZonedTime(new Date(2026, 8, 18, 0, 0), "America/New_York"); // Friday, due
    expect(taskBucket(makeTask({ due, start }), now, "America/New_York")).toBe("upcoming");
  });

  it("treats a Deadline just before midnight as overdue the instant the calendar day turns", () => {
    const due = fromZonedTime(new Date(2026, 8, 14, 23, 59), "America/New_York");
    const stillSameDay = fromZonedTime(new Date(2026, 8, 14, 23, 59, 30), "America/New_York");
    const nextDay = fromZonedTime(new Date(2026, 8, 15, 0, 0, 1), "America/New_York");
    expect(taskBucket(makeTask({ due }), stillSameDay, "America/New_York")).toBe("today");
    expect(taskBucket(makeTask({ due }), nextDay, "America/New_York")).toBe("overdue");
  });

  it("holds the calendar day across a DST transition (America/New_York springs forward 2026-03-08)", () => {
    // 2026-03-08 02:30 America/New_York doesn't exist (clocks skip 02:00 ->
    // 03:00); a Deadline set the day before must still read as due "today"
    // for a `now` later that same DST-transition day, not shift a day either
    // way because of the offset change mid-day.
    const due = fromZonedTime(new Date(2026, 2, 8, 1, 0), "America/New_York");
    const now = fromZonedTime(new Date(2026, 2, 8, 10, 0), "America/New_York");
    expect(taskBucket(makeTask({ due }), now, "America/New_York")).toBe("today");
  });

  it("derives 'today' from the current Viewer zone, which can read a different calendar day than the zone the Deadline was set in", () => {
    // A single instant: 2026-09-15 23:30 America/New_York is already
    // 2026-09-16 in Pacific/Auckland (many hours ahead). The Deadline is due
    // 2026-09-15 wherever it's read from a fixed instant, so viewed from a
    // Viewer zone far ahead, "today" has already turned into the 16th.
    const due = fromZonedTime(new Date(2026, 8, 15, 0, 0), "America/New_York");
    const now = fromZonedTime(new Date(2026, 8, 15, 23, 30), "America/New_York");

    expect(taskBucket(makeTask({ due }), now, "America/New_York")).toBe("today");
    expect(taskBucket(makeTask({ due }), now, "Pacific/Auckland")).toBe("overdue");
  });
});

describe("compareTasksWithinBucket", () => {
  it("orders an earlier Deadline before a later one", () => {
    const earlier = makeTask({ due: new Date(2026, 8, 10) });
    const later = makeTask({ due: new Date(2026, 8, 20) });
    expect(compareTasksWithinBucket(earlier, later)).toBeLessThan(0);
    expect(compareTasksWithinBucket(later, earlier)).toBeGreaterThan(0);
  });

  it("orders an undated Task after any dated one", () => {
    const dated = makeTask({ due: new Date(2026, 8, 10) });
    const undated = makeTask({ due: null });
    expect(compareTasksWithinBucket(dated, undated)).toBeLessThan(0);
    expect(compareTasksWithinBucket(undated, dated)).toBeGreaterThan(0);
  });

  it("breaks a Deadline tie by Priority descending (High before Medium before Low before None)", () => {
    const due = new Date(2026, 8, 10);
    const high = makeTask({ due, priority: 1 });
    const medium = makeTask({ due, priority: 5 });
    const low = makeTask({ due, priority: 9 });
    const none = makeTask({ due, priority: 0 });
    expect(compareTasksWithinBucket(high, medium)).toBeLessThan(0);
    expect(compareTasksWithinBucket(medium, low)).toBeLessThan(0);
    expect(compareTasksWithinBucket(low, none)).toBeLessThan(0);
  });

  it("breaks a Deadline-and-Priority tie by creation time ascending", () => {
    const due = new Date(2026, 8, 10);
    const createdFirst = makeTask({ due, priority: 5, createdAt: new Date(2026, 0, 1) });
    const createdSecond = makeTask({ due, priority: 5, createdAt: new Date(2026, 0, 2) });
    expect(compareTasksWithinBucket(createdFirst, createdSecond)).toBeLessThan(0);
  });

  it("is a true tie (0) when Deadline, Priority and creation time all match", () => {
    const due = new Date(2026, 8, 10);
    const createdAt = new Date(2026, 0, 1);
    const a = makeTask({ due, priority: 5, createdAt });
    const b = makeTask({ due, priority: 5, createdAt });
    expect(compareTasksWithinBucket(a, b)).toBe(0);
  });
});

describe("taskPlacement", () => {
  function placeable(overrides: Partial<PlaceableTask> = {}): PlaceableTask {
    return { start: null, due: null, ...overrides };
  }

  it("is panel when neither a Time block nor a Deadline is present", () => {
    expect(taskPlacement(placeable())).toBe("panel");
  });

  it("is allDay when a Deadline is present and there is no Time block", () => {
    expect(taskPlacement(placeable({ due: new Date(2026, 8, 15) }))).toBe("allDay");
  });

  it("is grid when a Time block is present and there is no Deadline", () => {
    expect(taskPlacement(placeable({ start: new Date(2026, 8, 15, 14, 0) }))).toBe("grid");
  });

  // ADR-0083: a Time block wins outright — a Task due Friday but blocked
  // Tuesday still renders on the grid, never doubled into the all-day lane.
  it("is grid when both a Time block and a Deadline are present — the Time block wins", () => {
    expect(
      taskPlacement(
        placeable({ start: new Date(2026, 8, 15, 14, 0), due: new Date(2026, 8, 18) }),
      ),
    ).toBe("grid");
  });
});

describe("taskDeadlineFallsOnDay", () => {
  it("is true when the Deadline falls on the same calendar day as day, in viewerZone", () => {
    const due = fromZonedTime(new Date(2026, 8, 15, 22, 0), "America/New_York");
    const day = fromZonedTime(new Date(2026, 8, 15, 0, 0), "America/New_York");
    expect(taskDeadlineFallsOnDay(due, day, "America/New_York")).toBe(true);
  });

  it("is false when the Deadline falls on a different calendar day than day", () => {
    const due = fromZonedTime(new Date(2026, 8, 15, 22, 0), "America/New_York");
    const day = fromZonedTime(new Date(2026, 8, 16, 0, 0), "America/New_York");
    expect(taskDeadlineFallsOnDay(due, day, "America/New_York")).toBe(false);
  });

  it("re-derives which day the Deadline falls on from viewerZone, not the zone it was set in", () => {
    // Fixed instants, mirroring taskBucket's own cross-zone test: only the
    // viewerZone argument varies below. 2026-09-15 23:30 America/New_York is
    // already 2026-09-16 in Pacific/Auckland, so the same due/day pair reads
    // as the same calendar day from New York and as different days from
    // Auckland.
    const due = fromZonedTime(new Date(2026, 8, 15, 0, 0), "America/New_York");
    const day = fromZonedTime(new Date(2026, 8, 15, 23, 30), "America/New_York");

    expect(taskDeadlineFallsOnDay(due, day, "America/New_York")).toBe(true);
    expect(taskDeadlineFallsOnDay(due, day, "Pacific/Auckland")).toBe(false);
  });
});

describe("taskFallsOnMonthDay", () => {
  function placeable(overrides: Partial<PlaceableTask> = {}): PlaceableTask {
    return { start: null, due: null, ...overrides };
  }

  it("is false for a panel-only Task with neither a Time block nor a Deadline", () => {
    const day = fromZonedTime(new Date(2026, 8, 15, 0, 0), "America/New_York");
    expect(taskFallsOnMonthDay(placeable(), day, "America/New_York")).toBe(false);
  });

  it("is true when the Time block's start falls on day, in viewerZone", () => {
    const start = fromZonedTime(new Date(2026, 8, 15, 14, 0), "America/New_York");
    const day = fromZonedTime(new Date(2026, 8, 15, 0, 0), "America/New_York");
    expect(taskFallsOnMonthDay(placeable({ start }), day, "America/New_York")).toBe(true);
  });

  it("is true when the Deadline falls on day and there is no Time block", () => {
    const due = fromZonedTime(new Date(2026, 8, 15, 0, 0), "America/New_York");
    const day = fromZonedTime(new Date(2026, 8, 15, 12, 0), "America/New_York");
    expect(taskFallsOnMonthDay(placeable({ due }), day, "America/New_York")).toBe(true);
  });

  // ADR-0083: the Time block wins outright — a Task blocked Tuesday but due
  // Friday renders on Tuesday's Month cell, not Friday's.
  it("prefers the Time block's day over the Deadline's when both are present", () => {
    const start = fromZonedTime(new Date(2026, 8, 15, 14, 0), "America/New_York");
    const due = fromZonedTime(new Date(2026, 8, 18, 0, 0), "America/New_York");
    const tuesday = fromZonedTime(new Date(2026, 8, 15, 0, 0), "America/New_York");
    const friday = fromZonedTime(new Date(2026, 8, 18, 0, 0), "America/New_York");

    expect(taskFallsOnMonthDay(placeable({ start, due }), tuesday, "America/New_York")).toBe(true);
    expect(taskFallsOnMonthDay(placeable({ start, due }), friday, "America/New_York")).toBe(false);
  });

  it("is false when the effective date falls on a different calendar day than day", () => {
    const start = fromZonedTime(new Date(2026, 8, 15, 14, 0), "America/New_York");
    const day = fromZonedTime(new Date(2026, 8, 16, 0, 0), "America/New_York");
    expect(taskFallsOnMonthDay(placeable({ start }), day, "America/New_York")).toBe(false);
  });
});

describe("bucketTasks", () => {
  it("splits Tasks into their four buckets, each pre-sorted", () => {
    const now = fromZonedTime(new Date(2026, 8, 15, 9, 0), "America/New_York");
    const overdueTask = makeTask({ due: fromZonedTime(new Date(2026, 8, 10), "America/New_York") });
    const todaySoon = makeTask({
      due: fromZonedTime(new Date(2026, 8, 15), "America/New_York"),
      priority: 9,
      createdAt: new Date(2026, 0, 1),
    });
    const todayUrgent = makeTask({
      due: fromZonedTime(new Date(2026, 8, 15), "America/New_York"),
      priority: 1,
      createdAt: new Date(2026, 0, 2),
    });
    const upcomingTask = makeTask({ due: fromZonedTime(new Date(2026, 8, 20), "America/New_York") });
    const noDateTask = makeTask();

    const buckets = bucketTasks(
      [overdueTask, todaySoon, upcomingTask, noDateTask, todayUrgent],
      now,
      "America/New_York",
    );

    expect(buckets.overdue).toEqual([overdueTask]);
    expect(buckets.today).toEqual([todayUrgent, todaySoon]);
    expect(buckets.upcoming).toEqual([upcomingTask]);
    expect(buckets.noDate).toEqual([noDateTask]);
  });
});
