import { create } from "zustand";
import { useAuthStore } from "./authStore";
import { makeOptimisticWrite } from "./optimisticWrite";
import { type Task, tasksApi } from "./tasksApi";
import type { TaskList } from "./taskListsApi";

// The Tasks panel's own data source (#310, #311, ADR-0083): every
// incomplete Task the caller owns in the active Workspace, plus a bounded
// tail of completed ones fetched only when "Show completed" asks.
// fetchTasks/fetchCompletedTasks/createTask and the detail surface's own
// writes (notes, Deadline, Priority, Task List) all mirror taskListsStore's
// server-first discipline (ADR-0067): the dialog is open to receive a
// failure, so there's nothing to paint ahead of the server. Completion is
// the one write that IS optimistic — the click is the feedback, and the
// client already knows the result before asking (ADR-0067, ADR-0068).
interface TasksState {
  tasks: Task[];
  completedTasks: Task[];
  fetchTasks: () => Promise<void>;
  fetchCompletedTasks: () => Promise<void>;
  createTask: (title: string, taskListId: number) => Promise<Task>;
  updateTaskNotes: (id: number, notes: string) => Promise<Task>;
  setTaskDeadline: (id: number, due: Date) => Promise<Task>;
  clearTaskDeadline: (id: number) => Promise<Task>;
  updateTaskPriority: (id: number, priority: number) => Promise<Task>;
  moveTask: (id: number, taskListId: number) => Promise<Task>;
  deleteTask: (id: number) => Promise<void>;
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

// replaceTask swaps `updated` in for its own id, in whichever of `tasks`/
// `completedTasks` currently holds it — a detail-surface write never knows
// ahead of time which list its Task lives in.
function replaceTask(set: (fn: (state: TasksState) => Partial<TasksState>) => void, updated: Task) {
  set((state) => ({
    tasks: state.tasks.map((t) => (t.id === updated.id ? updated : t)),
    completedTasks: state.completedTasks.map((t) => (t.id === updated.id ? updated : t)),
  }));
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

  // The detail surface's own fields (#311): a Task may be sitting in either
  // `tasks` or `completedTasks` depending on whether it's done, so each
  // write replaces it in whichever of the two actually holds it, leaving
  // the other untouched.
  updateTaskNotes: async (id, notes) => {
    const updated = await tasksApi.updateNotes(requireAccessToken(), id, notes);
    replaceTask(set, updated);
    return updated;
  },

  setTaskDeadline: async (id, due) => {
    const updated = await tasksApi.setDeadline(requireAccessToken(), id, due);
    replaceTask(set, updated);
    return updated;
  },

  clearTaskDeadline: async (id) => {
    const updated = await tasksApi.clearDeadline(requireAccessToken(), id);
    replaceTask(set, updated);
    return updated;
  },

  updateTaskPriority: async (id, priority) => {
    const updated = await tasksApi.updatePriority(requireAccessToken(), id, priority);
    replaceTask(set, updated);
    return updated;
  },

  moveTask: async (id, taskListId) => {
    const updated = await tasksApi.move(requireAccessToken(), id, taskListId);
    replaceTask(set, updated);
    return updated;
  },

  deleteTask: async (id) => {
    await tasksApi.remove(requireAccessToken(), id);
    set((state) => ({
      tasks: state.tasks.filter((t) => t.id !== id),
      completedTasks: state.completedTasks.filter((t) => t.id !== id),
    }));
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
