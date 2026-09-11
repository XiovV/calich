import { useEffect, useRef, useState } from "react";
import { addDays, format, isSameDay, startOfDay } from "date-fns";
import { useCalendarsStore } from "../lib/calendarsStore";
import { getCalendarById, type Calendar } from "../lib/calendar";
import { getCalendarBlockStyle, getOccurrenceBlockStyle } from "../lib/calendarColors";
import { layoutOverlappingEvents } from "../lib/layoutOverlappingEvents";
import { columnLayoutToBox } from "../lib/eventBlockGeometry";
import { useVisibleOccurrences } from "../hooks/useVisibleOccurrences";
import { usePointerDrag, type DragState } from "../hooks/usePointerDrag";
import { CLICK_DISTANCE_THRESHOLD_PX, isDragGesture, type Point } from "../lib/pointerDrag";
import { occurrenceKey, type Occurrence } from "../lib/occurrence";
import { viewerZone } from "../lib/floatingTime";
import { useShellStore } from "../lib/shellStore";
import { taskDeadlineFallsOnDay, taskPlacement } from "../lib/taskScheduling";
import { taskDropIntent } from "../lib/taskDropIntent";
import { getTaskTimeBlockDaySegments, taskTimeBlockEnd } from "../lib/taskTimeBlockSegments";
import { useTaskListsStore } from "../lib/taskListsStore";
import type { TaskList } from "../lib/taskListsApi";
import type { Task } from "../lib/tasksApi";
import { useTasksStore } from "../lib/tasksStore";
import { resolveGridDropTime, isPointOverTasksPanel } from "./gridDropTargets";
import {
  PIXELS_PER_HOUR,
  computeMoveToDate,
  computeMovedEventTimes,
  computeResizedEventTimes,
  durationToHeight,
  resolveDragTargetDate,
  timeToY,
  type DraftBlock,
} from "../lib/gridTime";
import { TimeAxis } from "./TimeAxis";
import { AllDayLane } from "./AllDayLane";
import { AllDayEventDragPreview } from "./AllDayEventDragPreview";
import { DayColumn, type EventDragPreviewData } from "./DayColumn";
import type { EventDragKind } from "./EventBlock";
import { ScopePicker } from "./ScopePicker";
import { TaskDragPreview } from "./TaskDragPreview";
import { useOccurrenceDragCommit } from "./useOccurrenceDragCommit";
import { GRID_Z_HEADER } from "./gridStacking";

const NOW_REFRESH_INTERVAL_MS = 60_000;

interface TimeGridProps {
  daysToShow: Date[];
  onDraftCreated: (day: Date, draft: DraftBlock) => void;
  onOccurrenceClick: (occurrence: Occurrence) => void;
}

interface EventDragPayload {
  occurrence: Occurrence;
  kind: EventDragKind;
  columnWidth: number;
}

/**
 * An all-day drag's Occurrence plus the day column the gesture picked it up
 * from — needed because a multi-day Occurrence (#232) renders a chip on
 * every day it spans, not just its true start.
 */
interface AllDayDragPayload {
  occurrence: Occurrence;
  dragStartDay: Date;
}

/**
 * A Task's own grid drag (#314): either an existing Time block being
 * rescheduled or resized, or a Deadline-only chip picked up off the all-day
 * lane. The two sources resolve their drop differently (delta math for a
 * block already on the grid; `resolveGridDropTime`'s elementFromPoint
 * technique for a chip with no grid position to measure a delta from), so
 * they stay distinct variants rather than one shape with optional fields.
 */
interface TaskBlockDragPayload {
  source: "timeBlock";
  task: Task;
  kind: EventDragKind;
  columnWidth: number;
}

interface TaskChipDragPayload {
  source: "allDayChip";
  task: Task;
}

type TaskGridDragPayload = TaskBlockDragPayload | TaskChipDragPayload;

function isLastDay(day: Date, daysToShow: Date[]): boolean {
  return isSameDay(day, daysToShow[daysToShow.length - 1]);
}

function getAllDayDateAtPoint(
  days: Date[],
  clientX: number,
  clientY: number,
): Date | null {
  const target = document.elementFromPoint(clientX, clientY);
  const columnElement = target?.closest<HTMLElement>("[data-allday-date]");
  const dateKey = columnElement?.dataset.alldayDate;
  if (!dateKey) return null;

  return days.find((day) => day.toDateString() === dateKey) ?? null;
}

function computeDragTimes(
  payload: EventDragPayload,
  deltaX: number,
  deltaY: number,
) {
  const minuteOffset = (deltaY / PIXELS_PER_HOUR) * 60;

  if (payload.kind === "move") {
    const dayOffset =
      payload.columnWidth > 0 ? Math.round(deltaX / payload.columnWidth) : 0;
    return computeMovedEventTimes(
      payload.occurrence.start,
      payload.occurrence.end,
      dayOffset,
      minuteOffset,
    );
  }

  const edge = payload.kind === "resize-start" ? "start" : "end";
  return computeResizedEventTimes(
    payload.occurrence.start,
    payload.occurrence.end,
    edge,
    minuteOffset,
  );
}

/**
 * `computeDragTimes`'s counterpart for a `timeBlock`-sourced Task drag
 * (#314) — same move/resize branch, just reading the block's current
 * `[start, end)` off the Task (`taskTimeBlockEnd`) instead of an
 * Occurrence's own. Casts `task.start` away from `Date | null` here rather
 * than at each of this function's own callers: every one only ever reaches
 * it with a Task that has a Time block (a `TaskBlockDragPayload` only exists
 * for one), so the cast belongs at the one place that math actually runs.
 */
function computeTaskDragTimes(
  payload: TaskBlockDragPayload,
  deltaX: number,
  deltaY: number,
): DraftBlock {
  const minuteOffset = (deltaY / PIXELS_PER_HOUR) * 60;
  const originalStart = payload.task.start as Date;
  const originalEnd = taskTimeBlockEnd(payload.task);

  if (payload.kind === "move") {
    const dayOffset =
      payload.columnWidth > 0 ? Math.round(deltaX / payload.columnWidth) : 0;
    return computeMovedEventTimes(originalStart, originalEnd, dayOffset, minuteOffset);
  }

  const edge = payload.kind === "resize-start" ? "start" : "end";
  return computeResizedEventTimes(originalStart, originalEnd, edge, minuteOffset);
}

interface EventDragPreview {
  day: Date;
  data: EventDragPreviewData;
}

function computeEventDragPreview(
  activeDrag: EventDragPayload,
  dragDelta: Point,
  visibleOccurrences: Occurrence[],
  calendars: Calendar[],
  daysToShow: Date[],
): EventDragPreview {
  const { start, end } = computeDragTimes(activeDrag, dragDelta.x, dragDelta.y);
  const originDay = startOfDay(activeDrag.occurrence.start);
  const day = activeDrag.kind === "move" ? startOfDay(start) : originDay;
  const isLastColumn = isLastDay(day, daysToShow);
  const showReadout = isDragGesture(dragDelta, CLICK_DISTANCE_THRESHOLD_PX);

  const originDayOccurrences = visibleOccurrences.filter((occurrence) =>
    isSameDay(occurrence.start, originDay),
  );
  const activeKey = occurrenceKey(activeDrag.occurrence);
  const originLayout = layoutOverlappingEvents(originDayOccurrences).find(
    (layout) => occurrenceKey(layout.occurrence) === activeKey,
  );
  const { left, width } = originLayout
    ? columnLayoutToBox(originLayout.column, originLayout.columnCount)
    : columnLayoutToBox(0, 1);

  const calendar = getCalendarById(
    calendars,
    activeDrag.occurrence.event.calendarId,
  );
  const blockStyle = getOccurrenceBlockStyle(activeDrag.occurrence.event, calendar);

  return {
    day,
    data: {
      top: timeToY(start, PIXELS_PER_HOUR),
      height: durationToHeight(start, end, PIXELS_PER_HOUR),
      left,
      width,
      title: activeDrag.occurrence.event.title,
      start,
      end,
      blockStyle,
      columnWidth: activeDrag.columnWidth,
      isLastColumn,
      showReadout,
    },
  };
}

/**
 * `computeEventDragPreview`'s counterpart for a Task's Time block being
 * rescheduled or resized on the grid (#314) — same math
 * (`computeMovedEventTimes`/`computeResizedEventTimes`), same overlap-layout
 * lookup for the ghost's column, just keyed on the Task rather than the
 * Occurrence. Only meaningful for a `timeBlock`-sourced drag: an
 * `allDayChip` drag has no existing grid position to preview a move from,
 * and gets `TaskDragPreview`'s floating label instead, same as a panel row.
 */
function computeTaskDragPreview(
  activeDrag: TaskBlockDragPayload,
  dragDelta: Point,
  gridTasks: Task[],
  taskLists: TaskList[],
  daysToShow: Date[],
): EventDragPreview {
  const { start, end } = computeTaskDragTimes(activeDrag, dragDelta.x, dragDelta.y);
  const originDay = startOfDay(activeDrag.task.start as Date);
  const day = activeDrag.kind === "move" ? startOfDay(start) : originDay;
  const isLastColumn = isLastDay(day, daysToShow);
  const showReadout = isDragGesture(dragDelta, CLICK_DISTANCE_THRESHOLD_PX);

  const originDaySegments = getTaskTimeBlockDaySegments(gridTasks, originDay);
  const originLayout = layoutOverlappingEvents(originDaySegments).find(
    (layout) => layout.occurrence.task.id === activeDrag.task.id,
  );
  const { left, width } = originLayout
    ? columnLayoutToBox(originLayout.column, originLayout.columnCount)
    : columnLayoutToBox(0, 1);

  const taskList = taskLists.find((list) => list.id === activeDrag.task.taskListId);
  const blockStyle = getCalendarBlockStyle(taskList);

  return {
    day,
    data: {
      top: timeToY(start, PIXELS_PER_HOUR),
      height: durationToHeight(start, end, PIXELS_PER_HOUR),
      left,
      width,
      title: activeDrag.task.title,
      start,
      end,
      blockStyle,
      columnWidth: activeDrag.columnWidth,
      isLastColumn,
      showReadout,
    },
  };
}

export function TimeGrid({
  daysToShow,
  onDraftCreated,
  onOccurrenceClick,
}: TimeGridProps) {
  const calendars = useCalendarsStore((state) => state.calendars);
  const taskLists = useTaskListsStore((state) => state.taskLists);
  const tasks = useTasksStore((state) => state.tasks);
  const completedTasks = useTasksStore((state) => state.completedTasks);
  const setTaskTimeBlock = useTasksStore((state) => state.setTaskTimeBlock);
  const clearTaskTimeBlock = useTasksStore((state) => state.clearTaskTimeBlock);
  const setTaskDeadlineAndClearTimeBlock = useTasksStore(
    (state) => state.setTaskDeadlineAndClearTimeBlock,
  );
  const showTasksOnCalendar = useShellStore((state) => state.showTasksOnCalendar);
  const showCompletedTasks = useShellStore((state) => state.showCompletedTasks);
  const dragCommit = useOccurrenceDragCommit();
  const scrollRef = useRef<HTMLDivElement>(null);
  const headerRef = useRef<HTMLDivElement>(null);
  const daysContainerRef = useRef<HTMLDivElement>(null);
  const [now, setNow] = useState(() => new Date());

  const [allDayHoverDateKey, setAllDayHoverDateKey] = useState<string | null>(null);

  // The grid renders Occurrences, not raw Events: each master is expanded over
  // the visible window (first day 00:00 to the day after the last). A
  // non-recurring Event yields exactly one Occurrence, so nothing changes for it.
  const windowStart = startOfDay(daysToShow[0]);
  const windowEnd = startOfDay(addDays(daysToShow[daysToShow.length - 1], 1));
  const allOccurrences = useVisibleOccurrences(
    windowStart.getTime(),
    windowEnd.getTime(),
  );
  // All-day Occurrences render in the pinned all-day lane, not the hourly
  // grid (ADR-0017).
  const allDayOccurrences = allOccurrences.filter((occurrence) => occurrence.event.allDay);
  const visibleOccurrences = allOccurrences.filter((occurrence) => !occurrence.event.allDay);

  // Deadline-only Tasks share the all-day lane with all-day Occurrences
  // (#312, ADR-0083): "Show tasks on calendar" gates the feature outright,
  // and "Show completed" (mirroring TasksPanel's own visibleTasks) decides
  // whether a completed Task's chip is among them — completing one never
  // removes it, only that switch does.
  const zone = viewerZone();
  const candidateTasks = showTasksOnCalendar
    ? [...tasks, ...(showCompletedTasks ? completedTasks : [])]
    : [];
  const deadlineTasks = candidateTasks.filter((task) => {
    if (taskPlacement(task) !== "allDay" || !task.due) return false;
    const due = task.due;
    return daysToShow.some((day) => taskDeadlineFallsOnDay(due, day, zone));
  });

  // Time-blocked Tasks render on the hourly grid — placement precedence's
  // `grid` surface (#313, ADR-0083). Unlike `deadlineTasks`, this is never
  // gated by "Show completed": the AC is that a completed block "stays in
  // place ... rather than being removed from under the cursor", full stop —
  // completing one must never make it vanish, which excluding it whenever
  // "Show completed" happens to be off (the default) would do. "Show tasks
  // on calendar" still gates the whole feature outright. DayColumn itself
  // narrows this list to whichever day's block actually touches it, the same
  // division it already makes for the full `visibleOccurrences` list handed
  // to every column.
  const gridTasks = showTasksOnCalendar
    ? [...tasks, ...completedTasks].filter((task) => taskPlacement(task) === "grid")
    : [];

  useEffect(() => {
    const interval = setInterval(
      () => setNow(new Date()),
      NOW_REFRESH_INTERVAL_MS,
    );
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    const container = scrollRef.current;
    if (!container) return;
    const headerHeight = headerRef.current?.offsetHeight ?? 0;
    const nowY = headerHeight + timeToY(new Date(), PIXELS_PER_HOUR);
    container.scrollTop = Math.max(0, nowY - container.clientHeight / 2);
  }, []);

  const eventDrag = usePointerDrag<EventDragPayload>({
    onClick: (payload) => {
      if (payload.kind === "move") onOccurrenceClick(payload.occurrence);
    },
    onDrag: (payload, state: DragState) => {
      const { start, end } = computeDragTimes(payload, state.delta.x, state.delta.y);
      dragCommit.commit(payload.occurrence, start, end);
    },
  });

  function handleOccurrenceDragStart(
    occurrence: Occurrence,
    kind: EventDragKind,
    clientX: number,
    clientY: number,
  ) {
    const columnWidth =
      (daysContainerRef.current?.getBoundingClientRect().width ?? 0) /
      daysToShow.length;
    eventDrag.start({ occurrence, kind, columnWidth }, clientX, clientY);
  }

  const dragPreview = eventDrag.active
    ? computeEventDragPreview(
        eventDrag.active,
        eventDrag.delta,
        visibleOccurrences,
        calendars,
        daysToShow,
      )
    : null;

  const allDayDrag = usePointerDrag<AllDayDragPayload>({
    onClick: (payload) => {
      setAllDayHoverDateKey(null);
      onOccurrenceClick(payload.occurrence);
    },
    onDrag: (payload, state: DragState) => {
      setAllDayHoverDateKey(null);
      const targetDate = getAllDayDateAtPoint(daysToShow, state.position.x, state.position.y);
      if (targetDate) {
        const adjustedTarget = resolveDragTargetDate(
          payload.occurrence.start,
          payload.dragStartDay,
          targetDate,
        );
        const { start, end } = computeMoveToDate(
          payload.occurrence.start,
          payload.occurrence.end,
          adjustedTarget,
        );
        dragCommit.commit(payload.occurrence, start, end);
      }
    },
    onMove: (_payload, state: DragState) => {
      const hoveredDay = getAllDayDateAtPoint(daysToShow, state.position.x, state.position.y);
      setAllDayHoverDateKey(hoveredDay ? hoveredDay.toDateString() : null);
    },
  });

  function handleAllDayDragStart(
    occurrence: Occurrence,
    dragStartDay: Date,
    clientX: number,
    clientY: number,
  ) {
    allDayDrag.start({ occurrence, dragStartDay }, clientX, clientY);
  }

  const allDayDragCalendar = allDayDrag.active
    ? getCalendarById(calendars, allDayDrag.active.occurrence.event.calendarId)
    : undefined;
  const allDayDragBlockStyle = getOccurrenceBlockStyle(
    allDayDrag.active?.occurrence.event,
    allDayDragCalendar,
  );

  // taskDrag covers #314's remaining drop-table rows for a Task already on
  // the grid or in the all-day lane: rescheduling a Time block, dragging a
  // Deadline-only chip onto the hourly grid, and (outside the drop table
  // proper) resizing a Time block. Every *drop-table* write goes through
  // taskDropIntent — this handler only resolves *where* the pointer landed
  // (by delta math for an in-grid move, by the same elementFromPoint
  // technique TaskRow's panel-drop and the Event all-day drag both already
  // use for every other surface), never what to write. Resizing is the one
  // branch that bypasses it: it isn't a drop-table row (a resize's target is
  // always the same surface it started on), so it writes directly, mirroring
  // EventBlock's own resize.
  const taskDrag = usePointerDrag<TaskGridDragPayload>({
    onClick: () => {},
    onDrag: (payload, state: DragState) => {
      if (payload.source === "allDayChip") {
        const dropTime = resolveGridDropTime(state.position.x, state.position.y);
        if (!dropTime) return;
        const write = taskDropIntent({ kind: "allDayChip" }, { surface: "hourlyGrid", time: dropTime });
        if (write.action === "setTimeBlock") {
          void setTaskTimeBlock(payload.task.id, write.start, write.durationMinutes);
        }
        return;
      }

      // Resizing never checks the drop point — exactly like EventBlock's own
      // resize, it's pure delta math against the edge that moved.
      if (payload.kind !== "move") {
        const { start, end } = computeTaskDragTimes(payload, state.delta.x, state.delta.y);
        void setTaskTimeBlock(payload.task.id, start, (end.getTime() - start.getTime()) / 60_000);
        return;
      }

      const allDayDate = getAllDayDateAtPoint(daysToShow, state.position.x, state.position.y);
      if (allDayDate) {
        const write = taskDropIntent(
          { kind: "timeBlock", durationMinutes: payload.task.durationMinutes as number },
          { surface: "allDayLane", date: allDayDate },
        );
        if (write.action === "setDeadlineAndClearTimeBlock") {
          void setTaskDeadlineAndClearTimeBlock(payload.task.id, write.due);
        }
        return;
      }

      if (isPointOverTasksPanel(state.position.x, state.position.y)) {
        const write = taskDropIntent(
          { kind: "timeBlock", durationMinutes: payload.task.durationMinutes as number },
          { surface: "panel" },
        );
        if (write.action === "clearTimeBlock") void clearTaskTimeBlock(payload.task.id);
        return;
      }

      // Still on the hourly grid: an ordinary reschedule, resolved the same
      // way an Event's own move is (delta math, not elementFromPoint), then
      // routed through taskDropIntent so the write itself stays the pure
      // module's call, not this handler's.
      const movedStart = computeTaskDragTimes(payload, state.delta.x, state.delta.y).start;
      const write = taskDropIntent(
        { kind: "timeBlock", durationMinutes: payload.task.durationMinutes as number },
        { surface: "hourlyGrid", time: movedStart },
      );
      if (write.action === "setTimeBlock") {
        void setTaskTimeBlock(payload.task.id, write.start, write.durationMinutes);
      }
    },
  });

  function handleTaskDragStart(
    task: Task,
    kind: EventDragKind,
    clientX: number,
    clientY: number,
  ) {
    const columnWidth =
      (daysContainerRef.current?.getBoundingClientRect().width ?? 0) /
      daysToShow.length;
    taskDrag.start({ source: "timeBlock", task, kind, columnWidth }, clientX, clientY);
  }

  function handleTaskChipDragStart(task: Task, clientX: number, clientY: number) {
    taskDrag.start({ source: "allDayChip", task }, clientX, clientY);
  }

  // Safe to share between DayColumn (hides the source block of a "timeBlock"
  // drag) and AllDayLane (hides the source chip of an "allDayChip" drag):
  // whichever source is active, its Task only ever appears among one
  // surface's own list (gridTasks or deadlineTasks), so the id never
  // wrongly matches the other surface's item.
  const draggingTaskId = taskDrag.active ? taskDrag.active.task.id : null;

  const taskDragPreview =
    taskDrag.active?.source === "timeBlock"
      ? computeTaskDragPreview(taskDrag.active, taskDrag.delta, gridTasks, taskLists, daysToShow)
      : null;

  const draggingChipTaskList =
    taskDrag.active?.source === "allDayChip"
      ? taskLists.find((list) => list.id === taskDrag.active?.task.taskListId)
      : undefined;

  return (
    <div className="flex h-full flex-col">
      <div ref={scrollRef} className="isolate flex-1 overflow-y-auto">
        <div
          ref={headerRef}
          className="sticky top-0 border-b border-border bg-surface"
          style={{ zIndex: GRID_Z_HEADER }}
        >
          <div className="flex">
            <div className="w-18 shrink-0" />
            {daysToShow.map((day) => (
              <div
                key={day.toISOString()}
                className="flex-1 border-l border-border py-2 text-center"
              >
                <p className="text-label-sm text-ink-muted">{format(day, "EEE")}</p>
                <p className="text-body text-ink">{format(day, "d")}</p>
              </div>
            ))}
          </div>
          <AllDayLane
            daysToShow={daysToShow}
            occurrences={allDayOccurrences}
            onOccurrenceClick={onOccurrenceClick}
            onOccurrenceDragStart={handleAllDayDragStart}
            draggingKey={
              allDayDrag.active ? occurrenceKey(allDayDrag.active.occurrence) : null
            }
            dragHoverDateKey={allDayHoverDateKey}
            deadlineTasks={deadlineTasks}
            onTaskDragStart={handleTaskChipDragStart}
            draggingTaskId={draggingTaskId}
          />
        </div>
        <div className="flex">
          <TimeAxis pixelsPerHour={PIXELS_PER_HOUR} />
          <div ref={daysContainerRef} className="flex flex-1">
            {daysToShow.map((day) => (
              <DayColumn
                key={day.toISOString()}
                day={day}
                occurrences={visibleOccurrences}
                tasks={gridTasks}
                pixelsPerHour={PIXELS_PER_HOUR}
                now={now}
                onDraftCreated={onDraftCreated}
                onOccurrenceClick={onOccurrenceClick}
                onOccurrenceDragStart={handleOccurrenceDragStart}
                draggingKey={
                  eventDrag.active ? occurrenceKey(eventDrag.active.occurrence) : null
                }
                eventDragPreview={
                  dragPreview && isSameDay(dragPreview.day, day)
                    ? dragPreview.data
                    : null
                }
                isLastColumn={isLastDay(day, daysToShow)}
                onTaskDragStart={handleTaskDragStart}
                draggingTaskId={draggingTaskId}
                taskDragPreview={
                  taskDragPreview && isSameDay(taskDragPreview.day, day)
                    ? taskDragPreview.data
                    : null
                }
              />
            ))}
          </div>
        </div>
      </div>
      {allDayDrag.active && (
        <AllDayEventDragPreview
          x={allDayDrag.position.x}
          y={allDayDrag.position.y}
          title={allDayDrag.active.occurrence.event.title}
          blockStyle={allDayDragBlockStyle}
        />
      )}
      {taskDrag.active?.source === "allDayChip" && (
        <TaskDragPreview
          x={taskDrag.position.x}
          y={taskDrag.position.y}
          title={taskDrag.active.task.title}
          blockStyle={getCalendarBlockStyle(draggingChipTaskList)}
        />
      )}
      {dragCommit.isScopePickerOpen && (
        <ScopePicker
          action="Edit"
          onConfirm={dragCommit.confirmScope}
          onClose={dragCommit.cancel}
        />
      )}
    </div>
  );
}
