import { format } from "date-fns";

interface MonthEventDragPreviewProps {
  x: number;
  y: number;
  title: string;
  start: Date;
  colorClass: string;
}

export function MonthEventDragPreview({
  x,
  y,
  title,
  start,
  colorClass,
}: MonthEventDragPreviewProps) {
  return (
    <div
      className={`pointer-events-none fixed z-50 max-w-48 -translate-x-2 -translate-y-1/2 truncate rounded-shell-sm px-1.5 py-1 text-label-sm text-ink-inverse opacity-90 ring-2 ring-accent ${colorClass}`}
      style={{ left: x, top: y }}
    >
      {format(start, "h:mm a")} {title}
    </div>
  );
}
