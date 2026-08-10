import { create } from "zustand";
import { useAuthStore } from "./authStore";
import {
  type CalendarDispositionChoice,
  type RemoveMemberImpact,
  type WorkspaceMember,
  workspaceMembersApi,
} from "./workspaceMembersApi";
import {
  type CreatedWorkspaceInvite,
  type OutstandingWorkspaceInvite,
  workspaceInvitesApi,
} from "./workspaceInvitesApi";

// The Workspace member-management screen's data source (#165): active
// Members alongside outstanding Invites, both scoped to whichever Workspace
// the switcher (#153) currently has active.
interface WorkspaceMembersState {
  members: WorkspaceMember[];
  invites: OutstandingWorkspaceInvite[];
  fetchAll: () => Promise<void>;
  setRole: (userId: number, role: "admin" | "member") => Promise<void>;
  removeImpact: (userId: number) => Promise<RemoveMemberImpact>;
  removeMember: (userId: number, calendars: CalendarDispositionChoice[]) => Promise<void>;
  createInvite: (email: string) => Promise<CreatedWorkspaceInvite>;
  reissueInvite: (inviteId: number) => Promise<CreatedWorkspaceInvite>;
  cancelInvite: (inviteId: number) => Promise<void>;
}

function requireAccessToken(): string {
  const accessToken = useAuthStore.getState().accessToken;
  if (!accessToken) throw new Error("Not authenticated.");
  return accessToken;
}

export const useWorkspaceMembersStore = create<WorkspaceMembersState>((set, get) => ({
  members: [],
  invites: [],

  fetchAll: async () => {
    const accessToken = requireAccessToken();
    const [members, invites] = await Promise.all([
      workspaceMembersApi.list(accessToken),
      workspaceInvitesApi.list(accessToken),
    ]);
    set({ members, invites });
  },

  setRole: async (userId, role) => {
    const updated = await workspaceMembersApi.setRole(requireAccessToken(), userId, role);
    set({
      members: get().members.map((m) => (m.userId === userId ? { ...m, role: updated.role } : m)),
    });
  },

  removeImpact: (userId) => workspaceMembersApi.removeImpact(requireAccessToken(), userId),

  removeMember: async (userId, calendars) => {
    await workspaceMembersApi.remove(requireAccessToken(), userId, calendars);
    set({ members: get().members.filter((m) => m.userId !== userId) });
  },

  createInvite: async (email) => {
    const created = await workspaceInvitesApi.create(requireAccessToken(), email);
    set({ invites: [...get().invites, created] });
    return created;
  },

  reissueInvite: async (inviteId) => {
    return workspaceInvitesApi.reissue(requireAccessToken(), inviteId);
  },

  cancelInvite: async (inviteId) => {
    await workspaceInvitesApi.cancel(requireAccessToken(), inviteId);
    set({ invites: get().invites.filter((i) => i.id !== inviteId) });
  },
}));
