import { differenceInCalendarDays } from "date-fns";
import { toZonedTime } from "date-fns-tz";
import { priorityLevelFromValue, priorityRank, type PriorityLevel } from "./taskPriority";

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
  return instantFallsOnDay(due, day, viewerZone);
}

/**
 * Whether `instant` falls on the same calendar date as `day`, in
 * `viewerZone` — the general form `taskDeadlineFallsOnDay` specializes to a
 * Deadline specifically. Used directly by `taskFallsOnMonthDay` (#315),
 * which needs the same day comparison against whichever of a Task's two
 * axes placement precedence picked, not a Deadline by name.
 */
function instantFallsOnDay(instant: Date, day: Date, viewerZone: string): boolean {
  return (
    differenceInCalendarDays(
      zonedCalendarDay(instant, viewerZone),
      zonedCalendarDay(day, viewerZone),
    ) === 0
  );
}

/**
 * Whether `task` should render on `day` in Month view (#315, ADR-0083):
 * placement precedence's Time-block-first, Deadline-second rule, collapsed
 * to a single day test since Month draws both surfaces as one chip rather
 * than splitting them across the hourly grid and the all-day lane the way
 * Day/Week do. `false` for a panel-only Task (neither axis set).
 */
export function taskFallsOnMonthDay(task: PlaceableTask, day: Date, viewerZone: string): boolean {
  const effective = task.start ?? task.due;
  if (!effective) return false;
  return instantFallsOnDay(effective, day, viewerZone);
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
 * Within-heading ordering (#311, #316, issue #309 story 24): Deadline
 * ascending with undated Tasks last, then Priority descending (by level, not
 * the raw value's own inverted scale — see `priorityRank`), then creation
 * time ascending, so the top of a heading is reliably the thing to do next —
 * on every Group by axis a Task bucket, a Task List, or a Priority level
 * alike, since none of them carries an ordering of its own.
 */
export function compareTasksWithinHeading(a: SchedulableTask, b: SchedulableTask): number {
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
 * `compareTasksWithinHeading` — what the Tasks panel renders directly.
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
    buckets[bucket].sort(compareTasksWithinHeading);
  }

  return buckets;
}

/**
 * Priority levels High to None — the order the panel's Group by: Priority
 * axis renders its headings in (#316, ADR-0083). The reverse of
 * taskPriority.ts's own `PRIORITY_LEVELS` (ascending, for the detail
 * surface's picker), which orders least to most rather than most pressing
 * first.
 */
export const PRIORITY_HEADING_ORDER: readonly PriorityLevel[] = ["high", "medium", "low", "none"];

/**
 * `tasks` split by Priority level, each already sorted by
 * `compareTasksWithinHeading` — the Group by: Priority axis's data source
 * (#316, ADR-0083). Within-heading ordering is unchanged from a Task
 * bucket's: Priority already breaks a Deadline tie there, so grouping by it
 * outright doesn't disturb Deadline-then-creation-time ordering within one
 * level.
 */
export function tasksByPriorityLevel<T extends SchedulableTask>(
  tasks: T[],
): Record<PriorityLevel, T[]> {
  const headings: Record<PriorityLevel, T[]> = { none: [], low: [], medium: [], high: [] };

  for (const task of tasks) {
    headings[priorityLevelFromValue(task.priority)].push(task);
  }

  for (const level of PRIORITY_HEADING_ORDER) {
    headings[level].sort(compareTasksWithinHeading);
  }

  return headings;
}

/**
 * `tasks` split by their own Task List id, each already sorted by
 * `compareTasksWithinHeading` — the Group by: Task List axis's data source
 * (#316, ADR-0083). Keyed rather than pre-ordered: heading order and colour
 * come from the Task Lists the panel already holds (the checked ones, in
 * list order), so this only has to answer which Tasks belong under each id.
 */
export function tasksByTaskList<T extends SchedulableTask & { taskListId: number }>(
  tasks: T[],
): Map<number, T[]> {
  const headings = new Map<number, T[]>();

  for (const task of tasks) {
    const forList = headings.get(task.taskListId);
    if (forList) {
      forList.push(task);
    } else {
      headings.set(task.taskListId, [task]);
    }
  }

  for (const forList of headings.values()) {
    forList.sort(compareTasksWithinHeading);
  }

  return headings;
}
