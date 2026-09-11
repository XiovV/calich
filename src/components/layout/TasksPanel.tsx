import { useEffect, useState } from "react";
import { Menu } from "@base-ui/react/menu";
import { Check, ChevronDown, ChevronRight, MoreVertical, X } from "lucide-react";
import { viewerZone } from "../../lib/floatingTime";
import { useShellStore } from "../../lib/shellStore";
import { TASK_BUCKET_LABELS, TASK_BUCKET_ORDER, bucketTasks } from "../../lib/taskScheduling";
import type { Task } from "../../lib/tasksApi";
import { useTasksStore } from "../../lib/tasksStore";
import { useWorkspacesStore } from "../../lib/workspacesStore";
import { IconButton } from "../ui/IconButton";
import { iconButtonClasses } from "../ui/iconButtonClasses";
import { TaskDetailModal } from "./TaskDetailModal";
import { TaskListsFilter } from "./TaskListsFilter";
import { TaskQuickAdd } from "./TaskQuickAdd";
import { TaskRow } from "./TaskRow";

// TasksPanel is the right-hand panel holding a User's Tasks (#310, #311,
// #317, ADR-0083): toggled from the top bar's Tasks button, closed by
// default, squeezing the grid rather than overlaying it. Splits into the
// four Task buckets — derived at render, never stored (ADR-0083) — each
// with its own count and each row showing its own Deadline. A completed
// Task, once "Show completed" is on, rejoins its own bucket rather than
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
  const buckets = bucketTasks(visibleTasks, new Date(), viewerZone());

  return (
    <div className="flex h-full flex-col gap-3 overflow-y-auto p-4">
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
                    className="flex cursor-default items-center gap-2 px-3 py-1.5 text-body text-ink data-[highlighted]:bg-surface-hover"
                  >
                    <Menu.CheckboxItemIndicator
                      keepMounted
                      className="flex size-4 shrink-0 items-center justify-center data-[unchecked]:opacity-0"
                    >
                      <Check className="size-4 text-accent-ink" />
                    </Menu.CheckboxItemIndicator>
                    Show tasks on calendar
                  </Menu.CheckboxItem>
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
      {visibleTasks.length === 0 ? (
        <p className="text-body text-ink-muted">No tasks yet.</p>
      ) : (
        <div className="flex flex-col gap-4">
          {TASK_BUCKET_ORDER.map((bucket) => (
            <div key={bucket} className="flex flex-col">
              <h3 className="text-label-sm font-medium text-ink-muted">
                {TASK_BUCKET_LABELS[bucket]} ({buckets[bucket].length})
              </h3>
              {buckets[bucket].map((task) => (
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
