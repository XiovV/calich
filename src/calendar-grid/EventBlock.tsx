import { format } from "date-fns";
import type { EventLayout } from "../lib/layoutOverlappingEvents";
import type { Event } from "../lib/mockEvents";
import { durationToHeight, timeToY } from "../lib/gridTime";
import { getCalendarById } from "../lib/mockCalendars";
import { getCalendarColorClass } from "../lib/calendarColors";
import { useCalendarsStore } from "../lib/calendarsStore";

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

  const top = timeToY(event.start, pixelsPerHour);
  const height = durationToHeight(event.start, event.end, pixelsPerHour);
  const width = 100 / columnCount;
  const left = column * width;

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
        // distance check in WeekGrid, so only keyboard activation reaches here.
        if (domEvent.detail === 0) {
          onEventClick(event);
        }
      }}
      className={`absolute cursor-pointer overflow-hidden rounded-shell-sm px-1.5 py-1 text-left text-ink-inverse ${calendar ? getCalendarColorClass(calendar.color) : "bg-calendar-graphite"}`}
      style={{
        top: `${top}px`,
        height: `${height}px`,
        left: `${left}%`,
        width: `calc(${width}% - 2px)`,
      }}
    >
      <div
        onMouseDown={(domEvent) => handleEdgeMouseDown(domEvent, "resize-start")}
        className="absolute inset-x-0 top-0 h-1.5 cursor-ns-resize"
      />
      <p className="truncate text-label-sm font-medium">{event.title}</p>
      <p className="truncate text-label-sm opacity-90">
        {format(event.start, "h:mm a")} – {format(event.end, "h:mm a")}
      </p>
      <div
        onMouseDown={(domEvent) => handleEdgeMouseDown(domEvent, "resize-end")}
        className="absolute inset-x-0 bottom-0 h-1.5 cursor-ns-resize"
      />
    </button>
  );
}
