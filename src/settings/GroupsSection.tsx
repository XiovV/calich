import { useEffect, useState } from "react";
import { Check, Pencil, Plus, Trash2, Users, X } from "lucide-react";
import { Button } from "../components/ui/Button";
import { IconButton } from "../components/ui/IconButton";
import { Input } from "../components/ui/Input";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { errorMessage } from "../lib/errorMessage";
import { useAuthStore } from "../lib/authStore";
import { useWorkspacesStore } from "../lib/workspacesStore";
import { useWorkspaceMembersStore } from "../lib/workspaceMembersStore";
import { useGroupsStore } from "../lib/groupsStore";
import type { Group } from "../lib/groupsApi";
import { DeleteGroupDialog } from "./DeleteGroupDialog";
import { GroupMembersDialog } from "./GroupMembersDialog";

// The Groups management screen (#167, ADR-0045): the Owner or Admin creates,
// renames, and deletes Groups, and manages which Workspace Members belong to
// each — scoped to whichever Workspace the switcher (#153) currently has
// active. Reads the active Workspace's membership from
// workspaceMembersStore, the same source MembersSection uses, so the
// membership dialog's picker always reflects that same Workspace.
export function GroupsSection() {
  const user = useAuthStore((state) => state.user);
  const activeWorkspaceId = useWorkspacesStore((state) => state.activeWorkspaceId);

  const workspaceMembers = useWorkspaceMembersStore((state) => state.members);
  const fetchWorkspaceMembers = useWorkspaceMembersStore((state) => state.fetchAll);

  const groups = useGroupsStore((state) => state.groups);
  const fetchGroups = useGroupsStore((state) => state.fetchGroups);
  const createGroup = useGroupsStore((state) => state.createGroup);
  const renameGroup = useGroupsStore((state) => state.renameGroup);

  const [loadError, setLoadError] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [editingId, setEditingId] = useState<number | null>(null);
  const [editingName, setEditingName] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<Group | null>(null);
  const [membersTarget, setMembersTarget] = useState<Group | null>(null);
  const { isSubmitting: isCreating, error: createError, run: runCreate } = useAsyncAction();
  const { isSubmitting: isRenaming, error: renameError, setError: setRenameError, run: runRename } = useAsyncAction();

  useEffect(() => {
    if (activeWorkspaceId === null) return;
    Promise.all([fetchGroups(), fetchWorkspaceMembers()])
      .then(() => setLoadError(null))
      .catch((err) => setLoadError(errorMessage(err)));
  }, [activeWorkspaceId, fetchGroups, fetchWorkspaceMembers]);

  const self = workspaceMembers.find((m) => m.userId === user?.id) ?? null;
  const canManage = self?.role === "owner" || self?.role === "admin";

  async function handleCreate(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!name.trim()) return;

    await runCreate(async () => {
      await createGroup(name.trim());
      setName("");
    });
  }

  function startEditing(group: Group) {
    setEditingId(group.id);
    setEditingName(group.name);
    setRenameError(null);
  }

  async function handleRename(group: Group) {
    const trimmed = editingName.trim();
    if (!trimmed || trimmed === group.name) {
      setEditingId(null);
      return;
    }

    await runRename(async () => {
      await renameGroup(group.id, trimmed);
      setEditingId(null);
    });
  }

  return (
    <section>
      <h2 className="text-heading font-medium text-ink">Groups</h2>
      <p className="mt-1 text-body text-ink-muted">
        Organize this workspace's members into named groups you can share calendars with.
      </p>

      {loadError && <p className="mt-2 text-label-sm text-danger">{loadError}</p>}

      {canManage && (
        <>
          <form onSubmit={handleCreate} className="mt-4 flex items-end gap-2">
            <Input
              label="New group"
              placeholder="Tech team"
              value={name}
              onChange={(domEvent) => setName(domEvent.target.value)}
              className="w-72"
            />
            <Button type="submit" disabled={!name.trim()} loading={isCreating} leadingIcon={<Plus className="size-4" />}>
              Create
            </Button>
          </form>

          {createError && <p className="mt-2 text-label-sm text-danger">{createError}</p>}
        </>
      )}

      <ul className="mt-4 flex flex-col gap-2">
        {groups.map((group) => {
          const isEditing = editingId === group.id;
          return (
            <li key={group.id} className="rounded-md border border-border px-3 py-2">
              <div className="flex items-center justify-between gap-2">
                {isEditing ? (
                  <Input
                    autoFocus
                    value={editingName}
                    onChange={(domEvent) => setEditingName(domEvent.target.value)}
                    onKeyDown={(domEvent) => {
                      if (domEvent.key === "Enter") handleRename(group);
                      if (domEvent.key === "Escape") setEditingId(null);
                    }}
                    className="flex-1"
                    aria-label={`Rename ${group.name}`}
                  />
                ) : (
                  <p className="text-body text-ink">{group.name}</p>
                )}

                <div className="flex items-center gap-1">
                  {canManage && isEditing && (
                    <>
                      <IconButton
                        onClick={() => handleRename(group)}
                        disabled={isRenaming}
                        aria-label={`Save ${group.name}`}
                      >
                        <Check className="size-4" />
                      </IconButton>
                      <IconButton onClick={() => setEditingId(null)} disabled={isRenaming} aria-label="Cancel rename">
                        <X className="size-4" />
                      </IconButton>
                    </>
                  )}
                  {canManage && !isEditing && (
                    <>
                      <IconButton onClick={() => setMembersTarget(group)} aria-label={`Manage ${group.name} members`}>
                        <Users className="size-4" />
                      </IconButton>
                      <IconButton onClick={() => startEditing(group)} aria-label={`Rename ${group.name}`}>
                        <Pencil className="size-4" />
                      </IconButton>
                      <IconButton onClick={() => setDeleteTarget(group)} aria-label={`Delete ${group.name}`}>
                        <Trash2 className="size-4" />
                      </IconButton>
                    </>
                  )}
                </div>
              </div>
              {isEditing && renameError && <p className="mt-2 text-label-sm text-danger">{renameError}</p>}
            </li>
          );
        })}

        {groups.length === 0 && !loadError && <p className="text-label-sm text-ink-muted">No groups yet.</p>}
      </ul>

      {deleteTarget && <DeleteGroupDialog group={deleteTarget} onClose={() => setDeleteTarget(null)} />}
      {membersTarget && <GroupMembersDialog group={membersTarget} onClose={() => setMembersTarget(null)} />}
    </section>
  );
}
