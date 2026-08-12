import { format } from "date-fns";

/** "15m", "1h", "1h 30m", "2h 15m", "8h" — zero parts dropped. Every
 * duration is a multiple of 15 minutes, the grid's snap increment. */
export function formatDragDuration(minutes: number): string {
  const hours = Math.floor(minutes / 60);
  const remainderMinutes = minutes % 60;
  const parts: string[] = [];
  if (hours > 0) parts.push(`${hours}h`);
  if (remainderMinutes > 0) parts.push(`${remainderMinutes}m`);
  return parts.join(" ");
}

/** The Drag readout's text, e.g. "09:15 – 09:45 · 30m" — the start, end and
 * total duration a move or resize gesture would commit if released now. */
export function formatDragReadout(
  start: Date,
  end: Date,
  timePattern: string,
): string {
  const minutes = Math.round((end.getTime() - start.getTime()) / 60_000);
  return `${format(start, timePattern)} – ${format(end, timePattern)} · ${formatDragDuration(minutes)}`;
}
