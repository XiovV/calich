import { addDays, isSameDay, isSameMonth, startOfMonth, startOfWeek } from "date-fns";
import type { Event } from "./event";

export const MONTH_GRID_ROWS = 6;
export const MONTH_GRID_COLUMNS = 7;
export const MONTH_GRID_CELL_COUNT = MONTH_GRID_ROWS * MONTH_GRID_COLUMNS;

export interface MonthGridCell {
  date: Date;
  inCurrentMonth: boolean;
}

export function buildMonthGrid(selectedDate: Date): MonthGridCell[] {
  const monthStart = startOfMonth(selectedDate);
  const gridStart = startOfWeek(monthStart);

  return Array.from({ length: MONTH_GRID_CELL_COUNT }, (_, index) => {
    const date = addDays(gridStart, index);
    return { date, inCurrentMonth: isSameMonth(date, monthStart) };
  });
}

export function getEventsForDay(events: Event[], day: Date): Event[] {
  return events
    .filter((event) => isSameDay(event.start, day))
    .sort((a, b) => a.start.getTime() - b.start.getTime());
}

export interface ChipCapacity {
  visibleCount: number;
  overflowCount: number;
}

export function computeChipCapacity(
  totalEvents: number,
  availableHeight: number,
  chipHeight: number,
  moreRowHeight: number,
): ChipCapacity {
  const maxWithoutOverflowRow = Math.floor(availableHeight / chipHeight);
  if (totalEvents <= maxWithoutOverflowRow) {
    return { visibleCount: totalEvents, overflowCount: 0 };
  }

  const remainingHeight = availableHeight - moreRowHeight;
  const visibleCount = Math.max(0, Math.floor(remainingHeight / chipHeight));
  return { visibleCount, overflowCount: totalEvents - visibleCount };
}
