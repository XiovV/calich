import type { EventLayout } from "../lib/layoutOverlappingEvents";
import type { Event } from "../lib/mockEvents";
import { durationToHeight, timeToY } from "../lib/gridTime";
import { getCalendarById } from "../lib/mockCalendars";
import { getCalendarColorClass } from "../lib/calendarColors";
import { useCalendarsStore } from "../lib/calendarsStore";
import { columnLayoutToBox } from "../lib/eventBlockGeometry";
import { EventVisual } from "./EventVisual";

export type EventDragKind = "move" | "resize-start" | "resize-end";

interface EventBlockProps {
  layout: EventLayout;
  pixelsPerHour: number;
  onEventClick: (event: Event) => void;
  onDragStart: (
    event: Event,
    kind: EventDragKind,
    clientX: number,
    clientY: number,
  ) => void;
}

export function EventBlock({
  layout,
  pixelsPerHour,
  onEventClick,
  onDragStart,
}: EventBlockProps) {
  const { event, column, columnCount } = layout;
  const calendars = useCalendarsStore((state) => state.calendars);
  const calendar = getCalendarById(calendars, event.calendarId);
  const colorClass = calendar
    ? getCalendarColorClass(calendar.color)
    : "bg-calendar-graphite";

  const top = timeToY(event.start, pixelsPerHour);
  const height = durationToHeight(event.start, event.end, pixelsPerHour);
  const { left, width } = columnLayoutToBox(column, columnCount);

  function handleEdgeMouseDown(
    domEvent: React.MouseEvent,
    kind: "resize-start" | "resize-end",
  ) {
    domEvent.stopPropagation();
    onDragStart(event, kind, domEvent.clientX, domEvent.clientY);
  }

  return (
    <button
      type="button"
      onMouseDown={(domEvent) => {
        domEvent.stopPropagation();
        onDragStart(event, "move", domEvent.clientX, domEvent.clientY);
      }}
      onClick={(domEvent) => {
        // event.detail is 0 for keyboard-triggered activations (Enter/Space)
        // and unset programmatic dispatches, but >=1 for real mouse clicks —
        // mouse clicks are already handled by the mousedown/mouseup drag-vs-click
        // distance check in TimeGrid, so only keyboard activation reaches here.
        if (domEvent.detail === 0) {
          onEventClick(event);
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
        start={event.start}
        end={event.end}
        colorClass={colorClass}
      />
      <div
        onMouseDown={(domEvent) => handleEdgeMouseDown(domEvent, "resize-end")}
        className="absolute inset-x-0 bottom-0 z-10 h-1.5 cursor-ns-resize"
      />
    </button>
  );
}
