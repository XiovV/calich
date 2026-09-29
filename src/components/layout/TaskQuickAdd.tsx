import { useState } from "react";
import { useShellStore } from "../../lib/shellStore";
import { useTaskListsStore } from "../../lib/taskListsStore";
import { resolveQuickAddTaskListId, useTasksStore } from "../../lib/tasksStore";
import { toast } from "../../lib/toast";

// TaskQuickAdd is the Tasks panel's capture field (#310): type a title,
// press Enter, and the Task appears — no dialog, no Save button. Commits to
// the default Task List, or the single checked Task List when exactly one
// is checked in the Lists filter above it (resolveQuickAddTaskListId).
export function TaskQuickAdd() {
  const [title, setTitle] = useState("");
  const taskLists = useTaskListsStore((state) => state.taskLists);
  const checkedTaskListIds = useShellStore((state) => state.checkedTaskListIds);
  const createTask = useTasksStore((state) => state.createTask);

  async function handleCommit() {
    const trimmed = title.trim();
    if (!trimmed) return;

    const taskListId = resolveQuickAddTaskListId(taskLists, checkedTaskListIds);
    if (taskListId === null) return;

    try {
      await createTask(trimmed, taskListId);
      // Cleared only on success: clearing eagerly would lose the typed
      // title on a failed create, with no dialog left open to recover it
      // from.
      setTitle("");
    } catch {
      toast.error("Couldn't create the task.");
    }
  }

  return (
    <input
      value={title}
      onChange={(event) => setTitle(event.target.value)}
      onKeyDown={(event) => {
        if (event.key === "Enter") handleCommit();
      }}
      placeholder="Add a task"
      aria-label="Add a task"
      className="w-full rounded-shell-sm border border-border bg-surface px-2 py-1.5 text-body text-ink outline-none focus:ring-1 focus:ring-accent-ink"
    />
  );
}
