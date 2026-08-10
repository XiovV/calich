import { useEffect, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { UserMinus, UserPlus } from "lucide-react";
import { IconButton } from "../components/ui/IconButton";
import { buttonClasses } from "../components/ui/buttonClasses";
import { useGroupsStore } from "../lib/groupsStore";
import { useWorkspaceMembersStore } from "../lib/workspaceMembersStore";
import type { Group } from "../lib/groupsApi";
import { errorMessage } from "../lib/errorMessage";

interface GroupMembersDialogProps {
  group: Group;
  onClose: () => void;
}

// The per-Group membership editor (#167): every Member of the active
// Workspace, toggled in or out of this Group — a Group's membership can only
// ever be drawn from its own Workspace's Members (ADR-0045), so this picks
// from workspaceMembersStore's already-loaded list rather than any wider
// directory.
export function GroupMembersDialog({ group, onClose }: GroupMembersDialogProps) {
  const workspaceMembers = useWorkspaceMembersStore((state) => state.members);
  const groupMembers = useGroupsStore((state) => state.membersByGroupId[group.id]);
  const fetchMembers = useGroupsStore((state) => state.fetchMembers);
  const addMember = useGroupsStore((state) => state.addMember);
  const removeMember = useGroupsStore((state) => state.removeMember);

  const [loadError, setLoadError] = useState<string | null>(null);
  const [busyUserId, setBusyUserId] = useState<number | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  useEffect(() => {
    fetchMembers(group.id).catch((err) => setLoadError(errorMessage(err)));
  }, [fetchMembers, group.id]);

  const memberUserIds = new Set((groupMembers ?? []).map((m) => m.userId));

  async function handleToggle(userId: number, isInGroup: boolean) {
    setBusyUserId(userId);
    setActionError(null);
    try {
      if (isInGroup) {
        await removeMember(group.id, userId);
      } else {
        await addMember(group.id, userId);
      }
    } catch (err) {
      setActionError(errorMessage(err));
    } finally {
      setBusyUserId(null);
    }
  }

  return (
    <Dialog.Root open onOpenChange={(open) => !open && onClose()}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-ink/20" />
        <Dialog.Popup className="fixed top-1/2 left-1/2 z-50 w-96 max-h-[85vh] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <Dialog.Title className="text-heading font-medium text-ink">{group.name} members</Dialog.Title>
          <Dialog.Description className="mt-1 text-body text-ink-muted">
            Choose which workspace members belong to this group.
          </Dialog.Description>

          {loadError && (
            <p className="mt-2 text-label-sm text-danger" role="alert">
              {loadError}
            </p>
          )}
          {actionError && (
            <p className="mt-2 text-label-sm text-danger" role="alert">
              {actionError}
            </p>
          )}

          <ul className="mt-4 flex flex-col gap-2">
            {workspaceMembers.map((member) => {
              const isInGroup = memberUserIds.has(member.userId);
              return (
                <li
                  key={member.userId}
                  className="flex items-center justify-between rounded-md border border-border px-3 py-2"
                >
                  <span className="text-body text-ink">{member.username}</span>
                  <IconButton
                    onClick={() => handleToggle(member.userId, isInGroup)}
                    disabled={busyUserId === member.userId || groupMembers === undefined}
                    aria-label={isInGroup ? `Remove ${member.username} from ${group.name}` : `Add ${member.username} to ${group.name}`}
                  >
                    {isInGroup ? <UserMinus className="size-4" /> : <UserPlus className="size-4" />}
                  </IconButton>
                </li>
              );
            })}

            {workspaceMembers.length === 0 && (
              <p className="text-label-sm text-ink-muted">No other workspace members yet.</p>
            )}
          </ul>

          <div className="mt-5 flex justify-end">
            <Dialog.Close className={buttonClasses({ variant: "outline", color: "secondary", size: "small" })}>
              Done
            </Dialog.Close>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
