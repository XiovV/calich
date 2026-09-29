import { beforeEach, expect, it, vi } from "vitest";

vi.mock("./taskListsApi", () => ({
  taskListsApi: { create: vi.fn() },
}));

const { taskListsApi } = await import("./taskListsApi");
const { useAuthStore } = await import("./authStore");
const { useTaskListsStore } = await import("./taskListsStore");
const { useShellStore } = await import("./shellStore");
const { createTaskListCascade } = await import("./createTaskListCascade");

const work = { id: 2, name: "Work", color: "#12809CFF", isDefault: false };

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ accessToken: "token-123" });
  useTaskListsStore.setState({ taskLists: [] });
  useShellStore.setState({
    checkedTaskListIds: new Set(),
    knownTaskListIds: new Set(),
  });
});

it("checks and marks the task list known once the create succeeds", async () => {
  vi.mocked(taskListsApi.create).mockResolvedValue(work);

  await createTaskListCascade("Work", "#12809CFF");

  expect(useShellStore.getState().checkedTaskListIds.has(work.id)).toBe(true);
  expect(useShellStore.getState().knownTaskListIds.has(work.id)).toBe(true);
});

it("checks nothing when the create fails", async () => {
  vi.mocked(taskListsApi.create).mockRejectedValue(new Error("network error"));

  await expect(createTaskListCascade("Work", "#12809CFF")).rejects.toThrow();

  expect(useShellStore.getState().checkedTaskListIds.size).toBe(0);
});

it("survives a later reconcile without re-checking a task list the caller deliberately unchecked", async () => {
  vi.mocked(taskListsApi.create).mockResolvedValue(work);

  await createTaskListCascade("Work", "#12809CFF");
  useShellStore.getState().toggleTaskListChecked(work.id);

  useShellStore.getState().reconcileCheckedTaskListIds([work.id]);

  expect(useShellStore.getState().checkedTaskListIds.has(work.id)).toBe(false);
});
