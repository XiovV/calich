import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./taskListsApi", () => ({
  taskListsApi: {
    list: vi.fn(),
    create: vi.fn(),
    rename: vi.fn(),
    recolor: vi.fn(),
    setDefault: vi.fn(),
    remove: vi.fn(),
  },
}));

const { taskListsApi } = await import("./taskListsApi");
const { useTaskListsStore } = await import("./taskListsStore");
const { useAuthStore } = await import("./authStore");

const inbox = { id: 1, name: "Inbox", color: "#E2483DFF", isDefault: true };
const work = { id: 2, name: "Work", color: "#12809CFF", isDefault: false };

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ accessToken: "token-123" });
  useTaskListsStore.setState({ taskLists: [inbox, work] });
});

// ADR-0083: promoting a Task List to default is atomic on the backend
// (clear old, set new in one transaction) — the local state update mirrors
// that by clearing isDefault on every other list in the same set() call.
describe("setDefaultTaskList", () => {
  it("promotes the target and clears the flag on whichever list held it before", async () => {
    vi.mocked(taskListsApi.setDefault).mockResolvedValue({ ...work, isDefault: true });

    await useTaskListsStore.getState().setDefaultTaskList(work.id);

    const lists = useTaskListsStore.getState().taskLists;
    expect(lists.find((l) => l.id === work.id)?.isDefault).toBe(true);
    expect(lists.find((l) => l.id === inbox.id)?.isDefault).toBe(false);
  });
});

describe("deleteTaskList", () => {
  it("removes the deleted list from local state", async () => {
    vi.mocked(taskListsApi.remove).mockResolvedValue(undefined);

    await useTaskListsStore.getState().deleteTaskList(work.id);

    expect(useTaskListsStore.getState().taskLists.map((l) => l.id)).toEqual([inbox.id]);
  });
});
