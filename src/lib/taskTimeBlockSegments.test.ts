import { describe, expect, it } from "vitest";
import { getTaskTimeBlockDaySegments, taskTimeBlockEnd } from "./taskTimeBlockSegments";
import type { Task } from "./tasksApi";

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: 1,
    taskListId: 1,
    title: "Write the spec",
    notes: "",
    due: null,
    start: null,
    durationMinutes: null,
    priority: 0,
    completed: false,
    createdAt: new Date(2026, 0, 1),
    ...overrides,
  };
}

describe("taskTimeBlockEnd", () => {
  it("is start plus durationMinutes", () => {
    const task = makeTask({ start: new Date(2026, 8, 10, 9, 0), durationMinutes: 90 });
    expect(taskTimeBlockEnd(task)).toEqual(new Date(2026, 8, 10, 10, 30));
  });
});

describe("getTaskTimeBlockDaySegments", () => {
  it("returns one segment for a Task blocked wholly inside the day", () => {
    const task = makeTask({ start: new Date(2026, 8, 10, 9, 0), durationMinutes: 60 });
    const segments = getTaskTimeBlockDaySegments([task], new Date(2026, 8, 10));
    expect(segments).toEqual([
      { task, start: new Date(2026, 8, 10, 9, 0), end: new Date(2026, 8, 10, 10, 0) },
    ]);
  });

  it("excludes a Task blocked on a different day", () => {
    const task = makeTask({ start: new Date(2026, 8, 11, 9, 0), durationMinutes: 60 });
    expect(getTaskTimeBlockDaySegments([task], new Date(2026, 8, 10))).toEqual([]);
  });

  it("excludes a Task with no Time block", () => {
    const task = makeTask({ due: new Date(2026, 8, 10) });
    expect(getTaskTimeBlockDaySegments([task], new Date(2026, 8, 10))).toEqual([]);
  });

  it("clips a block crossing midnight to each day it touches", () => {
    const task = makeTask({ start: new Date(2026, 8, 10, 23, 0), durationMinutes: 120 });

    const firstDay = getTaskTimeBlockDaySegments([task], new Date(2026, 8, 10));
    expect(firstDay).toHaveLength(1);
    expect(firstDay[0].start).toEqual(new Date(2026, 8, 10, 23, 0));
    expect(firstDay[0].end).toEqual(new Date(2026, 8, 11, 0, 0));

    const secondDay = getTaskTimeBlockDaySegments([task], new Date(2026, 8, 11));
    expect(secondDay).toHaveLength(1);
    expect(secondDay[0].start).toEqual(new Date(2026, 8, 11, 0, 0));
    expect(secondDay[0].end).toEqual(new Date(2026, 8, 11, 1, 0));
  });
});
