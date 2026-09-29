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

/**
 * Resolves a drag's drop point to a Month Day cell's date (#315), against
 * `data-cell-date` — the same attribute `MonthGrid`'s own `getCellDateAtPoint`
 * reads for an Event drag, here read from `TaskRow`'s panel-drop gesture,
 * which (unlike a drag already on the grid) has no `cells` array of its own
 * to match against. `date.toDateString()` round-trips through `new Date()`
 * unambiguously, so no such array is needed. `null` when the drop landed
 * anywhere else, including the hourly grid or the all-day lane, neither of
 * which carries this attribute.
 */
export function resolveMonthDropDate(clientX: number, clientY: number): Date | null {
  const target = document.elementFromPoint(clientX, clientY);
  const cellElement = target?.closest<HTMLElement>("[data-cell-date]");
  const dateKey = cellElement?.dataset.cellDate;
  if (!dateKey) return null;

  return new Date(dateKey);
}
