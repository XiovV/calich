import { create } from "zustand";
import { useAuthStore } from "./authStore";
import { makeOptimisticWrite } from "./optimisticWrite";
import { type Task, tasksApi } from "./tasksApi";
import type { TaskList } from "./taskListsApi";

// The Tasks panel's own data source (#310, ADR-0083): every incomplete Task
// the caller owns in the active Workspace, plus a bounded tail of completed
// ones fetched only when "Show completed" asks. fetchTasks/fetchCompletedTasks/
// createTask mirror taskListsStore's server-first discipline (ADR-0067): the
// surface showing the result is a dialog-less list, and each of those writes
// already knows nothing more than what the server hands back. Completion is
// the one write that IS optimistic — the click is the feedback, and the
// client already knows the result before asking (ADR-0067, ADR-0068).
// update and delete exist on the server (/api/tasks supports both) but
// nothing in this ticket's UI calls either yet — no rename or delete
// affordance on a Task row — so this store exposes only what quick-add and
// the completion control actually use.
interface TasksState {
  tasks: Task[];
  completedTasks: Task[];
  fetchTasks: () => Promise<void>;
  fetchCompletedTasks: () => Promise<void>;
  createTask: (title: string, taskListId: number) => Promise<Task>;
  setTaskCompleted: (id: number, completed: boolean) => Promise<boolean>;
}

// resolveQuickAddTaskListId is quick-add's own targeting rule (#310): the
// single checked Task List when exactly one is checked in the Lists filter
// — the filter already states the User's intent — otherwise the default
// Task List. checkedTaskListIds is intersected with taskLists itself rather
// than trusted alone, so a stale checked id (a Task List deleted elsewhere,
// not yet reconciled) can't be resolved to. Returns null only if taskLists
// carries no default at all, which never happens in practice (ADR-0083:
// exactly one Task List per (User, Workspace) is always the default).
export function resolveQuickAddTaskListId(
  taskLists: TaskList[],
  checkedTaskListIds: Set<number>,
): number | null {
  const checked = taskLists.filter((list) => checkedTaskListIds.has(list.id));
  if (checked.length === 1) return checked[0].id;

  return taskLists.find((list) => list.isDefault)?.id ?? null;
}

function requireAccessToken(): string {
  const accessToken = useAuthStore.getState().accessToken;
  if (!accessToken) throw new Error("Not authenticated.");
  return accessToken;
}

// Binds no Access-change policy (ADR-0067): a Task has no Calendar to name,
// and is private outright — there is no Share whose revocation could change
// what the caller can reach.
const write = makeOptimisticWrite();

export const useTasksStore = create<TasksState>((set, get) => ({
  tasks: [],
  completedTasks: [],

  fetchTasks: async () => {
    const tasks = await tasksApi.list(requireAccessToken());
    set({ tasks });
  },

  fetchCompletedTasks: async () => {
    const completedTasks = await tasksApi.listCompleted(requireAccessToken());
    set({ completedTasks });
  },

  createTask: async (title, taskListId) => {
    const created = await tasksApi.create(requireAccessToken(), title, taskListId);
    set({ tasks: [...get().tasks, created] });
    return created;
  },

  // setTaskCompleted paints the toggle immediately and puts it back on
  // failure (ADR-0067, ADR-0068) — no confirmation, one click either way.
  // Completing moves the Task out of the incomplete list and into the
  // completed one (and back, un-completing), since the two are separate
  // fetches rather than one list filtered client-side (ADR-0083: completed
  // Tasks are returned only when asked for).
  setTaskCompleted: async (id, completed) => {
    const task = get().tasks.find((t) => t.id === id) ?? get().completedTasks.find((t) => t.id === id);
    if (!task) return false;

    return write({
      apply: () => {
        if (completed) {
          set((state) => ({
            tasks: state.tasks.filter((t) => t.id !== id),
            completedTasks: [{ ...task, completed: true }, ...state.completedTasks],
          }));
        } else {
          set((state) => ({
            completedTasks: state.completedTasks.filter((t) => t.id !== id),
            tasks: [...state.tasks, { ...task, completed: false }],
          }));
        }
      },
      revert: () => {
        if (completed) {
          set((state) => ({
            completedTasks: state.completedTasks.filter((t) => t.id !== id),
            tasks: [...state.tasks, task],
          }));
        } else {
          set((state) => ({
            tasks: state.tasks.filter((t) => t.id !== id),
            completedTasks: [task, ...state.completedTasks],
          }));
        }
      },
      dispatch: async () => {
        if (completed) {
          await tasksApi.complete(requireAccessToken(), id);
        } else {
          await tasksApi.uncomplete(requireAccessToken(), id);
        }
      },
      fallbackMessage: completed ? "Couldn't complete the task." : "Couldn't reopen the task.",
    });
  },
}));
