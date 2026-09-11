import { authedFetch, errorFromResponse } from "./apiClient";
import { workspaceHeaders } from "./workspaceHeaders";

// Tasks (#310, #311, ADR-0083): a unit of work a User tracks, belonging to
// exactly one Task List and private to its owner. Scoped to the active
// Workspace via workspaceHeaders, same as every other Workspace-scoped
// call. Due/start/createdAt are real UTC instants on the wire (never
// date-only strings the way an all-day Event's are, ADR-0017) — a Task's
// Deadline and Time block carry no `tzid` of their own (ADR-0083), so they
// are "floating" in exactly the sense a tzid-less Event is: reinterpreted
// through whichever Viewer zone reads them, per taskScheduling.ts.

interface TaskWire {
  id: number;
  taskListId: number;
  title: string;
  notes: string;
  due: string | null;
  start: string | null;
  durationMinutes: number | null;
  priority: number;
  completed: boolean;
  createdAt: string;
}

export interface Task {
  id: number;
  taskListId: number;
  title: string;
  notes: string;
  due: Date | null;
  start: Date | null;
  durationMinutes: number | null;
  priority: number;
  completed: boolean;
  createdAt: Date;
}

function fromWire(wire: TaskWire): Task {
  return {
    id: wire.id,
    taskListId: wire.taskListId,
    title: wire.title,
    notes: wire.notes,
    due: wire.due ? new Date(wire.due) : null,
    start: wire.start ? new Date(wire.start) : null,
    durationMinutes: wire.durationMinutes,
    priority: wire.priority,
    completed: wire.completed,
    createdAt: new Date(wire.createdAt),
  };
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

    return ((await response.json()) as TaskWire[]).map(fromWire);
  },

  // The caller's most recently completed Tasks, bounded to a recent tail
  // rather than all history — fetched only when "Show completed" asks.
  async listCompleted(accessToken: string): Promise<Task[]> {
    const response = await authedFetch(accessToken, "/api/tasks/completed", {
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return ((await response.json()) as TaskWire[]).map(fromWire);
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

    return fromWire((await response.json()) as TaskWire);
  },

  // Changes id's notes — the detail surface's Notes field.
  async updateNotes(accessToken: string, id: number, notes: string): Promise<Task> {
    const response = await authedFetch(accessToken, `/api/tasks/${id}/notes`, {
      method: "PATCH",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ notes }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire((await response.json()) as TaskWire);
  },

  // Sets id's Deadline — the detail surface's Deadline field. Never touches
  // the Time block, the two axes being independent (ADR-0083).
  async setDeadline(accessToken: string, id: number, due: Date): Promise<Task> {
    const response = await authedFetch(accessToken, `/api/tasks/${id}/deadline`, {
      method: "PUT",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ due: due.toISOString() }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire((await response.json()) as TaskWire);
  },

  // Clears id's Deadline.
  async clearDeadline(accessToken: string, id: number): Promise<Task> {
    const response = await authedFetch(accessToken, `/api/tasks/${id}/deadline`, {
      method: "DELETE",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire((await response.json()) as TaskWire);
  },

  // Changes id's raw PRIORITY value (0-9) — the detail surface's Priority
  // field, chosen from None/Low/Medium/High and translated by the caller
  // (taskPriority.ts), never sent as a level of its own.
  async updatePriority(accessToken: string, id: number, priority: number): Promise<Task> {
    const response = await authedFetch(accessToken, `/api/tasks/${id}/priority`, {
      method: "PATCH",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ priority }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire((await response.json()) as TaskWire);
  },

  // Moves id into taskListId — the detail surface's Task List field.
  async move(accessToken: string, id: number, taskListId: number): Promise<Task> {
    const response = await authedFetch(accessToken, `/api/tasks/${id}/task-list`, {
      method: "PUT",
      credentials: "include",
      headers: workspaceHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ taskListId }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire((await response.json()) as TaskWire);
  },

  // Marks id completed.
  async complete(accessToken: string, id: number): Promise<Task> {
    const response = await authedFetch(accessToken, `/api/tasks/${id}/complete`, {
      method: "PUT",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire((await response.json()) as TaskWire);
  },

  // Un-marks id completed.
  async uncomplete(accessToken: string, id: number): Promise<Task> {
    const response = await authedFetch(accessToken, `/api/tasks/${id}/complete`, {
      method: "DELETE",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);

    return fromWire((await response.json()) as TaskWire);
  },

  // Deletes id outright — the detail surface's delete action.
  async remove(accessToken: string, id: number): Promise<void> {
    const response = await authedFetch(accessToken, `/api/tasks/${id}`, {
      method: "DELETE",
      credentials: "include",
      headers: workspaceHeaders(),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
