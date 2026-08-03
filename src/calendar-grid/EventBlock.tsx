import type { OccurrenceLayout } from "../lib/layoutOverlappingEvents";
import type { Occurrence } from "../lib/occurrence";
import { durationToHeight, timeToY } from "../lib/gridTime";
import { getCalendarById } from "../lib/calendar";
import { getCalendarColorClass } from "../lib/calendarColors";
import { useCalendarsStore } from "../lib/calendarsStore";
import { columnLayoutToBox } from "../lib/eventBlockGeometry";
import { EventVisual } from "./EventVisual";

export type EventDragKind = "move" | "resize-start" | "resize-end";

interface EventBlockProps {
  layout: OccurrenceLayout;
  pixelsPerHour: number;
  now: Date;
  onOccurrenceClick: (occurrence: Occurrence) => void;
  onDragStart: (
    occurrence: Occurrence,
    kind: EventDragKind,
    clientX: number,
    clientY: number,
  ) => void;
}

export function EventBlock({
  layout,
  pixelsPerHour,
  now,
  onOccurrenceClick,
  onDragStart,
}: EventBlockProps) {
  const { occurrence, column, columnCount } = layout;
  const { event } = occurrence;
  const isPast = occurrence.end < now;
  const calendars = useCalendarsStore((state) => state.calendars);
  const calendar = getCalendarById(calendars, event.calendarId);
  const colorClass = calendar
    ? getCalendarColorClass(calendar.color)
    : "bg-calendar-graphite";

  const top = timeToY(occurrence.start, pixelsPerHour);
  const height = durationToHeight(occurrence.start, occurrence.end, pixelsPerHour);
  const { left, width } = columnLayoutToBox(column, columnCount);

  function handleEdgeMouseDown(
    domEvent: React.MouseEvent,
    kind: "resize-start" | "resize-end",
  ) {
    domEvent.stopPropagation();
    onDragStart(occurrence, kind, domEvent.clientX, domEvent.clientY);
  }

  return (
    <button
      type="button"
      onMouseDown={(domEvent) => {
        domEvent.stopPropagation();
        onDragStart(occurrence, "move", domEvent.clientX, domEvent.clientY);
      }}
      onClick={(domEvent) => {
        // event.detail is 0 for keyboard-triggered activations (Enter/Space)
        // and unset programmatic dispatches, but >=1 for real mouse clicks —
        // mouse clicks are already handled by the mousedown/mouseup drag-vs-click
        // distance check in TimeGrid, so only keyboard activation reaches here.
        if (domEvent.detail === 0) {
          onOccurrenceClick(occurrence);
        }
      }}
      className="absolute cursor-pointer transition-[left,width] duration-100 ease-out"
      style={{
        top: `${top}px`,
        height: `${height}px`,
        left: `${left}%`,
        width: `calc(${width}% - 2px)`,
      }}
    >
      <div
        onMouseDown={(domEvent) => handleEdgeMouseDown(domEvent, "resize-start")}
        className="absolute inset-x-0 top-0 z-10 h-1.5 cursor-ns-resize"
      />
      <EventVisual
        title={event.title}
        start={occurrence.start}
        end={occurrence.end}
        colorClass={colorClass}
        isPast={isPast}
      />
      <div
        onMouseDown={(domEvent) => handleEdgeMouseDown(domEvent, "resize-end")}
        className="absolute inset-x-0 bottom-0 z-10 h-1.5 cursor-ns-resize"
      />
    </button>
  );
}
