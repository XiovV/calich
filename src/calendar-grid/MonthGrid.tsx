import { useRef, useState } from "react";
import { addDays, format, isSameDay, startOfDay } from "date-fns";
import { useShellStore } from "../lib/shellStore";
import { useCalendarsStore } from "../lib/calendarsStore";
import { useTaskListsStore } from "../lib/taskListsStore";
import { getTaskListById } from "../lib/taskListsApi";
import { useTasksStore } from "../lib/tasksStore";
import { getCalendarById } from "../lib/calendar";
import { getCalendarBlockStyle, getOccurrenceBlockStyle } from "../lib/calendarColors";
import { buildMonthGrid, getOccurrencesForDay } from "../lib/monthGrid";
import { computeMoveToDate } from "../lib/gridTime";
import { viewerZone } from "../lib/floatingTime";
import { taskDropIntent } from "../lib/taskDropIntent";
import { taskFallsOnMonthDay, taskPlacement } from "../lib/taskScheduling";
import { taskTimeBlockEnd } from "../lib/taskTimeBlockSegments";
import type { Task } from "../lib/tasksApi";
import { useVisibleOccurrences } from "../hooks/useVisibleOccurrences";
import { useWeekStartsOn } from "../hooks/useWeekStartsOn";
import { usePointerDrag, type DragState } from "../hooks/usePointerDrag";
import { occurrenceKey, type Occurrence } from "../lib/occurrence";
import { MonthDayCell } from "./MonthDayCell";
import { MonthEventDragPreview } from "./MonthEventDragPreview";
import { ScopePicker } from "./ScopePicker";
import { TaskDragPreview } from "./TaskDragPreview";
import { useOccurrenceDragCommit } from "./useOccurrenceDragCommit";
import type { DraftBlock } from "../lib/gridTime";

interface MonthGridProps {
  onDraftCreated: (day: Date, draft: DraftBlock) => void;
  onOccurrenceClick: (occurrence: Occurrence) => void;
}

function getCellDateAtPoint(
  cells: { date: Date }[],
  clientX: number,
  clientY: number,
): Date | null {
  const target = document.elementFromPoint(clientX, clientY);
  const cellElement = target?.closest<HTMLElement>("[data-cell-date]");
  const dateKey = cellElement?.dataset.cellDate;
  if (!dateKey) return null;

  const cell = cells.find((c) => c.date.toDateString() === dateKey);
  return cell?.date ?? null;
}

export function MonthGrid({ onDraftCreated, onOccurrenceClick }: MonthGridProps) {
  const selectedDate = useShellStore((state) => state.selectedDate);
  const weekStartsOn = useWeekStartsOn();
  const calendars = useCalendarsStore((state) => state.calendars);
  const taskLists = useTaskListsStore((state) => state.taskLists);
  const tasks = useTasksStore((state) => state.tasks);
  const completedTasks = useTasksStore((state) => state.completedTasks);
  const setTaskTimeBlock = useTasksStore((state) => state.setTaskTimeBlock);
  const setTaskDeadlineAndClearTimeBlock = useTasksStore(
    (state) => state.setTaskDeadlineAndClearTimeBlock,
  );
  const showTasksOnCalendar = useShellStore((state) => state.showTasksOnCalendar);
  const showCompletedTasks = useShellStore((state) => state.showCompletedTasks);
  const dragCommit = useOccurrenceDragCommit();

  const [hoveredCellKey, setHoveredCellKey] = useState<string | null>(null);

  // A drag that ends back inside its origin cell still fires a native "click"
  // on the cell (mousedown and mouseup share a common ancestor there), which
  // would otherwise reopen the create modal right after a move. Suppress the
  // next cell click when a real drag (not just a click-to-edit) just finished.
  const suppressNextCellClickRef = useRef(false);

  function handleDraftCreated(day: Date, draft: DraftBlock) {
    if (suppressNextCellClickRef.current) return;
    onDraftCreated(day, draft);
  }

  const cells = buildMonthGrid(selectedDate, weekStartsOn);

  // The grid renders Occurrences, not raw Events: each master is expanded over
  // the six-week window the grid displays. A non-recurring Event yields exactly
  // one Occurrence, so nothing changes for it.
  const windowStart = startOfDay(cells[0].date);
  const windowEnd = startOfDay(addDays(cells[cells.length - 1].date, 1));
  const visibleOccurrences = useVisibleOccurrences(
    windowStart.getTime(),
    windowEnd.getTime(),
  );

  // Month task chips (#315, ADR-0083): the same "Show tasks on calendar" /
  // "Show completed" split TimeGrid already makes for its own gridTasks
  // (Time-blocked) and deadlineTasks (Deadline-only) lists — a completed
  // Time-blocked Task never disappears once "Show tasks on calendar" is on
  // (only that switch hides it), where a completed Deadline-only one
  // additionally needs "Show completed". Month draws both surfaces as one
  // chip, so the two lists are merged once here rather than kept apart the
  // way DayColumn/AllDayLane keep them.
  const gridPlacedTasks = showTasksOnCalendar
    ? [...tasks, ...completedTasks].filter((task) => taskPlacement(task) === "grid")
    : [];
  const deadlinePlacedTasks = showTasksOnCalendar
    ? [...tasks, ...(showCompletedTasks ? completedTasks : [])].filter(
        (task) => taskPlacement(task) === "allDay",
      )
    : [];
  const monthTasks = [...gridPlacedTasks, ...deadlinePlacedTasks];
  const zone = viewerZone();

  const now = new Date();

  const eventDrag = usePointerDrag<Occurrence>({
    onClick: (occurrence) => {
      setHoveredCellKey(null);
      onOccurrenceClick(occurrence);
    },
    onDrag: (occurrence, state: DragState) => {
      setHoveredCellKey(null);
      suppressNextCellClickRef.current = true;
      setTimeout(() => {
        suppressNextCellClickRef.current = false;
      }, 0);

      const targetDate = getCellDateAtPoint(cells, state.position.x, state.position.y);
      if (targetDate) {
        const { start, end } = computeMoveToDate(occurrence.start, occurrence.end, targetDate);
        dragCommit.commit(occurrence, start, end);
      }
    },
    onMove: (_occurrence, state: DragState) => {
      const hoveredDate = getCellDateAtPoint(cells, state.position.x, state.position.y);
      setHoveredCellKey(hoveredDate ? hoveredDate.toDateString() : null);
    },
  });

  function handleOccurrenceDragStart(
    occurrence: Occurrence,
    clientX: number,
    clientY: number,
  ) {
    eventDrag.start(occurrence, clientX, clientY);
  }

  const dragCalendar = eventDrag.active
    ? getCalendarById(calendars, eventDrag.active.event.calendarId)
    : undefined;
  const dragBlockStyle = getOccurrenceBlockStyle(eventDrag.active?.event, dragCalendar);
  const draggingKey = eventDrag.active ? occurrenceKey(eventDrag.active) : null;

  // taskDrag is Month's own drop-table row (#315, ADR-0083): a chip already
  // shown here — whether it's a Time block or a Deadline-only one, there's
  // no on-grid distinction between the two the way Day/Week's separate
  // hourly-grid/all-day-lane surfaces draw — can only move to another Day
  // cell, never resize (Month has no time axis to resize against). A
  // Time-blocked Task keeps its time-of-day, combined with the drop day the
  // same way a Month Event drag already does (`computeMoveToDate`); a
  // Deadline-only one just takes the drop day outright. Both route through
  // `taskDropIntent` so the write itself stays that pure module's call, not
  // this handler's — the source's `kind` mirrors which chip is actually on
  // screen: "timeBlock" for one with a Time block, "allDayChip" for one
  // that's Deadline-only (there is no Month-grown panel-row source; that
  // one only ever starts in `TaskRow`).
  const taskDrag = usePointerDrag<Task>({
    onClick: () => {},
    onDrag: (task, state: DragState) => {
      setHoveredCellKey(null);
      suppressNextCellClickRef.current = true;
      setTimeout(() => {
        suppressNextCellClickRef.current = false;
      }, 0);

      const targetDate = getCellDateAtPoint(cells, state.position.x, state.position.y);
      if (!targetDate) return;

      if (task.start) {
        const { start } = computeMoveToDate(task.start, taskTimeBlockEnd(task), targetDate);
        const write = taskDropIntent(
          { kind: "timeBlock", durationMinutes: task.durationMinutes as number },
          { surface: "monthDay", date: start },
        );
        if (write.action === "setTimeBlock") {
          void setTaskTimeBlock(task.id, write.start, write.durationMinutes);
        }
        return;
      }

      const write = taskDropIntent({ kind: "allDayChip" }, { surface: "monthDay", date: targetDate });
      if (write.action === "setDeadlineAndClearTimeBlock") {
        void setTaskDeadlineAndClearTimeBlock(task.id, write.due);
      }
    },
    onMove: (_task, state: DragState) => {
      const hoveredDate = getCellDateAtPoint(cells, state.position.x, state.position.y);
      setHoveredCellKey(hoveredDate ? hoveredDate.toDateString() : null);
    },
  });

  function handleTaskDragStart(task: Task, clientX: number, clientY: number) {
    taskDrag.start(task, clientX, clientY);
  }

  const draggingTaskId = taskDrag.active ? taskDrag.active.id : null;
  const draggingTaskList = taskDrag.active
    ? getTaskListById(taskLists, taskDrag.active.taskListId)
    : undefined;

  return (
    <div className="flex h-full flex-col">
      <div className="grid grid-cols-7 border-t border-border">
        {cells.slice(0, 7).map(({ date }) => (
          <div
            key={date.toISOString()}
            className="border-b border-l border-border py-2 text-center text-label-sm text-ink-muted"
          >
            {format(date, "EEE")}
          </div>
        ))}
      </div>
      <div className="grid flex-1 grid-cols-7 grid-rows-6">
        {cells.map(({ date, inCurrentMonth }) => (
          <MonthDayCell
            key={date.toISOString()}
            date={date}
            inCurrentMonth={inCurrentMonth}
            isToday={isSameDay(date, now)}
            isDragHover={hoveredCellKey === date.toDateString()}
            occurrences={getOccurrencesForDay(visibleOccurrences, date).filter(
              (occurrence) => occurrenceKey(occurrence) !== draggingKey,
            )}
            tasks={monthTasks.filter(
              (task) => task.id !== draggingTaskId && taskFallsOnMonthDay(task, date, zone),
            )}
            onDraftCreated={handleDraftCreated}
            onOccurrenceClick={onOccurrenceClick}
            onOccurrenceDragStart={handleOccurrenceDragStart}
            onTaskDragStart={handleTaskDragStart}
          />
        ))}
      </div>
      {eventDrag.active && (
        <MonthEventDragPreview
          x={eventDrag.position.x}
          y={eventDrag.position.y}
          title={eventDrag.active.event.title}
          start={eventDrag.active.start}
          blockStyle={dragBlockStyle}
        />
      )}
      {taskDrag.active && (
        <TaskDragPreview
          x={taskDrag.position.x}
          y={taskDrag.position.y}
          title={taskDrag.active.title}
          blockStyle={getCalendarBlockStyle(draggingTaskList)}
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
