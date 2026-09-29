import { useTaskListsStore } from "./taskListsStore";
import { useShellStore } from "./shellStore";

// createTaskListCascade checks a Task List the moment it's created — an
// explicit act, not the "auto-check things I haven't seen" heuristic
// reconcileCheckedTaskListIds applies on refetch, so it must be checked
// immediately (#317, ADR-0083, mirroring createCalendarCascade.ts).
// Marking it known alongside checked (via addCheckedTaskListId, not
// toggleTaskListChecked) matters just as much: without it, the next
// reconcile treats the id as unseen and re-checks it even if the caller had
// since deliberately unchecked it. createTaskList throws on failure (unlike
// calendarsStore.addCalendar's own boolean-return-plus-rollback shape), so
// there is nothing to check or roll back until it has already succeeded.
export async function createTaskListCascade(name: string, color?: string): Promise<void> {
  const created = await useTaskListsStore.getState().createTaskList(name, color);
  useShellStore.getState().addCheckedTaskListId(created.id);
}
