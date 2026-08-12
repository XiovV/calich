import { useEffect, useRef, useState } from "react";
import { isSameDay } from "date-fns";
import { layoutOverlappingEvents } from "../lib/layoutOverlappingEvents";
import { occurrenceKey, type Occurrence } from "../lib/occurrence";
import { useWorkingHours } from "../hooks/useWorkingHours";
import { CLICK_DISTANCE_THRESHOLD_PX, isDragGesture } from "../lib/pointerDrag";
import {
  HOURS_IN_DAY,
  computeDraftBlock,
  durationToHeight,
  timeToY,
  type DraftBlock,
} from "../lib/gridTime";
import { columnLayoutToBox } from "../lib/eventBlockGeometry";
import type { CalendarBlockStyle } from "../lib/calendarColors";
import { EventBlock, type EventDragKind } from "./EventBlock";
import { CurrentTimeLine } from "./CurrentTimeLine";
import { DraftBlockPreview } from "./DraftBlockPreview";
import { EventDragPreview } from "./EventDragPreview";
import { DragReadout } from "./DragReadout";

const DRAFT_PSEUDO_EVENT_ID = "__draft-preview__";

export interface EventDragPreviewData {
  top: number;
  height: number;
  left: number;
  width: number;
  title: string;
  start: Date;
  end: Date;
  blockStyle: CalendarBlockStyle;
  columnWidth: number;
  isLastColumn: boolean;
  showReadout: boolean;
}

interface DayColumnProps {
  day: Date;
  occurrences: Occurrence[];
  pixelsPerHour: number;
  now: Date;
  onDraftCreated: (day: Date, draft: DraftBlock) => void;
  onOccurrenceClick: (occurrence: Occurrence) => void;
  onOccurrenceDragStart: (
    occurrence: Occurrence,
    kind: EventDragKind,
    clientX: number,
    clientY: number,
  ) => void;
  draggingKey: string | null;
  eventDragPreview: EventDragPreviewData | null;
  isLastColumn: boolean;
}

export function DayColumn({
  day,
  occurrences,
  pixelsPerHour,
  now,
  onDraftCreated,
  onOccurrenceClick,
  onOccurrenceDragStart,
  draggingKey,
  eventDragPreview,
  isLastColumn,
}: DayColumnProps) {
  const dayOccurrences = occurrences.filter((occurrence) =>
    isSameDay(occurrence.start, day),
  );
  const isToday = isSameDay(day, now);
  const workingHours = useWorkingHours();

  const columnRef = useRef<HTMLDivElement>(null);
  const [dragStartY, setDragStartY] = useState<number | null>(null);
  const [dragCurrentY, setDragCurrentY] = useState<number | null>(null);
  const [dragColumnWidth, setDragColumnWidth] = useState(0);

  const draftBlock =
    dragStartY !== null && dragCurrentY !== null
      ? computeDraftBlock(day, dragStartY, dragCurrentY, pixelsPerHour)
      : null;

  // Vertical-only: a create-drag never moves across day columns, so the x
  // component of the click-vs-drag threshold is always 0.
  const showReadout =
    dragStartY !== null &&
    dragCurrentY !== null &&
    isDragGesture(
      { x: 0, y: dragCurrentY - dragStartY },
      CLICK_DISTANCE_THRESHOLD_PX,
    );

  // While a draft is being created, include it as a pseudo-occurrence in the
  // same overlap-layout pass as the real occurrences, so existing events shrink
  // to make room for it instead of the preview overlapping them.
  const draftOccurrence: Occurrence | null = draftBlock
    ? {
        event: {
          id: DRAFT_PSEUDO_EVENT_ID,
          calendarId: "",
          title: "",
          start: draftBlock.start,
          end: draftBlock.end,
        },
        start: draftBlock.start,
        end: draftBlock.end,
      }
    : null;
  const layoutInput = draftOccurrence
    ? [...dayOccurrences, draftOccurrence]
    : dayOccurrences;

  const allLayouts = layoutOverlappingEvents(layoutInput);
  const layouts = allLayouts.filter(
    (layout) =>
      layout.occurrence.event.id !== DRAFT_PSEUDO_EVENT_ID &&
      occurrenceKey(layout.occurrence) !== draggingKey,
  );
  const draftLayout = draftBlock
    ? allLayouts.find(
        (layout) => layout.occurrence.event.id === DRAFT_PSEUDO_EVENT_ID,
      )
    : undefined;

  function offsetYFromEvent(clientY: number): number {
    const rect = columnRef.current?.getBoundingClientRect();
    return rect ? clientY - rect.top : 0;
  }

  function handleMouseDown(event: React.MouseEvent) {
    const offsetY = offsetYFromEvent(event.clientY);
    setDragStartY(offsetY);
    setDragCurrentY(offsetY);
    setDragColumnWidth(columnRef.current?.getBoundingClientRect().width ?? 0);
  }

  useEffect(() => {
    if (dragStartY === null) return;
    const startY = dragStartY;

    function handleMouseMove(event: MouseEvent) {
      setDragCurrentY(offsetYFromEvent(event.clientY));
    }

    function handleMouseUp(event: MouseEvent) {
      const endY = offsetYFromEvent(event.clientY);
      const draft = computeDraftBlock(day, startY, endY, pixelsPerHour);
      onDraftCreated(day, draft);
      setDragStartY(null);
      setDragCurrentY(null);
    }

    window.addEventListener("mousemove", handleMouseMove);
    window.addEventListener("mouseup", handleMouseUp);
    return () => {
      window.removeEventListener("mousemove", handleMouseMove);
      window.removeEventListener("mouseup", handleMouseUp);
    };
  }, [dragStartY, day, pixelsPerHour, onDraftCreated]);

  return (
    <div
      ref={columnRef}
      onMouseDown={handleMouseDown}
      className="relative flex-1 border-l border-border select-none"
      style={{ height: pixelsPerHour * HOURS_IN_DAY }}
    >
      {workingHours && workingHours.start > 0 && (
        <div
          className="pointer-events-none absolute inset-x-0 top-0 bg-ink/5 dark:bg-ink/12"
          style={{ height: (workingHours.start / 60) * pixelsPerHour }}
        />
      )}
      {workingHours && workingHours.end < HOURS_IN_DAY * 60 && (
        <div
          className="pointer-events-none absolute inset-x-0 bottom-0 bg-ink/5 dark:bg-ink/12"
          style={{ height: ((HOURS_IN_DAY * 60 - workingHours.end) / 60) * pixelsPerHour }}
        />
      )}
      {Array.from({ length: HOURS_IN_DAY }, (_, hour) => (
        <div
          key={hour}
          className="absolute inset-x-0 border-t border-border"
          style={{ top: hour * pixelsPerHour }}
        />
      ))}
      {layouts.map((layout) => (
        <EventBlock
          key={occurrenceKey(layout.occurrence)}
          layout={layout}
          pixelsPerHour={pixelsPerHour}
          now={now}
          onOccurrenceClick={onOccurrenceClick}
          onDragStart={onOccurrenceDragStart}
        />
      ))}
      {isToday && <CurrentTimeLine now={now} pixelsPerHour={pixelsPerHour} />}
      {draftBlock &&
        draftLayout &&
        (() => {
          const { left, width } = columnLayoutToBox(
            draftLayout.column,
            draftLayout.columnCount,
          );
          const top = timeToY(draftBlock.start, pixelsPerHour);
          const height = durationToHeight(
            draftBlock.start,
            draftBlock.end,
            pixelsPerHour,
          );
          return (
            <>
              <DraftBlockPreview top={top} height={height} left={left} width={width} />
              {showReadout && (
                <DragReadout
                  top={top}
                  height={height}
                  left={left}
                  width={width}
                  start={draftBlock.start}
                  end={draftBlock.end}
                  columnWidth={dragColumnWidth}
                  isLastColumn={isLastColumn}
                />
              )}
            </>
          );
        })()}
      {eventDragPreview && (
        <EventDragPreview
          top={eventDragPreview.top}
          height={eventDragPreview.height}
          left={eventDragPreview.left}
          width={eventDragPreview.width}
          title={eventDragPreview.title}
          start={eventDragPreview.start}
          end={eventDragPreview.end}
          blockStyle={eventDragPreview.blockStyle}
        />
      )}
      {eventDragPreview?.showReadout && (
        <DragReadout
          top={eventDragPreview.top}
          height={eventDragPreview.height}
          left={eventDragPreview.left}
          width={eventDragPreview.width}
          start={eventDragPreview.start}
          end={eventDragPreview.end}
          columnWidth={eventDragPreview.columnWidth}
          isLastColumn={eventDragPreview.isLastColumn}
        />
      )}
    </div>
  );
}
