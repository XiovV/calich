import { useEffect } from "react";
import { ChevronDown, ChevronRight, X } from "lucide-react";
import { useShellStore } from "../../lib/shellStore";
import { useTasksStore } from "../../lib/tasksStore";
import { useWorkspacesStore } from "../../lib/workspacesStore";
import { IconButton } from "../ui/IconButton";
import { TaskListsFilter } from "./TaskListsFilter";
import { TaskQuickAdd } from "./TaskQuickAdd";
import { TaskRow } from "./TaskRow";

// TasksPanel is the right-hand panel holding a User's Tasks (#310, #317,
// ADR-0083): toggled from the top bar's Tasks button, closed by default,
// squeezing the grid rather than overlaying it. No Deadlines yet, so every
// Task renders in one flat list — the demoable end state is capture, tick
// off, and show/hide completed (#310); the bucketed/grouped panel ADR-0083
// describes is a later ticket.
export function TasksPanel() {
  const setTasksPanelOpen = useShellStore((state) => state.setTasksPanelOpen);
  const tasks = useTasksStore((state) => state.tasks);
  const completedTasks = useTasksStore((state) => state.completedTasks);
  const fetchTasks = useTasksStore((state) => state.fetchTasks);
  const fetchCompletedTasks = useTasksStore((state) => state.fetchCompletedTasks);
  const showCompletedTasks = useShellStore((state) => state.showCompletedTasks);
  const setShowCompletedTasks = useShellStore((state) => state.setShowCompletedTasks);
  const activeWorkspaceId = useWorkspacesStore((state) => state.activeWorkspaceId);

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

  return (
    <div className="flex h-full flex-col gap-3 overflow-y-auto p-4">
      <div className="flex items-center justify-between">
        <h2 className="text-heading text-ink">Tasks</h2>
        <IconButton
          size="small"
          onClick={() => setTasksPanelOpen(false)}
          aria-label="Close Tasks panel"
        >
          <X className="size-4" />
        </IconButton>
      </div>
      <TaskListsFilter />
      <TaskQuickAdd />
      <div className="flex flex-col">
        {tasks.length === 0 ? (
          <p className="text-body text-ink-muted">No tasks yet.</p>
        ) : (
          tasks.map((task) => <TaskRow key={task.id} task={task} />)
        )}
      </div>
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
      {showCompletedTasks && (
        <div className="flex flex-col">
          {completedTasks.map((task) => (
            <TaskRow key={task.id} task={task} />
          ))}
        </div>
      )}
    </div>
  );
}
