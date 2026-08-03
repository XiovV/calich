import { isSameDay } from "date-fns";
import { useShellStore } from "../lib/shellStore";
import { useEventsStore } from "../lib/eventsStore";
import { buildMonthGrid, getEventsForDay } from "../lib/monthGrid";
import { MonthDayCell } from "./MonthDayCell";

const WEEKDAY_LABELS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

export function MonthGrid() {
  const selectedDate = useShellStore((state) => state.selectedDate);
  const checkedCalendarIds = useShellStore((state) => state.checkedCalendarIds);
  const events = useEventsStore((state) => state.events);

  const visibleEvents = events.filter((event) =>
    checkedCalendarIds.has(event.calendarId),
  );
  const cells = buildMonthGrid(selectedDate);
  const now = new Date();

  return (
    <div className="flex h-full flex-col">
      <div className="grid grid-cols-7 border-t border-border">
        {WEEKDAY_LABELS.map((label) => (
          <div
            key={label}
            className="border-b border-l border-border py-2 text-center text-label-sm text-ink-muted"
          >
            {label}
          </div>
        ))}
      </div>
      <div className="grid flex-1 grid-cols-7 grid-rows-6">
        {cells.map(({ date, inCurrentMonth }) => (
          <MonthDayCell
            key={date.toISOString()}
            date={date}
            inCurrentMonth={inCurrentMonth}
            isToday={isSameDay(date, now)}
            events={getEventsForDay(visibleEvents, date)}
          />
        ))}
      </div>
    </div>
  );
}
