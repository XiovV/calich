import { format } from "date-fns";
import type { EventLayout } from "../lib/layoutOverlappingEvents";
import { durationToHeight, timeToY } from "../lib/gridTime";
import { getCalendarById } from "../lib/mockCalendars";
import { getCalendarColorClass } from "../lib/calendarColors";
import { useCalendarsStore } from "../lib/calendarsStore";

interface EventBlockProps {
  layout: EventLayout;
  pixelsPerHour: number;
}

export function EventBlock({ layout, pixelsPerHour }: EventBlockProps) {
  const { event, column, columnCount } = layout;
  const calendars = useCalendarsStore((state) => state.calendars);
  const calendar = getCalendarById(calendars, event.calendarId);

  const top = timeToY(event.start, pixelsPerHour);
  const height = durationToHeight(event.start, event.end, pixelsPerHour);
  const width = 100 / columnCount;
  const left = column * width;

  return (
    <div
      className={`absolute overflow-hidden rounded-shell-sm px-1.5 py-1 text-ink-inverse ${calendar ? getCalendarColorClass(calendar.color) : "bg-calendar-graphite"}`}
      style={{
        top: `${top}px`,
        height: `${height}px`,
        left: `${left}%`,
        width: `calc(${width}% - 2px)`,
      }}
    >
      <p className="truncate text-label-sm font-medium">{event.title}</p>
      <p className="truncate text-label-sm opacity-90">
        {format(event.start, "h:mm a")} – {format(event.end, "h:mm a")}
      </p>
    </div>
  );
}
