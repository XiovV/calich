import { addDays, isSameDay, isSameMonth, startOfMonth, startOfWeek } from "date-fns";
import type { Occurrence } from "./occurrence";
import { roundUpToIncrement, type DraftBlock } from "./gridTime";

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

export function getOccurrencesForDay(
  occurrences: Occurrence[],
  day: Date,
): Occurrence[] {
  return occurrences
    .filter((occurrence) => isSameDay(occurrence.start, day))
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

export function computeCellDraft(
  cellDate: Date,
  now: Date,
  incrementMinutes = 15,
  durationMinutes = 30,
): DraftBlock {
  const roundedNow = roundUpToIncrement(now, incrementMinutes);
  const crossedMidnight = !isSameDay(roundedNow, now);

  const start = new Date(cellDate);
  if (crossedMidnight) {
    start.setHours(0, 0, 0, 0);
  } else {
    start.setHours(roundedNow.getHours(), roundedNow.getMinutes(), 0, 0);
  }

  const end = new Date(start.getTime() + durationMinutes * 60 * 1000);
  return { start, end };
}
