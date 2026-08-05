import type { CalendarBlockStyle } from "../lib/calendarColors";
import { EventVisual } from "./EventVisual";

interface EventDragPreviewProps {
  top: number;
  height: number;
  left: number;
  width: number;
  title: string;
  start: Date;
  end: Date;
  blockStyle: CalendarBlockStyle;
}

export function EventDragPreview({
  top,
  height,
  left,
  width,
  title,
  start,
  end,
  blockStyle,
}: EventDragPreviewProps) {
  return (
    <div
      className="pointer-events-none absolute overflow-hidden rounded-shell-sm opacity-90 ring-2 ring-accent transition-[top,height,left,width] duration-100 ease-out"
      style={{
        top: `${top}px`,
        height: `${height}px`,
        left: `${left}%`,
        width: `calc(${width}% - 2px)`,
      }}
    >
      <EventVisual title={title} start={start} end={end} blockStyle={blockStyle} />
    </div>
  );
}
