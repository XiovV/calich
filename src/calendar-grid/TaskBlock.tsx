import { getCalendarBlockStyle } from "../lib/calendarColors";
import { columnLayoutToBox } from "../lib/eventBlockGeometry";
import { durationToHeight, timeToY } from "../lib/gridTime";
import { taskTimeBlockEnd } from "../lib/taskTimeBlockSegments";
import type { TaskList } from "../lib/taskListsApi";
import type { Task } from "../lib/tasksApi";
import { useTasksStore } from "../lib/tasksStore";
import { TaskCompletionControl } from "../components/layout/TaskCompletionControl";
import type { EventDragKind } from "./EventBlock";
import { GRID_Z_OCCURRENCE_EDGE } from "./gridStacking";

interface TaskBlockProps {
  task: Task;
  taskList: TaskList | undefined;
  /** The block's span clipped to the day this instance renders in — equal to
   * the block's own bounds unless it crosses midnight (mirrors EventBlock's
   * segmentStart/segmentEnd, issue #230). */
  segmentStart: Date;
  segmentEnd: Date;
  column: number;
  columnCount: number;
  pixelsPerHour: number;
  onDragStart: (task: Task, kind: EventDragKind, clientX: number, clientY: number) => void;
}

/**
 * A Task's Time block on the hourly grid — placement precedence's `grid`
 * surface (#313, ADR-0083). Joins Events in the same overlap-layout pass
 * (ADR-0004) rather than drawing over them, drawn in its Task List's colour,
 * and carries the same completion control the panel row and the all-day
 * chip (`TaskDeadlineChip`) do — completing here is the same
 * `setTaskCompleted` write, and the block stays in place rendered completed
 * rather than being removed from under the cursor. Reschedules and resizes
 * by the same gesture `EventBlock` uses — a plain `<div>` rather than
 * `EventBlock`'s `<button>`, because this block already nests one
 * interactive control (the completion toggle) and a button-in-button is
 * invalid HTML (#314). Tasks carry no Access/read-only concept (ADR-0083),
 * so unlike `EventBlock` there is no read-only branch here at all.
 */
export function TaskBlock({
  task,
  taskList,
  segmentStart,
  segmentEnd,
  column,
  columnCount,
  pixelsPerHour,
  onDragStart,
}: TaskBlockProps) {
  const setTaskCompleted = useTasksStore((state) => state.setTaskCompleted);
  const blockStyle = getCalendarBlockStyle(taskList);
  const top = timeToY(segmentStart, pixelsPerHour);
  const height = durationToHeight(segmentStart, segmentEnd, pixelsPerHour);
  const { left, width } = columnLayoutToBox(column, columnCount);

  // A Task's block is never expected to cross midnight in practice, but the
  // guard costs nothing and keeps this exactly in step with EventBlock's own
  // rule: only the segment that actually carries the block's true start (or
  // end) gets that edge's resize handle.
  const showResizeStart = segmentStart.getTime() === (task.start as Date).getTime();
  const showResizeEnd = segmentEnd.getTime() === taskTimeBlockEnd(task).getTime();

  function handleEdgeMouseDown(domEvent: React.MouseEvent, kind: "resize-start" | "resize-end") {
    domEvent.stopPropagation();
    onDragStart(task, kind, domEvent.clientX, domEvent.clientY);
  }

  return (
    <div
      onMouseDown={(domEvent) => {
        domEvent.stopPropagation();
        onDragStart(task, "move", domEvent.clientX, domEvent.clientY);
      }}
      style={{
        top: `${top}px`,
        height: `${height}px`,
        left: `${left}%`,
        width: `calc(${width}% - 2px)`,
        ...blockStyle,
      }}
      className="absolute flex cursor-pointer items-center gap-1 overflow-hidden rounded-shell-sm px-1 text-label-sm"
    >
      {showResizeStart && (
        <div
          onMouseDown={(domEvent) => handleEdgeMouseDown(domEvent, "resize-start")}
          className="absolute inset-x-0 top-0 h-1.5 cursor-ns-resize"
          style={{ zIndex: GRID_Z_OCCURRENCE_EDGE }}
        />
      )}
      <span
        // Stops the completion control's own mousedown/click from bubbling
        // into the block's own drag-to-move gesture — the same guard
        // EventBlock's resize handles use to keep two gestures on one block
        // from colliding.
        onMouseDown={(event) => event.stopPropagation()}
        onClick={(event) => event.stopPropagation()}
      >
        <TaskCompletionControl
          checked={task.completed}
          onCheckedChange={(checked) => {
            void setTaskCompleted(task.id, checked);
          }}
          aria-label={task.completed ? `Mark "${task.title}" incomplete` : `Mark "${task.title}" complete`}
        />
      </span>
      <span className={`truncate ${task.completed ? "line-through opacity-70" : ""}`}>
        {task.title}
      </span>
      {showResizeEnd && (
        <div
          onMouseDown={(domEvent) => handleEdgeMouseDown(domEvent, "resize-end")}
          className="absolute inset-x-0 bottom-0 h-1.5 cursor-ns-resize"
          style={{ zIndex: GRID_Z_OCCURRENCE_EDGE }}
        />
      )}
    </div>
  );
}
