import { addDays, differenceInCalendarDays } from "date-fns";

export const PIXELS_PER_HOUR = 48;
export const HOURS_IN_DAY = 24;

export function timeToY(date: Date, pixelsPerHour: number): number {
  const hours = date.getHours() + date.getMinutes() / 60;
  return hours * pixelsPerHour;
}

export function durationToHeight(
  start: Date,
  end: Date,
  pixelsPerHour: number,
): number {
  const hours = (end.getTime() - start.getTime()) / (1000 * 60 * 60);
  return hours * pixelsPerHour;
}

export function yToTime(
  y: number,
  referenceDay: Date,
  pixelsPerHour: number,
): Date {
  const totalMinutes = (y / pixelsPerHour) * 60;
  const result = new Date(referenceDay);
  result.setHours(0, totalMinutes, 0, 0);
  return result;
}

function roundToIncrement(
  date: Date,
  incrementMinutes: number,
  round: (value: number) => number,
): Date {
  const incrementMs = incrementMinutes * 60 * 1000;
  const roundedMs = round(date.getTime() / incrementMs) * incrementMs;
  return new Date(roundedMs);
}

function snapToIncrement(date: Date, incrementMinutes: number): Date {
  return roundToIncrement(date, incrementMinutes, Math.round);
}

export interface DraftBlock {
  start: Date;
  end: Date;
}

export function computeDraftBlock(
  day: Date,
  startY: number,
  endY: number,
  pixelsPerHour: number,
  snapMinutes = 15,
  minDurationMinutes = 30,
): DraftBlock {
  const rawStart = yToTime(Math.min(startY, endY), day, pixelsPerHour);
  const rawEnd = yToTime(Math.max(startY, endY), day, pixelsPerHour);
  const start = snapToIncrement(rawStart, snapMinutes);
  const end = snapToIncrement(rawEnd, snapMinutes);

  if (end.getTime() <= start.getTime()) {
    return { start, end: new Date(start.getTime() + minDurationMinutes * 60 * 1000) };
  }

  return { start, end };
}

export function roundUpToIncrement(date: Date, incrementMinutes: number): Date {
  return roundToIncrement(date, incrementMinutes, Math.ceil);
}

export function computeDefaultDraft(
  now: Date,
  incrementMinutes = 15,
  durationMinutes = 30,
): DraftBlock {
  const start = roundUpToIncrement(now, incrementMinutes);
  const end = new Date(start.getTime() + durationMinutes * 60 * 1000);
  return { start, end };
}

export function computeMovedEventTimes(
  originalStart: Date,
  originalEnd: Date,
  dayOffset: number,
  minuteOffset: number,
  snapMinutes = 15,
): DraftBlock {
  const snappedMinuteOffset =
    Math.round(minuteOffset / snapMinutes) * snapMinutes;
  const totalOffsetMs =
    dayOffset * 24 * 60 * 60 * 1000 + snappedMinuteOffset * 60 * 1000;
  const duration = originalEnd.getTime() - originalStart.getTime();
  const start = new Date(originalStart.getTime() + totalOffsetMs);
  const end = new Date(start.getTime() + duration);
  return { start, end };
}

/**
 * `[start, end)` moved to `targetDate`, keeping the original time-of-day and
 * duration. Used by any date-cell drag (Month Day cells, the Week all-day
 * lane) where the drop target is a whole day rather than a pixel offset.
 */
export function computeMoveToDate(
  start: Date,
  end: Date,
  targetDate: Date,
): DraftBlock {
  const duration = end.getTime() - start.getTime();
  const newStart = new Date(targetDate);
  newStart.setHours(
    start.getHours(),
    start.getMinutes(),
    start.getSeconds(),
    start.getMilliseconds(),
  );
  const newEnd = new Date(newStart.getTime() + duration);
  return { start: newStart, end: newEnd };
}

/**
 * The date `computeMoveToDate` should treat as the drop target, adjusted for
 * a drag that started on a day other than the Occurrence's own start. A
 * multi-day Occurrence renders on every day it spans (#232), so a drag can
 * begin from any of them — shifting `targetDate` back by the same gap
 * between `dragStartDay` and `occurrenceStart` keeps `computeMoveToDate`
 * (which always anchors to the true start) moving the whole Occurrence by
 * the distance the user actually dragged, rather than snapping its start to
 * the drop point regardless of which day was picked up.
 */
export function resolveDragTargetDate(
  occurrenceStart: Date,
  dragStartDay: Date,
  targetDate: Date,
): Date {
  const dayOffset = differenceInCalendarDays(dragStartDay, occurrenceStart);
  return addDays(targetDate, -dayOffset);
}

export function computeResizedEventTimes(
  originalStart: Date,
  originalEnd: Date,
  edge: "start" | "end",
  minuteOffset: number,
  snapMinutes = 15,
  minDurationMinutes = 15,
): DraftBlock {
  const snappedOffsetMs =
    Math.round(minuteOffset / snapMinutes) * snapMinutes * 60 * 1000;
  const minDurationMs = minDurationMinutes * 60 * 1000;

  if (edge === "start") {
    const start = new Date(originalStart.getTime() + snappedOffsetMs);
    const latestStart = new Date(originalEnd.getTime() - minDurationMs);
    return {
      start: start.getTime() > latestStart.getTime() ? latestStart : start,
      end: originalEnd,
    };
  }

  const end = new Date(originalEnd.getTime() + snappedOffsetMs);
  const earliestEnd = new Date(originalStart.getTime() + minDurationMs);
  return {
    start: originalStart,
    end: end.getTime() < earliestEnd.getTime() ? earliestEnd : end,
  };
}
