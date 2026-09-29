import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// Same convention as CalendarList.test.tsx: the *Api module is mocked, the
// stores are real.
vi.mock("../../lib/taskListsApi", () => ({
  taskListsApi: {
    list: vi.fn(),
    create: vi.fn(),
    rename: vi.fn(),
    recolor: vi.fn(),
    setDefault: vi.fn(),
    remove: vi.fn(),
  },
}));

const { taskListsApi } = await import("../../lib/taskListsApi");
const { useAuthStore } = await import("../../lib/authStore");
const { useTaskListsStore } = await import("../../lib/taskListsStore");
const { useShellStore } = await import("../../lib/shellStore");
const { useWorkspacesStore } = await import("../../lib/workspacesStore");
const { TaskListsFilter } = await import("./TaskListsFilter");

const inbox = { id: 1, name: "Inbox", color: "#E2483DFF", isDefault: true };
const work = { id: 2, name: "Work", color: "#12809CFF", isDefault: false };

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ accessToken: "token-123" });
  useTaskListsStore.setState({ taskLists: [inbox, work] });
  useShellStore.setState({
    checkedTaskListIds: new Set([inbox.id, work.id]),
    knownTaskListIds: new Set([inbox.id, work.id]),
  });
  useWorkspacesStore.setState({ activeWorkspaceId: 7 });
  vi.mocked(taskListsApi.list).mockResolvedValue([inbox, work]);
});

async function openFilter() {
  await userEvent.click(screen.getByRole("button", { name: "Lists filter" }));
  await screen.findByRole("menu");
}

// #317, ADR-0083: the Lists filter shows every Task List with a colour
// checkbox and a checked-count, and offers "New list".
describe("TaskListsFilter", () => {
  it("shows every task list with its own checkbox, checked by default", async () => {
    render(<TaskListsFilter />);

    await openFilter();

    expect(screen.getByText("Inbox")).toBeInTheDocument();
    expect(screen.getByText("Work")).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Inbox" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Work" })).toBeChecked();
  });

  it("shows a checked count on the trigger that updates as boxes are toggled", async () => {
    render(<TaskListsFilter />);
    expect(screen.getByRole("button", { name: "Lists filter" })).toHaveTextContent("2 of 2");

    await openFilter();
    await userEvent.click(screen.getByRole("checkbox", { name: "Work" }));

    expect(screen.getByRole("button", { name: "Lists filter" })).toHaveTextContent("1 of 2");
    expect(screen.getByRole("checkbox", { name: "Work" })).not.toBeChecked();
  });

  it("creates a new list via New list without leaving the panel, and checks it automatically", async () => {
    vi.mocked(taskListsApi.create).mockResolvedValue({
      id: 3,
      name: "Errands",
      color: "#3F51B5FF",
      isDefault: false,
    });
    render(<TaskListsFilter />);

    await openFilter();
    // "New list" closes the dropdown (an ordinary Menu.Item click) and
    // reveals the name field in the panel itself, not inside the Popup — a
    // real text input fights a Menu's own Enter/Escape handling, so the
    // create field deliberately lives outside it. The panel itself is never
    // left throughout.
    await userEvent.click(screen.getByRole("menuitem", { name: "New list" }));
    const input = await screen.findByRole("textbox", { name: "New list name" });
    await userEvent.type(input, "Errands{Enter}");

    await waitFor(() => expect(taskListsApi.create).toHaveBeenCalled());
    expect(taskListsApi.create).toHaveBeenCalledWith("token-123", "Errands", expect.any(String));
    expect(useTaskListsStore.getState().taskLists.some((l) => l.name === "Errands")).toBe(true);
    expect(useShellStore.getState().checkedTaskListIds.has(3)).toBe(true);

    // Reopening the dropdown shows the newly created list alongside the
    // existing ones.
    await openFilter();
    expect(screen.getByText("Errands")).toBeInTheDocument();
  });

  // #317, ADR-0083: Task Lists are scoped to the active Workspace, so
  // switching Workspaces while the panel happens to be open must swap what
  // it shows rather than leaving the previous Workspace's Lists in view.
  it("refetches when the active workspace changes", async () => {
    const otherWorkspaceList = { id: 9, name: "Groceries", color: "#8E44ADFF", isDefault: true };
    render(<TaskListsFilter />);
    await waitFor(() => expect(taskListsApi.list).toHaveBeenCalledTimes(1));

    vi.mocked(taskListsApi.list).mockResolvedValue([otherWorkspaceList]);
    useWorkspacesStore.setState({ activeWorkspaceId: 42 });

    await waitFor(() => expect(taskListsApi.list).toHaveBeenCalledTimes(2));
    await openFilter();
    expect(screen.getByText("Groceries")).toBeInTheDocument();
    expect(screen.queryByText("Inbox")).not.toBeInTheDocument();
  });
});
