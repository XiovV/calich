import { format } from "date-fns";
import { getCalendarBlockStyle } from "../lib/calendarColors";
import type { TaskList } from "../lib/taskListsApi";
import type { Task } from "../lib/tasksApi";
import { useTasksStore } from "../lib/tasksStore";
import { TaskCompletionControl } from "../components/layout/TaskCompletionControl";

interface MonthTaskChipProps {
  task: Task;
  taskList: TaskList | undefined;
  timePattern: string;
  onDragStart: (task: Task, clientX: number, clientY: number) => void;
}

/**
 * A Task's chip in a Month Day cell (#315, ADR-0083) — placement precedence
 * collapsed to whichever single date the Task renders on there: the Time
 * block's if it has one, else the Deadline's. Drawn in its Task List's
 * colour (`getCalendarBlockStyle` needs only `.color`) and carrying the same
 * circular completion control every other Task surface does — completing
 * from here is the same `setTaskCompleted` write, not a parallel one. Shows
 * its time only when it has a Time block, mirroring `MonthDayCell`'s own
 * timed-vs-all-day Event chip convention, since a Deadline carries no
 * meaningful time of day the way a Time block does.
 */
export function MonthTaskChip({ task, taskList, timePattern, onDragStart }: MonthTaskChipProps) {
  const setTaskCompleted = useTasksStore((state) => state.setTaskCompleted);
  const blockStyle = getCalendarBlockStyle(taskList);

  return (
    <div
      onMouseDown={(domEvent) => {
        domEvent.stopPropagation();
        onDragStart(task, domEvent.clientX, domEvent.clientY);
      }}
      style={blockStyle}
      className="flex w-full cursor-pointer items-center gap-1 rounded-shell-sm px-1 text-left text-label-sm"
    >
      <span
        // Stops a click on the control from bubbling into the chip's own
        // drag gesture — the same guard every other Task surface's row uses
        // for its identical two-gestures-in-one-chip shape.
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
        {task.start ? `${format(task.start, timePattern)} ${task.title}` : task.title}
      </span>
    </div>
  );
}
