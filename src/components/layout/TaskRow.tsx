import type { Task } from "../../lib/tasksApi";
import { useTasksStore } from "../../lib/tasksStore";
import { TaskCompletionControl } from "./TaskCompletionControl";

interface TaskRowProps {
  task: Task;
}

// TaskRow is one row in the Tasks panel's flat list (#310, ADR-0083). No
// Deadlines yet, so every Task sits under "No date" with nothing to group
// by — this ticket's demoable end state is capture, tick off, and show/hide
// completed, not the bucketed/grouped panel ADR-0083 describes eventually.
export function TaskRow({ task }: TaskRowProps) {
  const setTaskCompleted = useTasksStore((state) => state.setTaskCompleted);

  return (
    <div className="flex items-center gap-2 py-1.5">
      <TaskCompletionControl
        checked={task.completed}
        onCheckedChange={(checked) => {
          void setTaskCompleted(task.id, checked);
        }}
        aria-label={task.completed ? `Mark "${task.title}" incomplete` : `Mark "${task.title}" complete`}
      />
      <span
        className={`min-w-0 flex-1 truncate text-body ${
          task.completed ? "text-ink-muted line-through" : "text-ink"
        }`}
      >
        {task.title}
      </span>
    </div>
  );
}
