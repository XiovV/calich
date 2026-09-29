import { useEffect, useState } from "react";
import { Menu } from "@base-ui/react/menu";
import { Check, ChevronDown, ChevronRight, MoreVertical, X } from "lucide-react";
import { toOpaqueHex } from "../../lib/calendarColors";
import { viewerZone } from "../../lib/floatingTime";
import { type TasksPanelAxis, useShellStore } from "../../lib/shellStore";
import {
  PRIORITY_HEADING_ORDER,
  TASK_BUCKET_LABELS,
  TASK_BUCKET_ORDER,
  bucketTasks,
  tasksByPriorityLevel,
  tasksByTaskList,
} from "../../lib/taskScheduling";
import { PRIORITY_LEVEL_LABELS } from "../../lib/taskPriority";
import { useTaskListsStore } from "../../lib/taskListsStore";
import type { TaskList } from "../../lib/taskListsApi";
import type { Task } from "../../lib/tasksApi";
import { useTasksStore } from "../../lib/tasksStore";
import { useWorkspacesStore } from "../../lib/workspacesStore";
import { IconButton } from "../ui/IconButton";
import { iconButtonClasses } from "../ui/iconButtonClasses";
import { TaskDetailModal } from "./TaskDetailModal";
import { TaskListsFilter } from "./TaskListsFilter";
import { TaskQuickAdd } from "./TaskQuickAdd";
import { TaskRow } from "./TaskRow";

const menuItemClasses =
  "flex cursor-default items-center gap-2 px-3 py-1.5 text-body text-ink data-[highlighted]:bg-surface-hover";

// A heading the panel currently renders — one of the three axes' own
// vocabulary (Task bucket / Task List / Priority label, never "group"; see
// TasksPanelAxis) collapsed to what JSX actually needs to draw one: a key,
// a label, an optional colour, and its own pre-sorted Tasks.
interface PanelHeading {
  key: string;
  label: string;
  color: string | null;
  tasks: Task[];
}

// computePanelHeadings resolves tasksPanelAxis into what JSX renders,
// keeping each axis's own rule in one place (#316, ADR-0083): Task bucket
// always renders its four headings via bucketTasks; Priority always renders
// its four via tasksByPriorityLevel/PRIORITY_HEADING_ORDER; Task List
// renders one heading per *checked* Task List, in list order, so an
// unchecked list's Tasks fall out the same way the Lists filter already
// implies elsewhere. Ordering within a heading is unchanged across axes —
// every branch's Tasks arrive pre-sorted by compareTasksWithinHeading.
function computePanelHeadings(
  axis: TasksPanelAxis,
  tasks: Task[],
  taskLists: TaskList[],
  checkedTaskListIds: Set<number>,
): PanelHeading[] {
  if (axis === "taskList") {
    const byTaskList = tasksByTaskList(tasks);
    return taskLists
      .filter((taskList) => checkedTaskListIds.has(taskList.id))
      .map((taskList) => ({
        key: String(taskList.id),
        label: taskList.name,
        color: taskList.color,
        tasks: byTaskList.get(taskList.id) ?? [],
      }));
  }

  if (axis === "priority") {
    const byPriority = tasksByPriorityLevel(tasks);
    return PRIORITY_HEADING_ORDER.map((level) => ({
      key: level,
      label: PRIORITY_LEVEL_LABELS[level],
      color: null,
      tasks: byPriority[level],
    }));
  }

  const buckets = bucketTasks(tasks, new Date(), viewerZone());
  return TASK_BUCKET_ORDER.map((bucket) => ({
    key: bucket,
    label: TASK_BUCKET_LABELS[bucket],
    color: null,
    tasks: buckets[bucket],
  }));
}

// TasksPanel is the right-hand panel holding a User's Tasks (#310, #311,
// #316, #317, ADR-0083): toggled from the top bar's Tasks button, closed by
// default, squeezing the grid rather than overlaying it. Arranges its Tasks
// under one of three headings at a time — Task bucket (the default), Task
// List or Priority — chosen from the "…" menu's Group by submenu and held
// as tasksPanelAxis, derived at render either way (ADR-0083). Each heading
// carries its own count and each row shows its own Deadline. A completed
// Task, once "Show completed" is on, rejoins its own heading rather than
// sitting in a separate pile: one finished late still reads as Overdue,
// struck through, because it was (CONTEXT.md's Completed entry).
export function TasksPanel() {
  const setTasksPanelOpen = useShellStore((state) => state.setTasksPanelOpen);
  const tasks = useTasksStore((state) => state.tasks);
  const completedTasks = useTasksStore((state) => state.completedTasks);
  const fetchTasks = useTasksStore((state) => state.fetchTasks);
  const fetchCompletedTasks = useTasksStore((state) => state.fetchCompletedTasks);
  const showCompletedTasks = useShellStore((state) => state.showCompletedTasks);
  const setShowCompletedTasks = useShellStore((state) => state.setShowCompletedTasks);
  const showTasksOnCalendar = useShellStore((state) => state.showTasksOnCalendar);
  const setShowTasksOnCalendar = useShellStore((state) => state.setShowTasksOnCalendar);
  const tasksPanelAxis = useShellStore((state) => state.tasksPanelAxis);
  const setTasksPanelAxis = useShellStore((state) => state.setTasksPanelAxis);
  const checkedTaskListIds = useShellStore((state) => state.checkedTaskListIds);
  const taskLists = useTaskListsStore((state) => state.taskLists);
  const activeWorkspaceId = useWorkspacesStore((state) => state.activeWorkspaceId);
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);

  useEffect(() => {
    // Refetches on mount and whenever the active Workspace changes, same
    // posture as TaskListsFilter's own effect — a failure here costs the
    // panel its Tasks and nothing else.
    if (activeWorkspaceId === null) return;
    fetchTasks().catch(() => {});
  }, [activeWorkspaceId, fetchTasks]);

  useEffect(() => {
    // Completed Tasks are fetched only once asked for (ADR-0083) — opening
    // "Show completed" is what triggers this, not mounting the panel.
    if (!showCompletedTasks || activeWorkspaceId === null) return;
    fetchCompletedTasks().catch(() => {});
  }, [showCompletedTasks, activeWorkspaceId, fetchCompletedTasks]);

  // selectedTask is looked up by id off the store on every render rather
  // than held as the object itself, so an edit made inside TaskDetailModal
  // (which replaces the Task in the store) is reflected back into the
  // still-open modal instead of showing a stale snapshot.
  const openTask = selectedTask
    ? (tasks.find((t) => t.id === selectedTask.id) ??
      completedTasks.find((t) => t.id === selectedTask.id) ??
      null)
    : null;

  const visibleTasks = showCompletedTasks ? [...tasks, ...completedTasks] : tasks;
  const headings = computePanelHeadings(tasksPanelAxis, visibleTasks, taskLists, checkedTaskListIds);
  // Reads off headings rather than visibleTasks.length: the Task List axis
  // can hold Tasks with no checked heading to render them under (every Task
  // List unchecked), in which case there's nothing to show even though
  // visibleTasks itself isn't empty.
  const hasVisibleTasks = headings.some((heading) => heading.tasks.length > 0);

  return (
    <div
      // Read by TimeGrid's grid-block drag (#314) via `isPointOverTasksPanel`,
      // the panel's own drop-target counterpart to DayColumn's
      // `data-grid-day-ms` and AllDayLane's `data-allday-date` — the same
      // elementFromPoint technique, this time asking only "is the pointer
      // over the panel at all" rather than resolving a day or a time.
      data-tasks-panel
      className="flex h-full flex-col gap-3 overflow-y-auto p-4"
    >
      <div className="flex items-center justify-between">
        <h2 className="text-heading text-ink">Tasks</h2>
        <div className="flex items-center gap-1">
          <Menu.Root>
            <Menu.Trigger
              aria-label="Tasks panel options"
              className={iconButtonClasses({ size: "small" })}
            >
              <MoreVertical className="size-4" />
            </Menu.Trigger>
            <Menu.Portal>
              <Menu.Positioner sideOffset={4} align="end" className="z-[60]">
                <Menu.Popup className="min-w-56 rounded-shell-md border border-border bg-surface py-1 shadow-elevation-2">
                  <Menu.CheckboxItem
                    checked={showTasksOnCalendar}
                    onCheckedChange={setShowTasksOnCalendar}
                    className={menuItemClasses}
                  >
                    <Menu.CheckboxItemIndicator
                      keepMounted
                      className="flex size-4 shrink-0 items-center justify-center data-[unchecked]:opacity-0"
                    >
                      <Check className="size-4 text-accent-ink" />
                    </Menu.CheckboxItemIndicator>
                    Show tasks on calendar
                  </Menu.CheckboxItem>
                  <div role="separator" className="my-1 border-t border-border" />
                  <Menu.SubmenuRoot>
                    <Menu.SubmenuTrigger className={`${menuItemClasses} justify-between`}>
                      Group by
                      <ChevronRight className="ml-2 size-3.5 shrink-0 text-ink-muted" />
                    </Menu.SubmenuTrigger>
                    <Menu.Portal>
                      <Menu.Positioner sideOffset={4} className="z-[60]">
                        <Menu.Popup className="min-w-44 rounded-shell-md border border-border bg-surface py-1 shadow-elevation-2">
                          <Menu.RadioGroup
                            value={tasksPanelAxis}
                            onValueChange={(value: string) =>
                              setTasksPanelAxis(value as TasksPanelAxis)
                            }
                          >
                            <Menu.RadioItem value="taskBucket" closeOnClick className={menuItemClasses}>
                              <Menu.RadioItemIndicator
                                keepMounted
                                className="flex size-4 shrink-0 items-center justify-center data-[unchecked]:opacity-0"
                              >
                                <Check className="size-4 text-accent-ink" />
                              </Menu.RadioItemIndicator>
                              Task bucket
                            </Menu.RadioItem>
                            <Menu.RadioItem value="taskList" closeOnClick className={menuItemClasses}>
                              <Menu.RadioItemIndicator
                                keepMounted
                                className="flex size-4 shrink-0 items-center justify-center data-[unchecked]:opacity-0"
                              >
                                <Check className="size-4 text-accent-ink" />
                              </Menu.RadioItemIndicator>
                              Task List
                            </Menu.RadioItem>
                            <Menu.RadioItem value="priority" closeOnClick className={menuItemClasses}>
                              <Menu.RadioItemIndicator
                                keepMounted
                                className="flex size-4 shrink-0 items-center justify-center data-[unchecked]:opacity-0"
                              >
                                <Check className="size-4 text-accent-ink" />
                              </Menu.RadioItemIndicator>
                              Priority
                            </Menu.RadioItem>
                          </Menu.RadioGroup>
                        </Menu.Popup>
                      </Menu.Positioner>
                    </Menu.Portal>
                  </Menu.SubmenuRoot>
                </Menu.Popup>
              </Menu.Positioner>
            </Menu.Portal>
          </Menu.Root>
          <IconButton
            size="small"
            onClick={() => setTasksPanelOpen(false)}
            aria-label="Close Tasks panel"
          >
            <X className="size-4" />
          </IconButton>
        </div>
      </div>
      <TaskListsFilter />
      <TaskQuickAdd />
      {!hasVisibleTasks ? (
        <p className="text-body text-ink-muted">No tasks yet.</p>
      ) : (
        <div className="flex flex-col gap-4">
          {headings.map((heading) => (
            <div key={heading.key} className="flex flex-col">
              <h3
                className="text-label-sm font-medium text-ink-muted"
                style={heading.color ? { color: toOpaqueHex(heading.color) } : undefined}
              >
                {heading.label} ({heading.tasks.length})
              </h3>
              {heading.tasks.map((task) => (
                <TaskRow key={task.id} task={task} onOpenDetail={setSelectedTask} />
              ))}
            </div>
          ))}
        </div>
      )}
      <button
        type="button"
        onClick={() => setShowCompletedTasks(!showCompletedTasks)}
        className="flex cursor-pointer items-center gap-1 self-start text-label-sm text-ink-muted hover:text-ink"
      >
        {showCompletedTasks ? (
          <ChevronDown className="size-4" />
        ) : (
          <ChevronRight className="size-4" />
        )}
        Show completed
      </button>
      {openTask && (
        <TaskDetailModal key={openTask.id} task={openTask} onClose={() => setSelectedTask(null)} />
      )}
    </div>
  );
}
