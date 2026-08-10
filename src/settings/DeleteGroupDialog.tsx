import { Dialog } from "@base-ui/react/dialog";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { useGroupsStore } from "../lib/groupsStore";
import type { Group } from "../lib/groupsApi";

interface DeleteGroupDialogProps {
  group: Group;
  onClose: () => void;
}

// Deletes a Group outright (#167) — its Members lose whatever Access this
// Group granted them on any Calendar it was shared with, but nothing about
// the Workspace itself changes.
export function DeleteGroupDialog({ group, onClose }: DeleteGroupDialogProps) {
  const deleteGroup = useGroupsStore((state) => state.deleteGroup);
  const { isSubmitting, error, run } = useAsyncAction();

  async function handleConfirm() {
    await run(async () => {
      await deleteGroup(group.id);
      onClose();
    });
  }

  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open && !isSubmitting) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-ink/20" />
        <Dialog.Popup className="fixed top-1/2 left-1/2 z-50 w-80 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <Dialog.Title className="text-heading font-medium text-ink">Delete {group.name}?</Dialog.Title>
          <Dialog.Description className="mt-2 text-body text-ink-muted">
            This removes the group and any Calendar shares granted to it. It cannot be undone.
          </Dialog.Description>

          {error && (
            <p className="mt-2 text-label-sm text-danger" role="alert">
              {error}
            </p>
          )}

          <div className="mt-5 flex justify-end gap-2">
            <Dialog.Close
              className={buttonClasses({ variant: "outline", color: "secondary", size: "small" })}
              disabled={isSubmitting}
            >
              Cancel
            </Dialog.Close>
            <Button color="danger" size="small" loading={isSubmitting} onClick={handleConfirm}>
              Delete group
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
