import { PIXELS_PER_HOUR, snapToIncrement, yToTime } from "../lib/gridTime";

/**
 * Resolves a drag's drop point to a grid day and time, if it landed on one —
 * the same data-attribute-plus-`elementFromPoint` technique
 * `AllDayLane`/`MonthGrid` use for their own in-grid drags
 * (`getAllDayDateAtPoint`, `getCellDateAtPoint`). Shared by every drag that
 * can end on the hourly grid regardless of where it started: a Tasks panel
 * row (#313), an existing grid block being rescheduled, and an all-day
 * lane's Deadline-only chip (#314) all read the same `[data-grid-day-ms]`
 * DayColumn sets. `null` when the drop landed anywhere else.
 */
export function resolveGridDropTime(clientX: number, clientY: number): Date | null {
  const target = document.elementFromPoint(clientX, clientY);
  const columnElement = target?.closest<HTMLElement>("[data-grid-day-ms]");
  const dayMsKey = columnElement?.dataset.gridDayMs;
  if (!columnElement || !dayMsKey) return null;

  const day = new Date(Number(dayMsKey));
  const offsetY = clientY - columnElement.getBoundingClientRect().top;
  return snapToIncrement(yToTime(offsetY, day, PIXELS_PER_HOUR), 15);
}

/**
 * Whether a drag's drop point landed over the Tasks panel — the drop
 * table's "back to panel" row (#314), read off the same
 * `elementFromPoint`-plus-data-attribute technique as `resolveGridDropTime`,
 * against `data-tasks-panel` on the panel's own root (`TasksPanel.tsx`).
 */
export function isPointOverTasksPanel(clientX: number, clientY: number): boolean {
  const target = document.elementFromPoint(clientX, clientY);
  return target?.closest("[data-tasks-panel]") != null;
}
