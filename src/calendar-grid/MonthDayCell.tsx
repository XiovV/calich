import { format } from "date-fns";
import type { Event } from "../lib/event";
import { getCalendarById } from "../lib/calendar";
import { getCalendarColorClass } from "../lib/calendarColors";
import { useCalendarsStore } from "../lib/calendarsStore";

interface MonthDayCellProps {
  date: Date;
  inCurrentMonth: boolean;
  isToday: boolean;
  events: Event[];
}

export function MonthDayCell({
  date,
  inCurrentMonth,
  isToday,
  events,
}: MonthDayCellProps) {
  const calendars = useCalendarsStore((state) => state.calendars);

  return (
    <div
      className={`flex min-h-0 flex-col gap-1 overflow-hidden border-b border-l border-border p-1 ${inCurrentMonth ? "" : "bg-surface-sunken"}`}
    >
      <span
        className={`shrink-0 self-start text-label-sm ${inCurrentMonth ? "text-ink" : "text-ink-muted"} ${
          isToday
            ? "flex h-5 w-5 items-center justify-center rounded-shell-pill bg-accent text-ink-inverse"
            : "px-1"
        }`}
      >
        {format(date, "d")}
      </span>
      <div className="flex min-h-0 flex-col gap-0.5 overflow-hidden">
        {events.map((event) => {
          const calendar = getCalendarById(calendars, event.calendarId);
          const colorClass = calendar
            ? getCalendarColorClass(calendar.color)
            : "bg-calendar-graphite";
          return (
            <div
              key={event.id}
              className={`truncate rounded-shell-sm px-1 text-label-sm text-ink-inverse ${colorClass}`}
            >
              {format(event.start, "h:mm a")} {event.title}
            </div>
          );
        })}
      </div>
    </div>
  );
}
