import { isSameDay } from "date-fns";
import { getCalendarById, isSubscribedCalendar } from "../lib/calendar";
import { getCalendarBlockStyle } from "../lib/calendarColors";
import { useCalendarsStore } from "../lib/calendarsStore";
import { occurrenceKey, type Occurrence } from "../lib/occurrence";

interface AllDayLaneProps {
  daysToShow: Date[];
  occurrences: Occurrence[];
  onOccurrenceClick: (occurrence: Occurrence) => void;
  onOccurrenceDragStart: (
    occurrence: Occurrence,
    clientX: number,
    clientY: number,
  ) => void;
  draggingKey: string | null;
  dragHoverDateKey: string | null;
}

/**
 * The all-day lane: a strip above the hourly grid in Day/Week view holding
 * all-day Occurrences as full-width chips, one column per visible day
 * (ADR-0017, CONTEXT.md's All-day lane). Renders nothing when there are no
 * all-day Occurrences to show. Each day column carries `data-allday-date` so
 * the drag handler (in TimeGrid) can resolve the date under the pointer the
 * same way MonthGrid resolves a Day cell — move only, no resize (ADR-0017).
 */
export function AllDayLane({
  daysToShow,
  occurrences,
  onOccurrenceClick,
  onOccurrenceDragStart,
  draggingKey,
  dragHoverDateKey,
}: AllDayLaneProps) {
  const calendars = useCalendarsStore((state) => state.calendars);

  if (occurrences.length === 0) return null;

  return (
    <div className="flex border-t border-border">
      <div className="w-14 shrink-0" />
      {daysToShow.map((day) => {
        const dayOccurrences = occurrences.filter((occurrence) =>
          isSameDay(occurrence.start, day),
        );
        const isDragHover = dragHoverDateKey === day.toDateString();
        return (
          <div
            key={day.toISOString()}
            data-allday-date={day.toDateString()}
            className={`flex flex-1 flex-col gap-0.5 border-l border-border p-0.5 ${isDragHover ? "bg-accent-soft" : ""}`}
          >
            {dayOccurrences.map((occurrence) => {
              const calendar = getCalendarById(calendars, occurrence.event.calendarId);
              const blockStyle = getCalendarBlockStyle(calendar);
              // Kept in the layout but hidden rather than removed: dropping
              // it from the DOM while dragging would shrink this day's
              // column (and the whole lane, which has no fixed height like
              // Month's grid cells) right when the drop target needs to
              // stay put.
              const isDragging = occurrenceKey(occurrence) === draggingKey;
              // A Subscribed Calendar's Occurrences don't drag (#84,
              // ADR-0032) — the gesture below is never started for one, so
              // its real mouse clicks fall through to the keyboard-only
              // branch instead.
              const isSubscribed = isSubscribedCalendar(calendar);
              return (
                <button
                  key={occurrenceKey(occurrence)}
                  type="button"
                  onMouseDown={(domEvent) => {
                    domEvent.stopPropagation();
                    if (isSubscribed) return;
                    onOccurrenceDragStart(occurrence, domEvent.clientX, domEvent.clientY);
                  }}
                  onClick={(domEvent) => {
                    // Real mouse clicks are handled by the mousedown/mouseup
                    // drag-vs-click distance check in TimeGrid; only keyboard
                    // activation (Enter/Space, event.detail === 0) reaches here.
                    if (domEvent.detail === 0 || isSubscribed) {
                      onOccurrenceClick(occurrence);
                    }
                  }}
                  style={blockStyle}
                  className={`cursor-pointer truncate rounded-shell-sm px-1 text-left text-label-sm ${isDragging ? "invisible" : ""}`}
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
