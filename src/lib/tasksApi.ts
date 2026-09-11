import { authedFetch, errorFromResponse } from "./apiClient";
import { workspaceHeaders } from "./workspaceHeaders";

// Tasks (#310, ADR-0083): a unit of work a User tracks, belonging to
// exactly one Task List and private to its owner. Scoped to the active
// Workspace via workspaceHeaders, same as every other Workspace-scoped
// call. This ticket only ever reads/writes title and completed — notes,
// due, start and priority exist on the server but nothing here sends them
// yet.

export interface Task {
  id: number;
  taskListId: number;
  title: string;
  completed: boolean;
}

export const tasksApi = {
  // Every incomplete Task the caller owns in the active Workspace,
  // unwindowed by date.
  async list(accessToken: string): Promise<Task[]> {
    const response = await authedFetch(accessToken, "/api/tasks/", {
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Task[];
  },

  // The caller's most recently completed Tasks, bounded to a recent tail
  // rather than all history — fetched only when "Show completed" asks.
  async listCompleted(accessToken: string): Promise<Task[]> {
    const response = await authedFetch(accessToken, "/api/tasks/completed", {
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Task[];
  },

  // Creates a Task titled title in taskListId — the quick-add field's
  // commit-on-Enter.
  async create(accessToken: string, title: string, taskListId: number): Promise<Task> {
    const response = await authedFetch(accessToken, "/api/tasks/", {
      method: "POST",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ title, taskListId }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Task;
  },

  // Renames id.
  async update(accessToken: string, id: number, title: string): Promise<Task> {
    const response = await authedFetch(accessToken, `/api/tasks/${id}`, {
      method: "PATCH",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ title }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Task;
  },

  // Marks id completed.
  async complete(accessToken: string, id: number): Promise<Task> {
    const response = await authedFetch(accessToken, `/api/tasks/${id}/complete`, {
      method: "PUT",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Task;
  },

  // Un-marks id completed.
  async uncomplete(accessToken: string, id: number): Promise<Task> {
    const response = await authedFetch(accessToken, `/api/tasks/${id}/complete`, {
      method: "DELETE",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as Task;
  },

  // Deletes id outright.
  async remove(accessToken: string, id: number): Promise<void> {
    const response = await authedFetch(accessToken, `/api/tasks/${id}`, {
      method: "DELETE",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
