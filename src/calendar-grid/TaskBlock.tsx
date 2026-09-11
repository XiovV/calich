import { getCalendarBlockStyle } from "../lib/calendarColors";
import { columnLayoutToBox } from "../lib/eventBlockGeometry";
import { durationToHeight, timeToY } from "../lib/gridTime";
import type { TaskList } from "../lib/taskListsApi";
import type { Task } from "../lib/tasksApi";
import { useTasksStore } from "../lib/tasksStore";
import { TaskCompletionControl } from "../components/layout/TaskCompletionControl";

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
}

/**
 * A Task's Time block on the hourly grid — placement precedence's `grid`
 * surface (#313, ADR-0083). Joins Events in the same overlap-layout pass
 * (ADR-0004) rather than drawing over them, drawn in its Task List's colour,
 * and carries the same completion control the panel row and the all-day
 * chip (`TaskDeadlineChip`) do — completing here is the same
 * `setTaskCompleted` write, and the block stays in place rendered completed
 * rather than being removed from under the cursor. No drag or resize yet:
 * #313 only wires the panel-drop gesture that creates a block; rescheduling
 * and resizing one are later tickets'.
 */
export function TaskBlock({
  task,
  taskList,
  segmentStart,
  segmentEnd,
  column,
  columnCount,
  pixelsPerHour,
}: TaskBlockProps) {
  const setTaskCompleted = useTasksStore((state) => state.setTaskCompleted);
  const blockStyle = getCalendarBlockStyle(taskList);
  const top = timeToY(segmentStart, pixelsPerHour);
  const height = durationToHeight(segmentStart, segmentEnd, pixelsPerHour);
  const { left, width } = columnLayoutToBox(column, columnCount);

  return (
    <div
      style={{
        top: `${top}px`,
        height: `${height}px`,
        left: `${left}%`,
        width: `calc(${width}% - 2px)`,
        ...blockStyle,
      }}
      className="absolute flex items-center gap-1 overflow-hidden rounded-shell-sm px-1 text-label-sm"
    >
      <span
        // Stops the completion control's own mousedown/click from bubbling
        // into a column-level gesture — the same guard EventBlock's resize
        // handles use to keep two gestures on one block from colliding.
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
    </div>
  );
}
