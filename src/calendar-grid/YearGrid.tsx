import { useShellStore } from "../lib/shellStore";
import { useEventsStore } from "../lib/eventsStore";
import { buildDaysWithEvents, buildYearMonths } from "../lib/yearGrid";
import { MiniMonth } from "./MiniMonth";

/**
 * The read-only Year overview: twelve Mini-months in a 4×3 grid for the Selected
 * date's year. Reduces the visible Events (checked Calendars only) to a set of
 * day keys that drive each Mini-month's Event-presence dots.
 */
export function YearGrid() {
  const selectedDate = useShellStore((state) => state.selectedDate);
  const checkedCalendarIds = useShellStore((state) => state.checkedCalendarIds);
  const events = useEventsStore((state) => state.events);

  const months = buildYearMonths(selectedDate);
  const daysWithEvents = buildDaysWithEvents(events, checkedCalendarIds);

  return (
    <div className="h-full overflow-auto p-4">
      <div className="mx-auto grid max-w-5xl grid-cols-4 grid-rows-3 gap-4">
        {months.map((month) => (
          <MiniMonth
            key={month.getMonth()}
            month={month}
            daysWithEvents={daysWithEvents}
          />
        ))}
      </div>
    </div>
  );
}
