import { create } from "zustand";
import { useAuthStore } from "./authStore";
import { useShellStore } from "./shellStore";
import { workspacesApi, type Workspace } from "./workspacesApi";

interface WorkspacesState {
  workspaces: Workspace[];
  activeWorkspaceId: number | null;
  fetchWorkspaces: () => Promise<void>;
  setActiveWorkspaceId: (id: number) => void;
}

function requireAccessToken(): string {
  const accessToken = useAuthStore.getState().accessToken;
  if (!accessToken) throw new Error("Not authenticated.");
  return accessToken;
}

// requireActiveWorkspaceId synchronously reads activeWorkspaceId outside of
// React, throwing if it's missing — a caller with no active Workspace is a
// caller bug, not a recoverable state (mirrors requireAccessToken above).
export function requireActiveWorkspaceId(): number {
  const activeWorkspaceId = useWorkspacesStore.getState().activeWorkspaceId;
  if (activeWorkspaceId === null) throw new Error("No active workspace.");
  return activeWorkspaceId;
}

// A minimal workspace switcher (#153): every User currently belongs to
// exactly one Workspace, so there is nothing to persist about which is
// "active" beyond this session — later tickets adding ways to join more than
// one Workspace are what make switching meaningfully change what's visible.
export const useWorkspacesStore = create<WorkspacesState>((set, get) => ({
  workspaces: [],
  activeWorkspaceId: null,

  fetchWorkspaces: async () => {
    const workspaces = await workspacesApi.list(requireAccessToken());
    set((state) => ({
      workspaces,
      activeWorkspaceId: state.activeWorkspaceId ?? workspaces[0]?.id ?? null,
    }));
  },

  setActiveWorkspaceId: (id) => {
    if (!get().workspaces.some((workspace) => workspace.id === id)) return;
    set({ activeWorkspaceId: id });
    // A Calendar Set belongs to one Workspace (ADR-0082): its id could
    // coincide with an unrelated Set's in the one just entered, or point at
    // nothing there at all. Every Workspace switch lands back on "All
    // calendars" rather than risk looking through either (#304).
    useShellStore.getState().setActiveCalendarSetId(null);
  },
}));
