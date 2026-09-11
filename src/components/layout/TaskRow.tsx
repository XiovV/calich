import { format } from "date-fns";
import { getCalendarBlockStyle } from "../../lib/calendarColors";
import { PIXELS_PER_HOUR, snapToIncrement, yToTime } from "../../lib/gridTime";
import { usePointerDrag } from "../../hooks/usePointerDrag";
import { useTaskListsStore } from "../../lib/taskListsStore";
import { taskDropIntent } from "../../lib/taskDropIntent";
import type { Task } from "../../lib/tasksApi";
import { useTasksStore } from "../../lib/tasksStore";
import { TaskCompletionControl } from "./TaskCompletionControl";
import { TaskDragPreview } from "../../calendar-grid/TaskDragPreview";

interface TaskRowProps {
  task: Task;
  onOpenDetail: (task: Task) => void;
}

/**
 * Resolves a panel row's drop point to a grid day and time, if it landed on
 * one — the same data-attribute-plus-`elementFromPoint` technique
 * AllDayLane/MonthGrid already use for their own in-grid drags
 * (`getAllDayDateAtPoint`, `getCellDateAtPoint`), here reaching from the
 * Tasks panel's own DOM subtree into DayColumn's (#313). `null` when the
 * drop landed anywhere else (the all-day lane, Month, the panel itself) —
 * none of those targets are this ticket's (#314 and later widen this).
 */
function resolveGridDropTime(clientX: number, clientY: number): Date | null {
  const target = document.elementFromPoint(clientX, clientY);
  const columnElement = target?.closest<HTMLElement>("[data-grid-day-ms]");
  const dayMsKey = columnElement?.dataset.gridDayMs;
  if (!columnElement || !dayMsKey) return null;

  const day = new Date(Number(dayMsKey));
  const offsetY = clientY - columnElement.getBoundingClientRect().top;
  return snapToIncrement(yToTime(offsetY, day, PIXELS_PER_HOUR), 15);
}

// TaskRow is one row inside a Task bucket (#310, #311, #313, ADR-0083): its
// own Deadline beside the title, so Thursday's work reads differently from
// Friday's inside the same bucket. A plain click opens the detail surface; a
// drag onto the hourly grid gives it a Time block instead (issue #309 story
// 47), reusing `usePointerDrag` — the same gesture-pure mousedown-origin /
// window-mousemove-mouseup hook every draggable Occurrence uses. It works
// unmodified here even though the Task drag starts in a different DOM
// subtree than the grid it can land on: `usePointerDrag` never touches the
// DOM itself, and `TimeGrid`'s own all-day drag already resolves its drop
// target the same elementFromPoint way, from inside its `onDrag`.
export function TaskRow({ task, onOpenDetail }: TaskRowProps) {
  const setTaskCompleted = useTasksStore((state) => state.setTaskCompleted);
  const setTaskTimeBlock = useTasksStore((state) => state.setTaskTimeBlock);
  const taskList = useTaskListsStore((state) =>
    state.taskLists.find((list) => list.id === task.taskListId),
  );

  const drag = usePointerDrag<Task>({
    onClick: onOpenDetail,
    onDrag: (draggedTask, state) => {
      const dropTime = resolveGridDropTime(state.position.x, state.position.y);
      if (!dropTime) return;
      const { start, durationMinutes } = taskDropIntent({ surface: "hourlyGrid", time: dropTime });
      void setTaskTimeBlock(draggedTask.id, start, durationMinutes);
    },
  });

  return (
    <>
      <div
        onMouseDown={(domEvent) => drag.start(task, domEvent.clientX, domEvent.clientY)}
        className="flex cursor-pointer items-center gap-2 rounded-shell-sm py-1.5 hover:bg-surface-hover"
      >
        <span
          // Stops the completion click (and the mousedown that precedes it)
          // from also starting a drag or opening the detail surface — the
          // two are one row's two separate gestures.
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
      {drag.active && (
        <TaskDragPreview
          x={drag.position.x}
          y={drag.position.y}
          title={task.title}
          blockStyle={getCalendarBlockStyle(taskList)}
        />
      )}
    </>
  );
}
