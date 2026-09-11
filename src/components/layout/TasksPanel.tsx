import { X } from "lucide-react";
import { useShellStore } from "../../lib/shellStore";
import { IconButton } from "../ui/IconButton";
import { TaskListsFilter } from "./TaskListsFilter";

// TasksPanel is the right-hand panel holding a User's Tasks (#317,
// ADR-0083): toggled from the top bar's Tasks button, closed by default,
// squeezing the grid rather than overlaying it (AppShell renders it as a
// flex sibling of <main>, exactly like the left Sidebar). This ticket
// delivers the containers (Task Lists) and the panel they live in — no Task
// exists yet, so the body below the Lists filter is a placeholder until a
// later ticket adds Tasks themselves.
export function TasksPanel() {
  const setTasksPanelOpen = useShellStore((state) => state.setTasksPanelOpen);

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
      <p className="text-body text-ink-muted">No tasks yet.</p>
    </div>
  );
}
