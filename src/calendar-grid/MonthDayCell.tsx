import { useEffect, useRef, useState } from "react";
import { format } from "date-fns";
import type { Event } from "../lib/event";
import { getCalendarById } from "../lib/calendar";
import { getCalendarColorClass } from "../lib/calendarColors";
import { useCalendarsStore } from "../lib/calendarsStore";
import { useShellStore } from "../lib/shellStore";
import { computeChipCapacity } from "../lib/monthGrid";

const CHIP_ROW_HEIGHT_PX = 20;
const MORE_ROW_HEIGHT_PX = 20;

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
  const setSelectedDate = useShellStore((state) => state.setSelectedDate);
  const setActiveView = useShellStore((state) => state.setActiveView);

  const eventsContainerRef = useRef<HTMLDivElement>(null);
  const [availableHeight, setAvailableHeight] = useState(0);

  useEffect(() => {
    const container = eventsContainerRef.current;
    if (!container) return;

    const observer = new ResizeObserver(([entry]) => {
      setAvailableHeight(entry.contentRect.height);
    });
    observer.observe(container);
    return () => observer.disconnect();
  }, []);

  function openDayView() {
    setSelectedDate(date);
    setActiveView("day");
  }

  const { visibleCount, overflowCount } = computeChipCapacity(
    events.length,
    availableHeight,
    CHIP_ROW_HEIGHT_PX,
    MORE_ROW_HEIGHT_PX,
  );
  const visibleEvents = events.slice(0, visibleCount);

  return (
    <div
      className={`flex min-h-0 flex-col gap-1 overflow-hidden border-b border-l border-border p-1 ${inCurrentMonth ? "" : "bg-surface-sunken"}`}
    >
      <button
        type="button"
        onClick={openDayView}
        className={`shrink-0 self-start text-label-sm ${inCurrentMonth ? "text-ink" : "text-ink-muted"} ${
          isToday
            ? "flex h-5 w-5 items-center justify-center rounded-shell-pill bg-accent text-ink-inverse"
            : "cursor-pointer rounded-shell-sm px-1 hover:bg-surface-hover"
        }`}
      >
        {format(date, "d")}
      </button>
      <div
        ref={eventsContainerRef}
        className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-hidden"
      >
        {visibleEvents.map((event) => {
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
        {overflowCount > 0 && (
          <button
            type="button"
            onClick={openDayView}
            className="cursor-pointer truncate rounded-shell-sm px-1 text-left text-label-sm text-ink-muted hover:text-ink"
          >
            +{overflowCount} more
          </button>
        )}
      </div>
    </div>
  );
}
