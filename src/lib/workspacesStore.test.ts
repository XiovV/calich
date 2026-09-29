import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./workspacesApi", async () => {
  const actual = await vi.importActual<typeof import("./workspacesApi")>("./workspacesApi");
  return {
    ...actual,
    workspacesApi: { list: vi.fn() },
  };
});

const { useWorkspacesStore } = await import("./workspacesStore");
const { useShellStore } = await import("./shellStore");

const workspaceA = { id: 1, name: "Acme", defaultSharePrivacy: "private" as const };
const workspaceB = { id: 2, name: "Beta", defaultSharePrivacy: "private" as const };

beforeEach(() => {
  vi.clearAllMocks();
  useWorkspacesStore.setState({ workspaces: [workspaceA, workspaceB], activeWorkspaceId: workspaceA.id });
  useShellStore.setState({ activeCalendarSetId: null });
});

// #304, ADR-0082: a Calendar Set belongs to one Workspace, so its id is
// meaningless — or coincidentally means something else — in any other one.
describe("setActiveWorkspaceId", () => {
  it("switches the active Workspace", () => {
    useWorkspacesStore.getState().setActiveWorkspaceId(workspaceB.id);

    expect(useWorkspacesStore.getState().activeWorkspaceId).toBe(workspaceB.id);
  });

  it("ignores an id that isn't in the caller's Workspace list", () => {
    useWorkspacesStore.getState().setActiveWorkspaceId(999);

    expect(useWorkspacesStore.getState().activeWorkspaceId).toBe(workspaceA.id);
  });

  it("resets the Active Calendar Set to All calendars", () => {
    useShellStore.setState({ activeCalendarSetId: 7 });

    useWorkspacesStore.getState().setActiveWorkspaceId(workspaceB.id);

    expect(useShellStore.getState().activeCalendarSetId).toBeNull();
  });

  it("leaves the Active Calendar Set alone when the switch is refused", () => {
    useShellStore.setState({ activeCalendarSetId: 7 });

    useWorkspacesStore.getState().setActiveWorkspaceId(999);

    expect(useShellStore.getState().activeCalendarSetId).toBe(7);
  });
});
