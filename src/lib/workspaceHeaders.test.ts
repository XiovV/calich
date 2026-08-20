import { afterEach, describe, expect, it } from "vitest";
import { workspaceHeaders } from "./workspaceHeaders";
import { useWorkspacesStore } from "./workspacesStore";

afterEach(() => {
  useWorkspacesStore.setState({ activeWorkspaceId: null });
});

describe("workspaceHeaders", () => {
  it("names the active workspace", () => {
    useWorkspacesStore.setState({ activeWorkspaceId: 7 });

    expect(workspaceHeaders()).toEqual({ "X-Workspace-Id": "7" });
  });

  it("merges extra headers alongside it", () => {
    useWorkspacesStore.setState({ activeWorkspaceId: 7 });

    expect(workspaceHeaders({ "Content-Type": "application/json" })).toEqual({
      "X-Workspace-Id": "7",
      "Content-Type": "application/json",
    });
  });

  // A caller with no active Workspace is a caller bug, not a recoverable
  // state — better a synchronous throw than a request the server refuses as
  // a non-Member (#225).
  it("throws rather than sending a request with no workspace", () => {
    expect(() => workspaceHeaders()).toThrow("No active workspace.");
  });
});
