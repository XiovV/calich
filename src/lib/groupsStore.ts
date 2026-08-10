import { create } from "zustand";
import { useAuthStore } from "./authStore";
import { type Group, type GroupMember, groupsApi } from "./groupsApi";

// The Groups management screen's data source (#167): every Group of the
// active Workspace, plus each Group's membership loaded on demand as the
// screen expands a Group to manage it. Mirrors workspaceMembersStore's
// shape, scoped to Groups instead of the Workspace's own membership.
interface GroupsState {
  groups: Group[];
  membersByGroupId: Record<number, GroupMember[]>;
  fetchGroups: () => Promise<void>;
  createGroup: (name: string) => Promise<Group>;
  renameGroup: (id: number, name: string) => Promise<void>;
  deleteGroup: (id: number) => Promise<void>;
  fetchMembers: (groupId: number) => Promise<void>;
  addMember: (groupId: number, userId: number) => Promise<void>;
  removeMember: (groupId: number, userId: number) => Promise<void>;
}

function requireAccessToken(): string {
  const accessToken = useAuthStore.getState().accessToken;
  if (!accessToken) throw new Error("Not authenticated.");
  return accessToken;
}

export const useGroupsStore = create<GroupsState>((set, get) => ({
  groups: [],
  membersByGroupId: {},

  fetchGroups: async () => {
    const groups = await groupsApi.list(requireAccessToken());
    set({ groups });
  },

  createGroup: async (name) => {
    const created = await groupsApi.create(requireAccessToken(), name);
    set({ groups: [...get().groups, created] });
    return created;
  },

  renameGroup: async (id, name) => {
    const renamed = await groupsApi.rename(requireAccessToken(), id, name);
    set({ groups: get().groups.map((g) => (g.id === id ? renamed : g)) });
  },

  deleteGroup: async (id) => {
    await groupsApi.remove(requireAccessToken(), id);
    const membersByGroupId = { ...get().membersByGroupId };
    delete membersByGroupId[id];
    set({ groups: get().groups.filter((g) => g.id !== id), membersByGroupId });
  },

  fetchMembers: async (groupId) => {
    const members = await groupsApi.listMembers(requireAccessToken(), groupId);
    set({ membersByGroupId: { ...get().membersByGroupId, [groupId]: members } });
  },

  addMember: async (groupId, userId) => {
    await groupsApi.addMember(requireAccessToken(), groupId, userId);
    const existing = get().membersByGroupId[groupId] ?? [];
    set({ membersByGroupId: { ...get().membersByGroupId, [groupId]: [...existing, { userId }] } });
  },

  removeMember: async (groupId, userId) => {
    await groupsApi.removeMember(requireAccessToken(), groupId, userId);
    const existing = get().membersByGroupId[groupId] ?? [];
    set({
      membersByGroupId: { ...get().membersByGroupId, [groupId]: existing.filter((m) => m.userId !== userId) },
    });
  },
}));
