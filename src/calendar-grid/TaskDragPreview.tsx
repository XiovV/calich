import type { CalendarBlockStyle } from "../lib/calendarColors";

interface TaskDragPreviewProps {
  x: number;
  y: number;
  title: string;
  blockStyle: CalendarBlockStyle;
}

/**
 * The floating label that follows the cursor while a Task panel row is
 * being dragged toward the hourly grid (#313, ADR-0083) — the panel-drop
 * gesture's own visual feedback, styled identically to
 * `AllDayEventDragPreview` so a Task drag reads as the same kind of gesture
 * an Event's own all-day drag already is.
 */
export function TaskDragPreview({ x, y, title, blockStyle }: TaskDragPreviewProps) {
  return (
    <div
      className="pointer-events-none fixed z-50 max-w-48 -translate-x-2 -translate-y-1/2 truncate rounded-shell-sm px-1.5 py-1 text-label-sm opacity-90 ring-2 ring-accent-ink"
      style={{ left: x, top: y, ...blockStyle }}
    >
      {title}
    </div>
  );
}
