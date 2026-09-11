/**
 * Drop semantics for time-blocking a Task by drag (#313, #314, #315,
 * ADR-0083): what a drag from a given source onto a given drop target
 * writes. Kept out of the drag handler entirely — `gridStacking`,
 * `useOccurrenceDragCommit` and `reminderDrafts` are the three pieces of
 * grid logic that stayed inside components, and they are exactly the three
 * with no tests. This module exists so drop semantics never join them.
 *
 * Three of the four targets let the target decide the write outright — the
 * source only decides the duration a fresh block gets (a moved block keeps
 * its own; a freshly-created one gets the 1h default), which is why
 * `TaskDragSource` carries nothing else. This is what lets one `hourlyGrid`
 * branch serve every one of the table's "-> hourly grid" rows (#313's
 * panel-row row, #314's reschedule row, and #314's all-day-chip row) without
 * a switch over the source's kind. `monthDay` is the exception (#315): Month
 * has no time axis to invent an hour from, so the same drop writes a moved
 * Time block when one already exists and a fresh Deadline when it doesn't —
 * the one target where the source's kind decides the *action*, not just a
 * duration.
 */

/** A Task's own 1h default Time block duration (issue #309 story 48). */
export const DEFAULT_TIME_BLOCK_DURATION_MINUTES = 60;

/**
 * What's being dragged. A panel row and an all-day chip both carry no
 * existing block, so a drop of either onto the hourly grid always produces a
 * fresh 1h one; a Time block carries its own current duration, which a
 * reschedule preserves rather than resetting.
 */
export type TaskDragSource =
  | { kind: "panelRow" }
  | { kind: "allDayChip" }
  | { kind: "timeBlock"; durationMinutes: number };

/**
 * The four surfaces a Task can be dropped on. `monthDay`'s `date` is a Month
 * Day cell (#315) — for a `timeBlock` source it must already carry the
 * resolved time-of-day (the caller combines the target day with the block's
 * own start, the same `computeMoveToDate` a Month Event drag already uses),
 * since a bare day carries no hour to move to.
 */
export type TaskDropTarget =
  | { surface: "hourlyGrid"; time: Date }
  | { surface: "allDayLane"; date: Date }
  | { surface: "panel" }
  | { surface: "monthDay"; date: Date };

/**
 * The fields a drop intent writes, as the store action it maps to rather
 * than a bare field bag — `setTimeBlock`/`clearTimeBlock` never carry `due`,
 * and `setDeadlineAndClearTimeBlock` is the one write that touches both axes
 * at once, structurally cordoned off from the other two. A Deadline is
 * therefore untouched by every row except the all-day-lane one by
 * construction, not by an explicit "leave it alone" branch.
 */
export type TaskDropWrite =
  | { action: "setTimeBlock"; start: Date; durationMinutes: number }
  | { action: "setDeadlineAndClearTimeBlock"; due: Date }
  | { action: "clearTimeBlock" };

/**
 * What dragging `source` onto `target` writes (issue #309 stories 47-58,
 * #314's drop table, #315's Month row). The full table:
 *
 * | Drag              | Drop target    | Writes                              |
 * |-------------------|----------------|-------------------------------------|
 * | Panel row         | hourly grid    | sets Time block, 1h default (#313)  |
 * | Time block        | elsewhere grid | moves Time block, own duration kept |
 * | All-day chip      | hourly grid    | sets Time block, 1h default         |
 * | Time block        | all-day lane   | sets Deadline, clears Time block    |
 * | Time block        | panel          | clears Time block                   |
 * | Time block        | Month Day cell | moves Time block, time-of-day kept  |
 * | Panel row/all-day | Month Day cell | sets Deadline, no Time block (#315) |
 *
 * The all-day-lane row is the one gesture that destroys data rather than
 * adding it, and it is deliberate: precedence (`taskScheduling.ts`) would
 * otherwise keep drawing the Task on the hourly grid off its stale Time
 * block, and the drag would visibly snap back after having written
 * something. The Month rows are deliberate in the opposite direction —
 * Month can *move* a Time block but never *create* one, because inventing an
 * hour from a gesture that named no hour is worse than the asymmetry with
 * Week view (ADR-0083).
 */
export function taskDropIntent(source: TaskDragSource, target: TaskDropTarget): TaskDropWrite {
  switch (target.surface) {
    case "hourlyGrid": {
      const durationMinutes =
        source.kind === "timeBlock" ? source.durationMinutes : DEFAULT_TIME_BLOCK_DURATION_MINUTES;
      return { action: "setTimeBlock", start: target.time, durationMinutes };
    }
    case "allDayLane":
      return { action: "setDeadlineAndClearTimeBlock", due: target.date };
    case "panel":
      return { action: "clearTimeBlock" };
    case "monthDay":
      return source.kind === "timeBlock"
        ? { action: "setTimeBlock", start: target.date, durationMinutes: source.durationMinutes }
        : { action: "setDeadlineAndClearTimeBlock", due: target.date };
  }
}
