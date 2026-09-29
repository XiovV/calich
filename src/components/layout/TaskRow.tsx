import { format } from "date-fns";
import { Menu } from "@base-ui/react/menu";
import { MoreVertical } from "lucide-react";
import { getCalendarBlockStyle } from "../../lib/calendarColors";
import { computeMoveToDate } from "../../lib/gridTime";
import { usePointerDrag } from "../../hooks/usePointerDrag";
import { useTaskListsStore } from "../../lib/taskListsStore";
import { getTaskListById } from "../../lib/taskListsApi";
import { taskDropIntent } from "../../lib/taskDropIntent";
import { taskTimeBlockEnd } from "../../lib/taskTimeBlockSegments";
import type { Task } from "../../lib/tasksApi";
import { useTasksStore } from "../../lib/tasksStore";
import { TaskCompletionControl } from "./TaskCompletionControl";
import { TaskDragPreview } from "../../calendar-grid/TaskDragPreview";
import { resolveGridDropTime, resolveMonthDropDate } from "../../calendar-grid/gridDropTargets";
import { iconButtonClasses } from "../ui/iconButtonClasses";

interface TaskRowProps {
  task: Task;
  onOpenDetail: (task: Task) => void;
}

const menuItemClasses =
  "flex cursor-default items-center px-3 py-1.5 text-body text-ink data-[highlighted]:bg-surface-hover";

// TaskRow is one row inside a Task bucket (#310, #311, #313, #315,
// ADR-0083): its own Deadline beside the title, so Thursday's work reads
// differently from Friday's inside the same bucket. A plain click opens the
// detail surface; a drag onto the hourly grid gives it a Time block instead
// (issue #309 story 47). A drag onto a Month Day cell moves that Time block,
// preserving its time-of-day, if the Task already has one — the row still
// shows for an already-scheduled Task, bucketed by its own start when it
// carries no Deadline — and otherwise gives it a Deadline instead: Month
// never invents an hour from a gesture that named none (#315, ADR-0083).
// Reuses `usePointerDrag` — the same gesture-pure mousedown-origin /
// window-mousemove-mouseup hook every draggable Occurrence uses. It works
// unmodified here even though the Task drag starts in a different DOM
// subtree than the grid it can land on: `usePointerDrag` never touches the
// DOM itself, and `TimeGrid`'s own all-day drag already resolves its drop
// target the same elementFromPoint way, from inside its `onDrag`.
export function TaskRow({ task, onOpenDetail }: TaskRowProps) {
  const setTaskCompleted = useTasksStore((state) => state.setTaskCompleted);
  const setTaskTimeBlock = useTasksStore((state) => state.setTaskTimeBlock);
  const clearTaskTimeBlock = useTasksStore((state) => state.clearTaskTimeBlock);
  const setTaskDeadlineAndClearTimeBlock = useTasksStore(
    (state) => state.setTaskDeadlineAndClearTimeBlock,
  );
  const taskList = useTaskListsStore((state) => getTaskListById(state.taskLists, task.taskListId));

  const drag = usePointerDrag<Task>({
    onClick: onOpenDetail,
    onDrag: (draggedTask, state) => {
      const monthDate = resolveMonthDropDate(state.position.x, state.position.y);
      if (monthDate) {
        // A row in the panel isn't always a fresh, unscheduled Task — one
        // already carrying a Time block still shows here (bucketed by its
        // own start when it has no Deadline), so this drag has to read as
        // "timeBlock", not a blanket "panelRow", or dropping it on a Month
        // Day cell would wrongly wipe that block into a Deadline instead of
        // preserving its time-of-day (#315, ADR-0083).
        if (draggedTask.start) {
          const { start } = computeMoveToDate(draggedTask.start, taskTimeBlockEnd(draggedTask), monthDate);
          const write = taskDropIntent(
            { kind: "timeBlock", durationMinutes: draggedTask.durationMinutes as number },
            { surface: "monthDay", date: start },
          );
          if (write.action !== "setTimeBlock") return;
          void setTaskTimeBlock(draggedTask.id, write.start, write.durationMinutes);
          return;
        }

        const write = taskDropIntent({ kind: "panelRow" }, { surface: "monthDay", date: monthDate });
        if (write.action !== "setDeadlineAndClearTimeBlock") return;
        void setTaskDeadlineAndClearTimeBlock(draggedTask.id, write.due);
        return;
      }

      const dropTime = resolveGridDropTime(state.position.x, state.position.y);
      if (!dropTime) return;
      const write = taskDropIntent({ kind: "panelRow" }, { surface: "hourlyGrid", time: dropTime });
      if (write.action !== "setTimeBlock") return;
      void setTaskTimeBlock(draggedTask.id, write.start, write.durationMinutes);
    },
  });

  return (
    <>
      <div
        onMouseDown={(domEvent) => drag.start(task, domEvent.clientX, domEvent.clientY)}
        className="group flex cursor-pointer items-center gap-2 rounded-shell-sm py-1.5 hover:bg-surface-hover"
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
        {/* Absent rather than disabled when there's no Time block to clear
            (mirrors CalendarList's own row-menu philosophy) — there being
            nothing else on this menu yet, that's the same as showing no
            trigger at all (#314). Placed here, not the detail modal, so
            removing a block never requires opening it first — the same
            "no precise drag required" reasoning the menu item itself answers
            to (issue #309 story 58). */}
        {task.start && (
          <Menu.Root>
            <Menu.Trigger
              aria-label={`"${task.title}" actions`}
              title={`"${task.title}" actions`}
              onMouseDown={(event) => event.stopPropagation()}
              onClick={(event) => event.stopPropagation()}
              className={iconButtonClasses({
                size: "tiny",
                className:
                  "shrink-0 opacity-0 focus-visible:opacity-100 group-hover:opacity-100 data-[popup-open]:opacity-100",
              })}
            >
              <MoreVertical className="size-3.5" />
            </Menu.Trigger>
            <Menu.Portal>
              <Menu.Positioner sideOffset={4} align="end" className="z-[60]">
                {/* Rendered through a portal, so it sits outside the row's
                    own DOM subtree — but React still bubbles its synthetic
                    events up through the *component* tree the JSX describes,
                    all the way to the row div's own onMouseDown. Without
                    this guard, mousing down on an item both starts the row's
                    drag gesture and (since the mouseup that follows the
                    click never moves) fires its onClick, reopening the
                    detail surface right after "Unschedule" runs. */}
                <Menu.Popup
                  onMouseDown={(event) => event.stopPropagation()}
                  className="rounded-shell-md border border-border bg-surface py-1 shadow-elevation-2"
                >
                  <Menu.Item
                    onClick={() => void clearTaskTimeBlock(task.id)}
                    className={menuItemClasses}
                  >
                    Unschedule
                  </Menu.Item>
                </Menu.Popup>
              </Menu.Positioner>
            </Menu.Portal>
          </Menu.Root>
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
