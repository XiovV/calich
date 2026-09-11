import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./tasksApi", () => ({
  tasksApi: {
    list: vi.fn(),
    listCompleted: vi.fn(),
    create: vi.fn(),
    updateNotes: vi.fn(),
    setDeadline: vi.fn(),
    clearDeadline: vi.fn(),
    setTimeBlock: vi.fn(),
    updatePriority: vi.fn(),
    move: vi.fn(),
    complete: vi.fn(),
    uncomplete: vi.fn(),
    remove: vi.fn(),
  },
}));

vi.mock("./toast", () => ({
  toast: { error: vi.fn() },
}));

const { tasksApi } = await import("./tasksApi");
const { toast } = await import("./toast");
const { useAuthStore } = await import("./authStore");
const { useTasksStore, resolveQuickAddTaskListId } = await import("./tasksStore");

const inbox = { id: 1, name: "Inbox", color: "#12809CFF", isDefault: true };
const work = { id: 2, name: "Work", color: "#E2483DFF", isDefault: false };
const personal = { id: 3, name: "Personal", color: "#4A90D9FF", isDefault: false };

const buyMilk = {
  id: 10,
  taskListId: inbox.id,
  title: "Buy milk",
  notes: "",
  due: null,
  start: null,
  durationMinutes: null,
  priority: 0,
  completed: false,
  createdAt: new Date(2026, 0, 1),
};

function resetStore() {
  useTasksStore.setState({ tasks: [], completedTasks: [] });
  useAuthStore.setState({ accessToken: "token-123" });
}

beforeEach(() => {
  vi.clearAllMocks();
  resetStore();
});

describe("resolveQuickAddTaskListId", () => {
  it("targets the default task list when nothing is checked", () => {
    expect(resolveQuickAddTaskListId([inbox, work, personal], new Set())).toBe(inbox.id);
  });

  it("targets the default task list when more than one is checked", () => {
    expect(resolveQuickAddTaskListId([inbox, work, personal], new Set([work.id, personal.id]))).toBe(
      inbox.id,
    );
  });

  it("targets the single checked task list when exactly one is checked", () => {
    expect(resolveQuickAddTaskListId([inbox, work, personal], new Set([work.id]))).toBe(work.id);
  });

  it("ignores a stale checked id no longer present among the task lists", () => {
    expect(resolveQuickAddTaskListId([inbox, work], new Set([personal.id]))).toBe(inbox.id);
  });
});

describe("fetchTasks", () => {
  it("loads incomplete tasks from the API", async () => {
    vi.mocked(tasksApi.list).mockResolvedValue([buyMilk]);

    await useTasksStore.getState().fetchTasks();

    expect(useTasksStore.getState().tasks).toEqual([buyMilk]);
  });
});

describe("createTask", () => {
  it("appends the task once the API call resolves", async () => {
    vi.mocked(tasksApi.create).mockResolvedValue(buyMilk);

    const created = await useTasksStore.getState().createTask("Buy milk", inbox.id);

    expect(created).toEqual(buyMilk);
    expect(useTasksStore.getState().tasks).toEqual([buyMilk]);
    expect(tasksApi.create).toHaveBeenCalledWith("token-123", "Buy milk", inbox.id);
  });
});

describe("detail surface writes", () => {
  it("updateTaskNotes replaces a completed task in place, in completedTasks", async () => {
    const completed = { ...buyMilk, completed: true };
    useTasksStore.setState({ tasks: [], completedTasks: [completed] });
    const withNotes = { ...completed, notes: "2%, not whole" };
    vi.mocked(tasksApi.updateNotes).mockResolvedValue(withNotes);

    await useTasksStore.getState().updateTaskNotes(buyMilk.id, "2%, not whole");

    expect(useTasksStore.getState().completedTasks).toEqual([withNotes]);
    expect(useTasksStore.getState().tasks).toEqual([]);
  });

  it("setTaskDeadline and clearTaskDeadline replace the task with the server's response", async () => {
    useTasksStore.setState({ tasks: [buyMilk], completedTasks: [] });
    const due = new Date(2026, 8, 15);
    const withDeadline = { ...buyMilk, due };
    vi.mocked(tasksApi.setDeadline).mockResolvedValue(withDeadline);

    await useTasksStore.getState().setTaskDeadline(buyMilk.id, due);
    expect(useTasksStore.getState().tasks).toEqual([withDeadline]);

    vi.mocked(tasksApi.clearDeadline).mockResolvedValue(buyMilk);
    await useTasksStore.getState().clearTaskDeadline(buyMilk.id);
    expect(useTasksStore.getState().tasks).toEqual([buyMilk]);
  });

  it("updateTaskPriority replaces the task with the server's raw value", async () => {
    useTasksStore.setState({ tasks: [buyMilk], completedTasks: [] });
    const prioritized = { ...buyMilk, priority: 1 };
    vi.mocked(tasksApi.updatePriority).mockResolvedValue(prioritized);

    await useTasksStore.getState().updateTaskPriority(buyMilk.id, 1);

    expect(useTasksStore.getState().tasks).toEqual([prioritized]);
  });

  it("moveTask replaces the task's taskListId", async () => {
    useTasksStore.setState({ tasks: [buyMilk], completedTasks: [] });
    const moved = { ...buyMilk, taskListId: work.id };
    vi.mocked(tasksApi.move).mockResolvedValue(moved);

    await useTasksStore.getState().moveTask(buyMilk.id, work.id);

    expect(useTasksStore.getState().tasks).toEqual([moved]);
  });

  it("deleteTask removes the task from whichever list holds it", async () => {
    useTasksStore.setState({ tasks: [buyMilk], completedTasks: [] });
    vi.mocked(tasksApi.remove).mockResolvedValue(undefined);

    await useTasksStore.getState().deleteTask(buyMilk.id);

    expect(useTasksStore.getState().tasks).toEqual([]);
  });
});

describe("setTaskCompleted", () => {
  it("moves the task to completedTasks immediately, before the API call resolves", () => {
    useTasksStore.setState({ tasks: [buyMilk], completedTasks: [] });
    let resolveComplete: () => void = () => {};
    vi.mocked(tasksApi.complete).mockReturnValue(
      new Promise((resolve) => {
        resolveComplete = () => resolve({ ...buyMilk, completed: true });
      }),
    );

    const promise = useTasksStore.getState().setTaskCompleted(buyMilk.id, true);

    expect(useTasksStore.getState().tasks).toEqual([]);
    expect(useTasksStore.getState().completedTasks).toEqual([{ ...buyMilk, completed: true }]);

    resolveComplete();
    return promise;
  });

  it("rolls back and shows a toast when completing fails", async () => {
    useTasksStore.setState({ tasks: [buyMilk], completedTasks: [] });
    vi.mocked(tasksApi.complete).mockRejectedValue(new Error("network error"));

    const ok = await useTasksStore.getState().setTaskCompleted(buyMilk.id, true);

    expect(ok).toBe(false);
    expect(useTasksStore.getState().tasks).toEqual([buyMilk]);
    expect(useTasksStore.getState().completedTasks).toEqual([]);
    expect(toast.error).toHaveBeenCalledWith("Couldn't complete the task.");
  });

  it("moves the task back to tasks immediately when un-completing", () => {
    const completed = { ...buyMilk, completed: true };
    useTasksStore.setState({ tasks: [], completedTasks: [completed] });
    let resolveUncomplete: () => void = () => {};
    vi.mocked(tasksApi.uncomplete).mockReturnValue(
      new Promise((resolve) => {
        resolveUncomplete = () => resolve(buyMilk);
      }),
    );

    const promise = useTasksStore.getState().setTaskCompleted(buyMilk.id, false);

    expect(useTasksStore.getState().completedTasks).toEqual([]);
    expect(useTasksStore.getState().tasks).toEqual([{ ...completed, completed: false }]);

    resolveUncomplete();
    return promise;
  });

  it("rolls back and shows a toast when un-completing fails", async () => {
    const completed = { ...buyMilk, completed: true };
    useTasksStore.setState({ tasks: [], completedTasks: [completed] });
    vi.mocked(tasksApi.uncomplete).mockRejectedValue(new Error("network error"));

    const ok = await useTasksStore.getState().setTaskCompleted(buyMilk.id, false);

    expect(ok).toBe(false);
    expect(useTasksStore.getState().completedTasks).toEqual([completed]);
    expect(useTasksStore.getState().tasks).toEqual([]);
    expect(toast.error).toHaveBeenCalledWith("Couldn't reopen the task.");
  });

  it("resolves false and touches nothing when the task isn't in either list", async () => {
    useTasksStore.setState({ tasks: [], completedTasks: [] });

    const ok = await useTasksStore.getState().setTaskCompleted(999, true);

    expect(ok).toBe(false);
    expect(tasksApi.complete).not.toHaveBeenCalled();
  });
});

// setTaskTimeBlock covers #313: the grid drop's own write, optimistic like
// setTaskCompleted (ADR-0067) since the block appearing on the grid is the
// feedback — and, in every case, leaving the Deadline exactly as it was, the
// two axes being independent (ADR-0083).
describe("setTaskTimeBlock", () => {
  const dueDated = { ...buyMilk, due: new Date(2026, 8, 20) };

  it("paints the Time block immediately, before the API call resolves, leaving the Deadline untouched", () => {
    useTasksStore.setState({ tasks: [dueDated], completedTasks: [] });
    const start = new Date(2026, 8, 15, 14, 0);
    let resolveSet: () => void = () => {};
    vi.mocked(tasksApi.setTimeBlock).mockReturnValue(
      new Promise((resolve) => {
        resolveSet = () => resolve({ ...dueDated, start, durationMinutes: 60 });
      }),
    );

    const promise = useTasksStore.getState().setTaskTimeBlock(dueDated.id, start, 60);

    expect(useTasksStore.getState().tasks).toEqual([{ ...dueDated, start, durationMinutes: 60 }]);
    expect(tasksApi.setTimeBlock).toHaveBeenCalledWith("token-123", dueDated.id, start, 60);

    resolveSet();
    return promise;
  });

  it("rolls back and shows a toast when the server refuses, leaving the Deadline untouched", async () => {
    useTasksStore.setState({ tasks: [dueDated], completedTasks: [] });
    const start = new Date(2026, 8, 15, 14, 0);
    vi.mocked(tasksApi.setTimeBlock).mockRejectedValue(new Error("network error"));

    const ok = await useTasksStore.getState().setTaskTimeBlock(dueDated.id, start, 60);

    expect(ok).toBe(false);
    expect(useTasksStore.getState().tasks).toEqual([dueDated]);
    expect(toast.error).toHaveBeenCalledWith(`Couldn't schedule "${dueDated.title}".`);
  });

  it("resolves false and touches nothing when the task isn't in either list", async () => {
    useTasksStore.setState({ tasks: [], completedTasks: [] });

    const ok = await useTasksStore.getState().setTaskTimeBlock(999, new Date(), 60);

    expect(ok).toBe(false);
    expect(tasksApi.setTimeBlock).not.toHaveBeenCalled();
  });
});
