import { format } from "date-fns";
import type { Task } from "../../lib/tasksApi";
import { useTasksStore } from "../../lib/tasksStore";
import { TaskCompletionControl } from "./TaskCompletionControl";

interface TaskRowProps {
  task: Task;
  onOpenDetail: (task: Task) => void;
}

// TaskRow is one row inside a Task bucket (#310, #311, ADR-0083): its own
// Deadline beside the title, so Thursday's work reads differently from
// Friday's inside the same bucket, and a click anywhere but the completion
// control opens the detail surface.
export function TaskRow({ task, onOpenDetail }: TaskRowProps) {
  const setTaskCompleted = useTasksStore((state) => state.setTaskCompleted);

  return (
    <div
      onClick={() => onOpenDetail(task)}
      className="flex cursor-pointer items-center gap-2 rounded-shell-sm py-1.5 hover:bg-surface-hover"
    >
      <span
        // Stops the completion click from also opening the detail surface —
        // the two are one row's two separate gestures.
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
      <span
        className={`min-w-0 flex-1 truncate text-body ${
          task.completed ? "text-ink-muted line-through" : "text-ink"
        }`}
      >
        {task.title}
      </span>
      {task.due && (
        <span className="shrink-0 text-label-sm text-ink-muted">{format(task.due, "MMM d")}</span>
      )}
    </div>
  );
}
