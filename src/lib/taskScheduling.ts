import { differenceInCalendarDays } from "date-fns";
import { toZonedTime } from "date-fns-tz";
import { priorityRank } from "./taskPriority";

/**
 * The four headings the Tasks panel sorts Tasks under (#311, ADR-0083,
 * CONTEXT.md's Task bucket entry), derived at render and never stored —
 * nothing moves a Task between buckets at midnight because there is nothing
 * stored to move.
 */
export type TaskBucket = "overdue" | "today" | "upcoming" | "noDate";

export const TASK_BUCKET_ORDER: readonly TaskBucket[] = [
  "overdue",
  "today",
  "upcoming",
  "noDate",
];

export const TASK_BUCKET_LABELS: Record<TaskBucket, string> = {
  overdue: "Overdue",
  today: "Today",
  upcoming: "Upcoming",
  noDate: "No date",
};

/**
 * The fields `taskBucket`/`compareTasksWithinBucket` read off a Task —
 * structural rather than the full `Task` shape, so a caller can pass either
 * the real thing or a bare test fixture.
 */
export interface SchedulableTask {
  /** The Deadline (`DUE`) — decides the bucket outright when present. */
  due: Date | null;
  /** The Time block's start (`DTSTART`) — the bucket's fallback when there
   * is no Deadline. Nothing sets this yet (#312/#313 land the Time block
   * itself); this is the groundwork ADR-0083 calls for. */
  start: Date | null;
  priority: number;
  createdAt: Date;
}

/**
 * `instant` converted to a Date whose plain (system-local) getters read the
 * wall-clock calendar day it falls on in `zone` — the same trick
 * `floatingTime.ts`'s `toFloating` relies on, here used just for the day,
 * not a full floating-frame RRULE run.
 */
function zonedCalendarDay(instant: Date, zone: string): Date {
  return toZonedTime(instant, zone);
}

/**
 * The three surfaces placement precedence (#312, ADR-0083) can resolve a
 * Task to. There is no discriminator field on a Task itself — presence of
 * `start`/`due` IS the state `taskPlacement` reads.
 */
export type TaskPlacement = "grid" | "allDay" | "panel";

/** The fields `taskPlacement` reads — structural, like `SchedulableTask`. */
export interface PlaceableTask {
  /** The Time block's start (`DTSTART`) — wins outright when present. */
  start: Date | null;
  /** The Deadline (`DUE`) — the fallback surface when there is no Time block. */
  due: Date | null;
}

/**
 * Placement precedence (#312, ADR-0083): a Task renders in exactly one
 * place. A Time block wins outright and puts it on the hourly grid; failing
 * that, a Deadline puts it in the all-day lane; failing that, it is
 * panel-only. Every later grid ticket (Time-blocking, drag) depends on this
 * holding for all four field combinations.
 */
export function taskPlacement(task: PlaceableTask): TaskPlacement {
  if (task.start) return "grid";
  if (task.due) return "allDay";
  return "panel";
}

/**
 * Whether `due` — a Task's Deadline — falls on the same calendar date as
 * `day`, both read through `viewerZone`. The all-day lane's own per-day
 * filter for a Deadline-only Task (#312), using the same zone handling
 * `taskBucket` does rather than comparing raw UTC fields, so a Deadline set
 * near midnight lands on the date it reads as in the Viewer zone, not
 * whatever date its UTC instant happens to carry.
 */
export function taskDeadlineFallsOnDay(due: Date, day: Date, viewerZone: string): boolean {
  return (
    differenceInCalendarDays(
      zonedCalendarDay(due, viewerZone),
      zonedCalendarDay(day, viewerZone),
    ) === 0
  );
}

/**
 * The Task bucket `task` falls into, derived from its Deadline against
 * today in `viewerZone` — falling back to the Time block's start when there
 * is no Deadline, and `noDate` when neither is present (ADR-0083). A
 * Deadline always wins over a Time block: a Friday-due Task blocked on
 * Tuesday is `upcoming`, never `today`, because the fallback is only
 * consulted when `due` is absent outright.
 */
export function taskBucket(task: SchedulableTask, now: Date, viewerZone: string): TaskBucket {
  const effective = task.due ?? task.start;
  if (!effective) return "noDate";

  const dayDiff = differenceInCalendarDays(
    zonedCalendarDay(effective, viewerZone),
    zonedCalendarDay(now, viewerZone),
  );
  if (dayDiff < 0) return "overdue";
  if (dayDiff === 0) return "today";
  return "upcoming";
}

/**
 * Within-bucket ordering (#311, issue #309 story 24): Deadline ascending
 * with undated Tasks last, then Priority descending (by level, not the raw
 * value's own inverted scale — see `priorityRank`), then creation time
 * ascending, so the top of a bucket is reliably the thing to do next.
 */
export function compareTasksWithinBucket(a: SchedulableTask, b: SchedulableTask): number {
  const dueDiff = compareDeadlineAscendingUndatedLast(a.due, b.due);
  if (dueDiff !== 0) return dueDiff;

  const priorityDiff = priorityRank(b.priority) - priorityRank(a.priority);
  if (priorityDiff !== 0) return priorityDiff;

  return a.createdAt.getTime() - b.createdAt.getTime();
}

function compareDeadlineAscendingUndatedLast(a: Date | null, b: Date | null): number {
  if (a === null && b === null) return 0;
  if (a === null) return 1;
  if (b === null) return -1;
  return a.getTime() - b.getTime();
}

/**
 * `tasks` split into the four Task buckets, each already sorted by
 * `compareTasksWithinBucket` — what the Tasks panel renders directly.
 * Generic over `T` so a caller can hand it real `Task`s (which carry an id,
 * title, etc. beyond `SchedulableTask`'s fields) and get them back typed.
 */
export function bucketTasks<T extends SchedulableTask>(
  tasks: T[],
  now: Date,
  viewerZone: string,
): Record<TaskBucket, T[]> {
  const buckets: Record<TaskBucket, T[]> = {
    overdue: [],
    today: [],
    upcoming: [],
    noDate: [],
  };

  for (const task of tasks) {
    buckets[taskBucket(task, now, viewerZone)].push(task);
  }

  for (const bucket of TASK_BUCKET_ORDER) {
    buckets[bucket].sort(compareTasksWithinBucket);
  }

  return buckets;
}
