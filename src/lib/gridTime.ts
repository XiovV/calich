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
