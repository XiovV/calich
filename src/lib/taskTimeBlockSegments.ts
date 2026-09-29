import { addDays, addMinutes, startOfDay } from "date-fns";
import type { Task } from "./tasksApi";

/**
 * `getDaySegments`'s (`occurrenceSegments.ts`) counterpart for a Task's Time
 * block (#313, ADR-0083): the block's own `[start, end)` — `start` plus
 * `durationMinutes` — clipped to one day's `[00:00, 24:00)` bounds, so the
 * hourly grid never has to render a block past its own midnight.
 */
export interface TaskTimeBlockSegment {
  task: Task;
  start: Date;
  end: Date;
}

/** A Time-blocked Task's own end instant: `start` plus `durationMinutes`
 * (ADR-0083's VTODO `DTSTART`+`DURATION`, never `DUE`). Only meaningful for
 * a Task that actually has a Time block — callers filter for that first. */
export function taskTimeBlockEnd(task: Task): Date {
  return addMinutes(task.start as Date, task.durationMinutes ?? 0);
}

function taskTimeBlockIntersectsDay(task: Task, day: Date): boolean {
  if (!task.start || !task.durationMinutes) return false;
  const dayStart = startOfDay(day);
  const dayEnd = addDays(dayStart, 1);
  return task.start < dayEnd && taskTimeBlockEnd(task) > dayStart;
}

function taskTimeBlockSegmentForDay(task: Task, day: Date): TaskTimeBlockSegment {
  const dayStart = startOfDay(day);
  const dayEnd = addDays(dayStart, 1);
  const start = task.start as Date;
  const end = taskTimeBlockEnd(task);
  return {
    task,
    start: start < dayStart ? dayStart : start,
    end: end > dayEnd ? dayEnd : end,
  };
}

/**
 * Every `tasks` Time block that touches `day`, clipped to its bounds — one
 * per Task placed on the hourly grid (`taskPlacement(task) === "grid"`) that
 * intersects it. A Task with no Time block never produces a segment: the
 * caller is expected to have already applied placement precedence and "Show
 * tasks on calendar"/"Show completed", the same division TimeGrid already
 * makes for `deadlineTasks`.
 */
export function getTaskTimeBlockDaySegments(tasks: Task[], day: Date): TaskTimeBlockSegment[] {
  return tasks
    .filter((task) => taskTimeBlockIntersectsDay(task, day))
    .map((task) => taskTimeBlockSegmentForDay(task, day));
}
