import { AlertDialog } from "@base-ui/react/alert-dialog";
import type { Task } from "../../lib/tasksApi";
import { Button } from "../ui/Button";
import { buttonClasses } from "../ui/buttonClasses";

interface DeleteTaskConfirmationProps {
  task: Task;
  onConfirm: () => void;
  onClose: () => void;
}

// DeleteTaskConfirmation is the detail surface's delete action (#311),
// mirroring DeleteEventConfirmation — a Task is outright removed, with no
// undo, so it gets the same one-question AlertDialog rather than the
// no-confirmation posture Completion uses (ADR-0068 is about a dialog's
// footer, not about whether a destructive action gets asked about at all).
export function DeleteTaskConfirmation({ task, onConfirm, onClose }: DeleteTaskConfirmationProps) {
  return (
    <AlertDialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <AlertDialog.Portal>
        <AlertDialog.Backdrop className="fixed inset-0 z-40 bg-ink/20" />
        <AlertDialog.Popup className="fixed top-1/2 left-1/2 z-50 w-80 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <AlertDialog.Title className="text-heading font-medium text-ink">
            Delete "{task.title}"?
          </AlertDialog.Title>
          <AlertDialog.Description className="mt-2 text-body text-ink-muted">
            This can't be undone.
          </AlertDialog.Description>

          <div className="mt-5 flex justify-end gap-2">
            <AlertDialog.Close
              className={buttonClasses({
                variant: "outline",
                color: "secondary",
                size: "small",
              })}
            >
              Cancel
            </AlertDialog.Close>
            <Button color="danger" size="small" onClick={onConfirm}>
              Delete
            </Button>
          </div>
        </AlertDialog.Popup>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
