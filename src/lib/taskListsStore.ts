import { create } from "zustand";
import { useAuthStore } from "./authStore";
import { type TaskList, taskListsApi } from "./taskListsApi";

// The Tasks panel's Lists filter data source (#317, ADR-0083): every Task
// List the caller owns in the active Workspace. Mirrors calendarSetsStore's
// shape. Deliberately no cross-store reach into shellStore here (unlike
// calendarSetsStore.deleteCalendarSet) — createTaskListCascade.ts is where
// "checked the moment it's created" is composed, the same split
// createCalendarCascade.ts already keeps between calendarsStore and
// shellStore.
interface TaskListsState {
  taskLists: TaskList[];
  fetchTaskLists: () => Promise<void>;
  createTaskList: (name: string, color?: string) => Promise<TaskList>;
  renameTaskList: (id: number, name: string) => Promise<void>;
  recolorTaskList: (id: number, color: string) => Promise<void>;
  setDefaultTaskList: (id: number) => Promise<void>;
  deleteTaskList: (id: number) => Promise<void>;
}

function requireAccessToken(): string {
  const accessToken = useAuthStore.getState().accessToken;
  if (!accessToken) throw new Error("Not authenticated.");
  return accessToken;
}

export const useTaskListsStore = create<TaskListsState>((set, get) => ({
  taskLists: [],

  fetchTaskLists: async () => {
    const taskLists = await taskListsApi.list(requireAccessToken());
    set({ taskLists });
  },

  createTaskList: async (name, color) => {
    const created = await taskListsApi.create(requireAccessToken(), name, color);
    set({ taskLists: [...get().taskLists, created] });
    return created;
  },

  renameTaskList: async (id, name) => {
    const renamed = await taskListsApi.rename(requireAccessToken(), id, name);
    set({ taskLists: get().taskLists.map((l) => (l.id === id ? renamed : l)) });
  },

  recolorTaskList: async (id, color) => {
    const recolored = await taskListsApi.recolor(requireAccessToken(), id, color);
    set({ taskLists: get().taskLists.map((l) => (l.id === id ? recolored : l)) });
  },

  // Promoting id to default clears the flag locally on whichever list held
  // it before, mirroring the atomic clear-then-set the backend performs in
  // one transaction (ADR-0083).
  setDefaultTaskList: async (id) => {
    const promoted = await taskListsApi.setDefault(requireAccessToken(), id);
    set({
      taskLists: get().taskLists.map((l) =>
        l.id === promoted.id ? promoted : l.isDefault ? { ...l, isDefault: false } : l,
      ),
    });
  },

  deleteTaskList: async (id) => {
    await taskListsApi.remove(requireAccessToken(), id);
    set({ taskLists: get().taskLists.filter((l) => l.id !== id) });
  },
}));
