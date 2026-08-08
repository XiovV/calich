import { format } from "date-fns";
import type { CalendarBlockStyle } from "../lib/calendarColors";
import { useTimePattern } from "../hooks/useTimePattern";

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
  const timePattern = useTimePattern();
  return (
    <div className="h-full w-full overflow-hidden rounded-shell-sm bg-surface">
      <div
        style={blockStyle}
        className={`h-full w-full px-1.5 py-1 text-left ${isPast ? "opacity-60" : ""}`}
      >
        <p className="truncate text-label-sm font-medium">{title}</p>
        <p className="truncate text-label-sm opacity-90">
          {format(start, timePattern)} – {format(end, timePattern)}
        </p>
      </div>
    </div>
  );
}
