import { AttachmentIndicator } from "./AttachmentIndicator";
import { canWriteCalendarEvents, getCalendarById } from "../lib/calendar";
import { getOccurrenceBlockStyle } from "../lib/calendarColors";
import { useCalendarsStore } from "../lib/calendarsStore";
import { occurrenceIntersectsDay } from "../lib/occurrenceSegments";
import { occurrenceKey, type Occurrence } from "../lib/occurrence";

interface AllDayLaneProps {
  daysToShow: Date[];
  occurrences: Occurrence[];
  onOccurrenceClick: (occurrence: Occurrence) => void;
  onOccurrenceDragStart: (
    occurrence: Occurrence,
    dragStartDay: Date,
    clientX: number,
    clientY: number,
  ) => void;
  draggingKey: string | null;
  dragHoverDateKey: string | null;
}

/**
 * The all-day lane: a strip above the hourly grid in Day/Week view holding
 * all-day Occurrences as full-width chips, one column per visible day
 * (ADR-0017, ADR-0069, CONTEXT.md's All-day lane). Renders nothing when there
 * are no all-day Occurrences to show. An Occurrence spanning several dates
 * gets a chip in every day column it touches (#232), each opening the same
 * Event. Each day column carries `data-allday-date` so the drag handler (in
 * TimeGrid) can resolve the date under the pointer the same way MonthGrid
 * resolves a Day cell — move only, no resize (ADR-0017).
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
      <div className="w-18 shrink-0" />
      {daysToShow.map((day) => {
        const dayOccurrences = occurrences.filter((occurrence) =>
          occurrenceIntersectsDay(occurrence, day),
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
              const blockStyle = getOccurrenceBlockStyle(occurrence.event, calendar);
              // Kept in the layout but hidden rather than removed: dropping
              // it from the DOM while dragging would shrink this day's
              // column (and the whole lane, which has no fixed height like
              // Month's grid cells) right when the drop target needs to
              // stay put.
              const isDragging = occurrenceKey(occurrence) === draggingKey;
              // An Occurrence on a Calendar the caller can't write doesn't
              // drag (#111, ADR-0034) — the gesture below is never started
              // for one, so its real mouse clicks fall through to the
              // keyboard-only branch instead.
              const isReadOnly = !canWriteCalendarEvents(calendar);
              return (
                <button
                  key={occurrenceKey(occurrence)}
                  type="button"
                  onMouseDown={(domEvent) => {
                    domEvent.stopPropagation();
                    if (isReadOnly) return;
                    onOccurrenceDragStart(occurrence, day, domEvent.clientX, domEvent.clientY);
                  }}
                  onClick={(domEvent) => {
                    // Real mouse clicks are handled by the mousedown/mouseup
                    // drag-vs-click distance check in TimeGrid; only keyboard
                    // activation (Enter/Space, event.detail === 0) reaches here.
                    if (domEvent.detail === 0 || isReadOnly) {
                      onOccurrenceClick(occurrence);
                    }
                  }}
                  style={blockStyle}
                  className={`flex w-full cursor-pointer items-center gap-1 rounded-shell-sm px-1 text-left text-label-sm ${isDragging ? "invisible" : ""}`}
                >
                  <span className="truncate">{occurrence.event.title}</span>
                  <AttachmentIndicator
                    hasAttachments={Boolean(occurrence.event.attachments?.length)}
                  />
                </button>
              );
            })}
          </div>
        );
      })}
    </div>
  );
}
