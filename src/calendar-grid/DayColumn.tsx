import { useEffect, useRef, useState } from "react";
import { isSameDay } from "date-fns";
import { layoutOverlappingEvents } from "../lib/layoutOverlappingEvents";
import { getDaySegments, type OccurrenceDaySegment } from "../lib/occurrenceSegments";
import { getTaskTimeBlockDaySegments, type TaskTimeBlockSegment } from "../lib/taskTimeBlockSegments";
import { occurrenceKey, type Occurrence } from "../lib/occurrence";
import type { Task } from "../lib/tasksApi";
import { useTaskListsStore } from "../lib/taskListsStore";
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
import { TaskBlock } from "./TaskBlock";
import { CurrentTimeLine } from "./CurrentTimeLine";
import { DraftBlockPreview } from "./DraftBlockPreview";
import { EventDragPreview } from "./EventDragPreview";
import { DragReadout } from "./DragReadout";

const DRAFT_PSEUDO_EVENT_ID = "__draft-preview__";

/**
 * One item the day column's overlap-layout pass can place — an Occurrence's
 * day segment or a Task's Time block day segment (#313, ADR-0083: Time
 * blocks join Events in overlap layout, ADR-0004, rather than drawing over
 * them). `layoutOverlappingEvents` only needs `start`/`end`, so this union
 * feeds it directly; the `kind` discriminant is this file's own, not
 * something either segment type carries.
 */
type GridBlockItem =
  | (OccurrenceDaySegment & { kind: "occurrence" })
  | (TaskTimeBlockSegment & { kind: "task" });

function isDraftItem(item: GridBlockItem): boolean {
  return item.kind === "occurrence" && item.occurrence.event.id === DRAFT_PSEUDO_EVENT_ID;
}

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
  /** Every Task placed on the hourly grid (`taskPlacement(task) === "grid"`)
   * that should be visible somewhere in the window — the caller (TimeGrid)
   * has already applied "Show tasks on calendar" and "Show completed", the
   * same division it makes for `deadlineTasks` (#312). This column filters
   * to the ones whose block actually touches `day`. */
  tasks: Task[];
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
  /** Rescheduling or resizing a Task's own Time block (#314) — the same
   * `onDragStart` shape `onOccurrenceDragStart` gives EventBlock, just keyed
   * on the Task rather than the Occurrence. */
  onTaskDragStart: (task: Task, kind: EventDragKind, clientX: number, clientY: number) => void;
  draggingTaskId: number | null;
  taskDragPreview: EventDragPreviewData | null;
}

export function DayColumn({
  day,
  occurrences,
  tasks,
  pixelsPerHour,
  now,
  onDraftCreated,
  onOccurrenceClick,
  onOccurrenceDragStart,
  draggingKey,
  eventDragPreview,
  isLastColumn,
  onTaskDragStart,
  draggingTaskId,
  taskDragPreview,
}: DayColumnProps) {
  const daySegments = getDaySegments(occurrences, day);
  const taskSegments = getTaskTimeBlockDaySegments(tasks, day);
  const taskLists = useTaskListsStore((state) => state.taskLists);
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

  // While a draft is being created, include it as a pseudo-segment in the
  // same overlap-layout pass as the real day segments, so existing events
  // shrink to make room for it instead of the preview overlapping them. A
  // create-drag never crosses midnight (bounded to this one day column), so
  // its segment is just its own unclipped bounds.
  const draftSegment: GridBlockItem | null = draftBlock
    ? {
        kind: "occurrence",
        occurrence: {
          event: {
            id: DRAFT_PSEUDO_EVENT_ID,
            calendarId: "",
            title: "",
            start: draftBlock.start,
            end: draftBlock.end,
          },
          start: draftBlock.start,
          end: draftBlock.end,
        },
        start: draftBlock.start,
        end: draftBlock.end,
      }
    : null;
  const occurrenceItems: GridBlockItem[] = daySegments.map((segment) => ({
    ...segment,
    kind: "occurrence",
  }));
  const taskItems: GridBlockItem[] = taskSegments.map((segment) => ({
    ...segment,
    kind: "task",
  }));
  const layoutInput = draftSegment
    ? [...occurrenceItems, ...taskItems, draftSegment]
    : [...occurrenceItems, ...taskItems];

  const allLayouts = layoutOverlappingEvents(layoutInput);
  const layouts = allLayouts.filter((layout) => {
    const item = layout.occurrence;
    if (isDraftItem(item)) return false;
    if (item.kind === "occurrence" && occurrenceKey(item.occurrence) === draggingKey) return false;
    if (item.kind === "task" && item.task.id === draggingTaskId) return false;
    return true;
  });
  const draftLayout = draftBlock
    ? allLayouts.find((layout) => isDraftItem(layout.occurrence))
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
      // Read by TaskRow's resolveGridDropTime (#313) to find which day
      // column, and which offset within it, a Task panel row was dropped on
      // — the same
      // data-attribute-plus-elementFromPoint technique AllDayLane/MonthGrid
      // already use for their own cross-cell drags, here reaching across
      // into the Tasks panel's own DOM subtree instead of just this grid's.
      data-grid-day-ms={day.getTime()}
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
      {layouts.map((layout) => {
        const item = layout.occurrence;
        if (item.kind === "occurrence") {
          return (
            <EventBlock
              key={occurrenceKey(item.occurrence)}
              occurrence={item.occurrence}
              segmentStart={item.start}
              segmentEnd={item.end}
              column={layout.column}
              columnCount={layout.columnCount}
              pixelsPerHour={pixelsPerHour}
              now={now}
              onOccurrenceClick={onOccurrenceClick}
              onDragStart={onOccurrenceDragStart}
            />
          );
        }
        return (
          <TaskBlock
            key={`task-${item.task.id}`}
            task={item.task}
            taskList={taskLists.find((list) => list.id === item.task.taskListId)}
            segmentStart={item.start}
            segmentEnd={item.end}
            column={layout.column}
            columnCount={layout.columnCount}
            pixelsPerHour={pixelsPerHour}
            onDragStart={onTaskDragStart}
          />
        );
      })}
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
      {taskDragPreview && (
        <EventDragPreview
          top={taskDragPreview.top}
          height={taskDragPreview.height}
          left={taskDragPreview.left}
          width={taskDragPreview.width}
          title={taskDragPreview.title}
          start={taskDragPreview.start}
          end={taskDragPreview.end}
          blockStyle={taskDragPreview.blockStyle}
        />
      )}
      {taskDragPreview?.showReadout && (
        <DragReadout
          top={taskDragPreview.top}
          height={taskDragPreview.height}
          left={taskDragPreview.left}
          width={taskDragPreview.width}
          start={taskDragPreview.start}
          end={taskDragPreview.end}
          columnWidth={taskDragPreview.columnWidth}
          isLastColumn={taskDragPreview.isLastColumn}
        />
      )}
    </div>
  );
}
