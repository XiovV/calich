import { Dialog } from "@base-ui/react/dialog";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";
import { useAsyncAction } from "../hooks/useAsyncAction";
import type { Account } from "../lib/accountsApi";

interface CancelInviteDialogProps {
  account: Account;
  onCancel: () => Promise<void>;
  onClose: () => void;
}

// Cancels a Pending invite by deleting the row outright (ADR-0042) — a
// Pending account owns nothing, so unlike DeleteAccountDialog there is no
// disposition to choose.
export function CancelInviteDialog({ account, onCancel, onClose }: CancelInviteDialogProps) {
  const { isSubmitting, error, run } = useAsyncAction();

  async function handleConfirm() {
    await run(async () => {
      await onCancel();
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
          <Dialog.Title className="text-heading font-medium text-ink">
            Cancel {account.username}'s invite?
          </Dialog.Title>
          <Dialog.Description className="mt-2 text-body text-ink-muted">
            This deletes the pending account. The username becomes available again.
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
              Keep invite
            </Dialog.Close>
            <Button color="danger" size="small" loading={isSubmitting} onClick={handleConfirm}>
              Cancel invite
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
