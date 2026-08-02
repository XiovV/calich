export const PIXELS_PER_HOUR = 60;
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

export function snapToIncrement(date: Date, incrementMinutes: number): Date {
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
