import { format } from "date-fns";
import type { CalendarBlockStyle } from "../lib/calendarColors";

interface MonthEventDragPreviewProps {
  x: number;
  y: number;
  title: string;
  start: Date;
  blockStyle: CalendarBlockStyle;
}

export function MonthEventDragPreview({
  x,
  y,
  title,
  start,
  blockStyle,
}: MonthEventDragPreviewProps) {
  return (
    <div
      className="pointer-events-none fixed z-50 max-w-48 -translate-x-2 -translate-y-1/2 truncate rounded-shell-sm px-1.5 py-1 text-label-sm opacity-90 ring-2 ring-accent"
      style={{ left: x, top: y, ...blockStyle }}
    >
      {format(start, "h:mm a")} {title}
    </div>
  );
}
