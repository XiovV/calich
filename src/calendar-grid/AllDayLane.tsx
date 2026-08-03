import { isSameDay } from "date-fns";
import { getCalendarById } from "../lib/calendar";
import { getCalendarColorClass } from "../lib/calendarColors";
import { useCalendarsStore } from "../lib/calendarsStore";
import { occurrenceKey, type Occurrence } from "../lib/occurrence";

interface AllDayLaneProps {
  daysToShow: Date[];
  occurrences: Occurrence[];
  onOccurrenceClick: (occurrence: Occurrence) => void;
}

/**
 * The all-day lane: a strip above the hourly grid in Day/Week view holding
 * all-day Occurrences as full-width chips, one column per visible day
 * (ADR-0017, CONTEXT.md's All-day lane). Renders nothing when there are no
 * all-day Occurrences to show.
 */
export function AllDayLane({ daysToShow, occurrences, onOccurrenceClick }: AllDayLaneProps) {
  const calendars = useCalendarsStore((state) => state.calendars);

  if (occurrences.length === 0) return null;

  return (
    <div className="flex border-t border-border">
      <div className="w-14 shrink-0" />
      {daysToShow.map((day) => {
        const dayOccurrences = occurrences.filter((occurrence) =>
          isSameDay(occurrence.start, day),
        );
        return (
          <div
            key={day.toISOString()}
            className="flex flex-1 flex-col gap-0.5 border-l border-border p-0.5"
          >
            {dayOccurrences.map((occurrence) => {
              const calendar = getCalendarById(calendars, occurrence.event.calendarId);
              const colorClass = calendar
                ? getCalendarColorClass(calendar.color)
                : "bg-calendar-graphite";
              return (
                <button
                  key={occurrenceKey(occurrence)}
                  type="button"
                  onClick={() => onOccurrenceClick(occurrence)}
                  className={`cursor-pointer truncate rounded-shell-sm px-1 text-left text-label-sm text-ink-inverse ${colorClass}`}
                >
                  {occurrence.event.title}
                </button>
              );
            })}
          </div>
        );
      })}
    </div>
  );
}
