import { AlertDialog } from "@base-ui/react/alert-dialog";
import type { Event } from "../lib/event";

interface DeleteEventConfirmationProps {
  event: Event;
  onConfirm: () => void;
  onClose: () => void;
}

export function DeleteEventConfirmation({
  event,
  onConfirm,
  onClose,
}: DeleteEventConfirmationProps) {
  return (
    <AlertDialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <AlertDialog.Portal>
        <AlertDialog.Backdrop className="fixed inset-0 bg-ink/20" />
        <AlertDialog.Popup className="fixed top-1/2 left-1/2 w-80 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <AlertDialog.Title className="text-heading font-medium text-ink">
            Delete "{event.title}"?
          </AlertDialog.Title>
          <AlertDialog.Description className="mt-2 text-body text-ink-muted">
            This can't be undone.
          </AlertDialog.Description>

          <div className="mt-5 flex justify-end gap-2">
            <AlertDialog.Close className="rounded-shell-sm border border-border px-3 py-1.5 text-body text-ink hover:bg-surface-hover">
              Cancel
            </AlertDialog.Close>
            <button
              type="button"
              onClick={onConfirm}
              className="rounded-shell-sm bg-calendar-tomato px-3 py-1.5 text-body text-ink-inverse hover:opacity-90"
            >
              Delete
            </button>
          </div>
        </AlertDialog.Popup>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
