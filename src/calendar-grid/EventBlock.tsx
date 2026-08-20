import type { Occurrence } from "../lib/occurrence";
import { durationToHeight, timeToY } from "../lib/gridTime";
import { canWriteCalendarEvents, getCalendarById } from "../lib/calendar";
import { getOccurrenceBlockStyle } from "../lib/calendarColors";
import { useCalendarsStore } from "../lib/calendarsStore";
import { columnLayoutToBox } from "../lib/eventBlockGeometry";
import { EventVisual } from "./EventVisual";
import { GRID_Z_OCCURRENCE_EDGE } from "./gridStacking";

export type EventDragKind = "move" | "resize-start" | "resize-end";

interface EventBlockProps {
  occurrence: Occurrence;
  /**
   * The Occurrence's span clipped to the day this block renders in — equal
   * to `occurrence.start`/`occurrence.end` unless the Occurrence crosses
   * midnight, in which case this is just today's segment (issue #230). Drives
   * the block's position and height; `occurrence.start`/`end` (the true,
   * uncropped bounds) still drive what's displayed and what a drag commits.
   */
  segmentStart: Date;
  segmentEnd: Date;
  column: number;
  columnCount: number;
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
  occurrence,
  segmentStart,
  segmentEnd,
  column,
  columnCount,
  pixelsPerHour,
  now,
  onOccurrenceClick,
  onDragStart,
}: EventBlockProps) {
  const { event } = occurrence;
  const isPast = occurrence.end < now;
  const calendars = useCalendarsStore((state) => state.calendars);
  const calendar = getCalendarById(calendars, event.calendarId);
  const blockStyle = getOccurrenceBlockStyle(event, calendar);
  // A Calendar's Occurrences are read-only unless the caller may write its
  // Events (#111, ADR-0034) — Viewer Access or a Subscription both resolve
  // here. No drag-to-move, no resize handles: the gesture is never started
  // at all rather than started and silently discarded, so no drag preview
  // flashes and snaps back.
  const isReadOnly = !canWriteCalendarEvents(calendar);
  // A midnight-crossing Occurrence's non-start-day segments have no real
  // start edge to resize from, and its non-end-day segments have no real
  // end edge — only the segment that actually carries that edge gets the
  // handle for it (issue #230).
  const showResizeStart = segmentStart.getTime() === occurrence.start.getTime();
  const showResizeEnd = segmentEnd.getTime() === occurrence.end.getTime();

  const top = timeToY(segmentStart, pixelsPerHour);
  const height = durationToHeight(segmentStart, segmentEnd, pixelsPerHour);
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
        if (isReadOnly) return;
        onDragStart(occurrence, "move", domEvent.clientX, domEvent.clientY);
      }}
      onClick={(domEvent) => {
        // event.detail is 0 for keyboard-triggered activations (Enter/Space)
        // and unset programmatic dispatches, but >=1 for real mouse clicks —
        // mouse clicks are already handled by the mousedown/mouseup drag-vs-click
        // distance check in TimeGrid, so only keyboard activation reaches here.
        // A read-only Occurrence never starts that drag-vs-click gesture
        // above, so its real mouse clicks fall through to here too.
        if (domEvent.detail === 0 || isReadOnly) {
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
      {!isReadOnly && showResizeStart && (
        <div
          onMouseDown={(domEvent) => handleEdgeMouseDown(domEvent, "resize-start")}
          className="absolute inset-x-0 top-0 h-1.5 cursor-ns-resize"
          style={{ zIndex: GRID_Z_OCCURRENCE_EDGE }}
        />
      )}
      <EventVisual
        title={event.title}
        start={occurrence.start}
        end={occurrence.end}
        blockStyle={blockStyle}
        isPast={isPast}
        hasAttachments={Boolean(event.attachments?.length)}
      />
      {!isReadOnly && showResizeEnd && (
        <div
          onMouseDown={(domEvent) => handleEdgeMouseDown(domEvent, "resize-end")}
          className="absolute inset-x-0 bottom-0 h-1.5 cursor-ns-resize"
          style={{ zIndex: GRID_Z_OCCURRENCE_EDGE }}
        />
      )}
    </button>
  );
}
