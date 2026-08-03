import { AlertDialog } from "@base-ui/react/alert-dialog";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";

interface DiscardRecurrenceWarningProps {
  onConfirm: () => void;
  onClose: () => void;
}

/**
 * Warns before a recurrence rule change discards existing per-Occurrence
 * edits and cancellations — changing/removing the rule is forced to "All
 * events" because Overrides/Exceptions are keyed by a `RECURRENCE-ID` the new
 * rule may no longer generate (ADR-0016).
 */
export function DiscardRecurrenceWarning({
  onConfirm,
  onClose,
}: DiscardRecurrenceWarningProps) {
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
            Discard event changes?
          </AlertDialog.Title>
          <AlertDialog.Description className="mt-2 text-body text-ink-muted">
            Changing how this event repeats applies to all events in the
            series and discards any changes made to individual events.
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
              Continue
            </Button>
          </div>
        </AlertDialog.Popup>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
