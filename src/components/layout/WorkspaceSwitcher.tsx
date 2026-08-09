import { Select } from "../ui/Select";
import { useWorkspacesStore } from "../../lib/workspacesStore";

// A minimal Workspace switcher (#153, ADR-0044): every User currently
// belongs to exactly one Workspace, so this renders a single-option Select
// until later tickets add ways to join more than one. Hidden rather than
// disabled while empty, since a brand-new account's Workspace list is still
// loading when AppShell mounts this.
export function WorkspaceSwitcher() {
  const workspaces = useWorkspacesStore((state) => state.workspaces);
  const activeWorkspaceId = useWorkspacesStore((state) => state.activeWorkspaceId);
  const setActiveWorkspaceId = useWorkspacesStore((state) => state.setActiveWorkspaceId);

  if (workspaces.length === 0 || activeWorkspaceId === null) return null;

  return (
    <Select
      value={String(activeWorkspaceId)}
      onValueChange={(value) => setActiveWorkspaceId(Number(value))}
      options={workspaces.map((workspace) => ({
        value: String(workspace.id),
        label: workspace.name,
      }))}
      aria-label="Select workspace"
    />
  );
}
