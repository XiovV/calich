import type { CalendarBlockStyle } from "../lib/calendarColors";

interface AllDayEventDragPreviewProps {
  x: number;
  y: number;
  title: string;
  blockStyle: CalendarBlockStyle;
}

export function AllDayEventDragPreview({
  x,
  y,
  title,
  blockStyle,
}: AllDayEventDragPreviewProps) {
  return (
    <div
      className="pointer-events-none fixed z-50 max-w-48 -translate-x-2 -translate-y-1/2 truncate rounded-shell-sm px-1.5 py-1 text-label-sm opacity-90 ring-2 ring-accent"
      style={{ left: x, top: y, ...blockStyle }}
    >
      {title}
    </div>
  );
}
