import { format } from "date-fns";
import type { CalendarBlockStyle } from "../lib/calendarColors";

interface EventVisualProps {
  title: string;
  start: Date;
  end: Date;
  blockStyle: CalendarBlockStyle;
  isPast?: boolean;
}

export function EventVisual({
  title,
  start,
  end,
  blockStyle,
  isPast,
}: EventVisualProps) {
  return (
    <div className="h-full w-full overflow-hidden rounded-shell-sm bg-surface">
      <div
        style={blockStyle}
        className={`h-full w-full px-1.5 py-1 text-left ${isPast ? "opacity-60" : ""}`}
      >
        <p className="truncate text-label-sm font-medium">{title}</p>
        <p className="truncate text-label-sm opacity-90">
          {format(start, "h:mm a")} – {format(end, "h:mm a")}
        </p>
      </div>
    </div>
  );
}
