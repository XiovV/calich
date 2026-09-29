import { getCalendarBlockStyle } from "../lib/calendarColors";
import type { TaskList } from "../lib/taskListsApi";
import type { Task } from "../lib/tasksApi";
import { useTasksStore } from "../lib/tasksStore";
import { TaskCompletionControl } from "../components/layout/TaskCompletionControl";

interface TaskDeadlineChipProps {
  task: Task;
  taskList: TaskList | undefined;
  onDragStart: (task: Task, clientX: number, clientY: number) => void;
  /** Hidden rather than removed while its own drag is active — the same
   * `invisible`-not-unmounted treatment AllDayLane already gives a dragging
   * Occurrence chip, so this day's column doesn't shrink out from under the
   * drop target mid-gesture. */
  isDragging: boolean;
}

/**
 * A Deadline-only Task's chip in the all-day lane (#312, ADR-0083) — the
 * placement precedence's `allDay` surface. Drawn in its Task List's colour
 * (`getCalendarBlockStyle` needs only `.color`, so it works unchanged for a
 * Task List the same as it does for a Calendar), and carrying the same
 * circular completion control the panel's own `TaskRow` does — completing
 * from here is the same `setTaskCompleted` write, not a parallel one. Kept
 * in place rather than removed once completed: "Show completed" (passed by
 * the caller, which decides whether this chip is rendered at all) is what
 * hides it, not completion itself. Draggable onto the hourly grid (#314,
 * the drop table's all-day-chip row) — dragging within the all-day lane
 * itself is not a gesture this ticket builds.
 */
export function TaskDeadlineChip({ task, taskList, onDragStart, isDragging }: TaskDeadlineChipProps) {
  const setTaskCompleted = useTasksStore((state) => state.setTaskCompleted);
  const blockStyle = getCalendarBlockStyle(taskList);

  return (
    <div
      onMouseDown={(domEvent) => {
        domEvent.stopPropagation();
        onDragStart(task, domEvent.clientX, domEvent.clientY);
      }}
      style={blockStyle}
      className={`flex w-full cursor-pointer items-center gap-1 rounded-shell-sm px-1 text-left text-label-sm ${isDragging ? "invisible" : ""}`}
    >
      <span
        // Stops a click on the control from bubbling into the chip's own
        // drag gesture — the same guard TaskRow uses for its identical
        // two-gestures-in-one-row shape.
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
