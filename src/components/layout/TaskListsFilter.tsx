import { useEffect, useState } from "react";
import { Menu } from "@base-ui/react/menu";
import { ChevronDown, Plus } from "lucide-react";
import { refetchTaskListsAndReconcile, useShellStore } from "../../lib/shellStore";
import { useTaskListsStore } from "../../lib/taskListsStore";
import { useWorkspacesStore } from "../../lib/workspacesStore";
import { createTaskListCascade } from "../../lib/createTaskListCascade";
import { getNextUnusedColor } from "../../lib/calendarColors";
import { toast } from "../../lib/toast";
import { TaskListToggle } from "./TaskListToggle";

const menuItemClasses =
  "flex cursor-default items-center gap-2 px-3 py-1.5 text-body text-ink data-[highlighted]:bg-surface-hover";

// TaskListsFilter is the Tasks panel's Lists filter (#317, ADR-0083): every
// Task List the caller has in the active Workspace, each with its own
// colour checkbox, a checked-count on the trigger, and a "New list" entry
// that creates one without leaving the panel. The Task List counterpart to
// the sidebar's CalendarList, condensed into one dropdown the way
// CalendarSetSwitcher condenses Calendar Sets — except multi-select
// (several Lists can be checked at once) rather than CalendarSetSwitcher's
// single active choice, so each row is a plain checkbox row rather than a
// Menu.RadioItem.
//
// The name field for a new list renders as a plain input *below* the Menu
// rather than inside its Popup: a base-ui Menu owns Enter/Escape/typeahead
// for its own roving-focus navigation, which fights a real text input
// mounted inside it. Closing the dropdown when "New list" is clicked (the
// ordinary Menu.Item behaviour) and revealing the field in the panel below
// it still satisfies "without leaving the panel" — nothing here navigates
// away from TasksPanel.
export function TaskListsFilter() {
  const taskLists = useTaskListsStore((state) => state.taskLists);
  const checkedTaskListIds = useShellStore((state) => state.checkedTaskListIds);
  const toggleTaskListChecked = useShellStore((state) => state.toggleTaskListChecked);
  const [isCreating, setIsCreating] = useState(false);
  const [draftName, setDraftName] = useState("");
  const activeWorkspaceId = useWorkspacesStore((state) => state.activeWorkspaceId);

  useEffect(() => {
    // Refetches on mount and whenever the active Workspace changes (#317,
    // ADR-0083, mirroring AppShell's own Calendar refetch effect) — Task
    // Lists are scoped to the active Workspace, so switching Workspaces
    // while the panel happens to be open must swap what this shows rather
    // than leaving the previous Workspace's Lists on screen. A failure here
    // costs the filter its Task List options and nothing else, same posture
    // as CalendarSetSwitcher's own fetch effect.
    if (activeWorkspaceId === null) return;
    refetchTaskListsAndReconcile().catch(() => {});
  }, [activeWorkspaceId]);

  const checkedCount = taskLists.filter((list) => checkedTaskListIds.has(list.id)).length;

  async function handleCreate() {
    const name = draftName.trim();
    setIsCreating(false);
    setDraftName("");
    if (!name) return;

    try {
      const usedColors = taskLists.map((list) => list.color);
      await createTaskListCascade(name, getNextUnusedColor(usedColors));
    } catch {
      toast.error("Couldn't create the list.");
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <Menu.Root>
        <Menu.Trigger
          aria-label="Lists filter"
          className="flex w-full cursor-pointer items-center justify-between gap-2 rounded-shell-md bg-surface-sunken px-3 py-2 text-body text-ink ring-1 ring-border transition-colors outline-none hover:bg-surface-hover data-[popup-open]:ring-2 data-[popup-open]:ring-accent-ink"
        >
          <span>Lists</span>
          <span className="flex items-center gap-1 text-label-sm text-ink-muted">
            {checkedCount} of {taskLists.length}
            <ChevronDown className="size-4" />
          </span>
        </Menu.Trigger>
        <Menu.Portal>
          <Menu.Positioner sideOffset={4} className="z-[60]">
            <Menu.Popup className="min-w-[--anchor-width] rounded-shell-md border border-border bg-surface py-1.5 shadow-elevation-2">
              {taskLists.map((taskList) => (
                <div key={taskList.id} className={menuItemClasses}>
                  <TaskListToggle
                    checked={checkedTaskListIds.has(taskList.id)}
                    onCheckedChange={() => toggleTaskListChecked(taskList.id)}
                    color={taskList.color}
                    aria-label={taskList.name}
                  />
                  <span className="min-w-0 flex-1 truncate">{taskList.name}</span>
                </div>
              ))}
              <div role="separator" className="my-1 border-t border-border" />
              <Menu.Item onClick={() => setIsCreating(true)} className={menuItemClasses}>
                <Plus className="size-4" />
                New list
              </Menu.Item>
            </Menu.Popup>
          </Menu.Positioner>
        </Menu.Portal>
      </Menu.Root>
      {isCreating && (
        <input
          autoFocus
          value={draftName}
          onChange={(event) => setDraftName(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") handleCreate();
            if (event.key === "Escape") {
              setIsCreating(false);
              setDraftName("");
            }
          }}
          onBlur={handleCreate}
          placeholder="List name"
          aria-label="New list name"
          className="w-full rounded-shell-sm border border-border bg-surface px-2 py-1.5 text-body text-ink outline-none focus:ring-1 focus:ring-accent-ink"
        />
      )}
    </div>
  );
}
