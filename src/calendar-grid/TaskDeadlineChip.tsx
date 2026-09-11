import { getCalendarBlockStyle } from "../lib/calendarColors";
import type { TaskList } from "../lib/taskListsApi";
import type { Task } from "../lib/tasksApi";
import { useTasksStore } from "../lib/tasksStore";
import { TaskCompletionControl } from "../components/layout/TaskCompletionControl";

interface TaskDeadlineChipProps {
  task: Task;
  taskList: TaskList | undefined;
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
 * hides it, not completion itself.
 */
export function TaskDeadlineChip({ task, taskList }: TaskDeadlineChipProps) {
  const setTaskCompleted = useTasksStore((state) => state.setTaskCompleted);
  const blockStyle = getCalendarBlockStyle(taskList);

  return (
    <div
      style={blockStyle}
      className="flex w-full items-center gap-1 rounded-shell-sm px-1 text-left text-label-sm"
    >
      <span
        // Stops a click on the control from bubbling into anything the
        // chip's own row might one day gain — the same guard TaskRow uses
        // for its identical two-gestures-in-one-row shape.
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
