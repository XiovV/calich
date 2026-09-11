/**
 * Drop semantics for time-blocking a Task by drag (#313, ADR-0083): what a
 * drag onto a given drop target writes. Kept out of the drag handler
 * entirely — `gridStacking`, `useOccurrenceDragCommit` and `reminderDrafts`
 * are the three pieces of grid logic that stayed inside components, and they
 * are exactly the three with no tests. This module exists so drop semantics
 * never join them.
 *
 * Only the drop table's panel-row -> hourly-grid row is implemented here.
 * The remaining rows (a Time block dragged elsewhere on the grid or into the
 * all-day lane, a drop back into the panel, a Month day cell) are later
 * tickets' own drag sources and drop targets, added to this module rather
 * than to a handler when they land.
 */

/** A Task's own 1h default Time block duration (issue #309 story 48). */
export const DEFAULT_TIME_BLOCK_DURATION_MINUTES = 60;

/**
 * The one drop target this ticket exercises: the hourly grid, at the
 * wall-clock instant the drop resolved to (already snapped to the grid's
 * own increment by the caller).
 */
export interface TaskDropTarget {
  surface: "hourlyGrid";
  time: Date;
}

/** The fields a drop intent writes. `due` is never part of this row's
 * result — the caller sends only `start`/`durationMinutes`, so the
 * Deadline is untouched by construction rather than by an explicit
 * "leave it alone" branch. */
export interface TaskDropWrite {
  start: Date;
  durationMinutes: number;
}

/**
 * What dragging a Task's panel row onto `target` writes: a Time block at the
 * drop time with a 1h default duration (issue #309 stories 47-48). A drop
 * needs no further decision to be useful — nothing here reads the Task's
 * current fields, because a panel row (as opposed to an existing Time block
 * being dragged) always produces a fresh block rather than adjusting one.
 */
export function taskDropIntent(target: TaskDropTarget): TaskDropWrite {
  return {
    start: target.time,
    durationMinutes: DEFAULT_TIME_BLOCK_DURATION_MINUTES,
  };
}
