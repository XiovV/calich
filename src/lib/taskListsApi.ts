import { authedFetch, errorFromResponse } from "./apiClient";
import { workspaceHeaders } from "./workspaceHeaders";

// Task Lists (#317, ADR-0083): a named, private container of Tasks
// belonging to one User within one Workspace. Scoped to the currently
// active Workspace via workspaceHeaders, the same as every other
// Workspace-scoped call.

export interface TaskList {
  id: number;
  name: string;
  color: string;
  isDefault: boolean;
}

// getTaskListById mirrors calendar.ts's own getCalendarById — the same
// find-by-id lookup every Task List colour resolution needs (TaskRow,
// TaskBlock, TaskDeadlineChip, MonthTaskChip), kept in one place rather than
// inlined at each call site.
export function getTaskListById(
  taskLists: TaskList[],
  id: number,
): TaskList | undefined {
  return taskLists.find((list) => list.id === id);
}

export const taskListsApi = {
  // Every Task List the caller owns in the active Workspace.
  async list(accessToken: string): Promise<TaskList[]> {
    const response = await authedFetch(accessToken, "/api/task-lists/", {
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as TaskList[];
  },

  // Creates a new, non-default Task List named name in the active
  // Workspace. An empty color auto-assigns a Swatch.
  async create(accessToken: string, name: string, color = ""): Promise<TaskList> {
    const response = await authedFetch(accessToken, "/api/task-lists/", {
      method: "POST",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ name, color }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as TaskList;
  },

  // Renames id.
  async rename(accessToken: string, id: number, name: string): Promise<TaskList> {
    const response = await authedFetch(accessToken, `/api/task-lists/${id}`, {
      method: "PATCH",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ name }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as TaskList;
  },

  // Recolors id.
  async recolor(accessToken: string, id: number, color: string): Promise<TaskList> {
    const response = await authedFetch(accessToken, `/api/task-lists/${id}/color`, {
      method: "PATCH",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ color }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as TaskList;
  },

  // Promotes id to be the caller's default Task List, atomically clearing
  // whichever one held the flag before.
  async setDefault(accessToken: string, id: number): Promise<TaskList> {
    const response = await authedFetch(accessToken, `/api/task-lists/${id}/default`, {
      method: "PUT",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as TaskList;
  },

  // Deletes id outright. Refused by the backend while id still holds the
  // default flag.
  async remove(accessToken: string, id: number): Promise<void> {
    const response = await authedFetch(accessToken, `/api/task-lists/${id}`, {
      method: "DELETE",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
