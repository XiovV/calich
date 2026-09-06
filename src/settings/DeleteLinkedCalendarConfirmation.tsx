import { AlertDialog } from "@base-ui/react/alert-dialog";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";

interface DeleteLinkedCalendarConfirmationProps {
  name: string;
  shareCount: number;
  isDeleting: boolean;
  onConfirm: () => void;
  onClose: () => void;
}

// Unchecking a previously-imported calendar in the picker deletes the
// Linked Calendar (#295). Unlike a Subscribed Calendar — which is only ever
// a mirror of its feed — a Linked Calendar may carry per-User colours,
// Default reminders, Shares, and Events created here, so this names what is
// lost rather than reading as a harmless un-mirror. When other people hold a
// Share, it says so explicitly.
export function DeleteLinkedCalendarConfirmation({
  name,
  shareCount,
  isDeleting,
  onConfirm,
  onClose,
}: DeleteLinkedCalendarConfirmationProps) {
  return (
    <AlertDialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <AlertDialog.Portal>
        <AlertDialog.Backdrop className="fixed inset-0 z-[60] bg-ink/20" />
        <AlertDialog.Popup className="fixed top-1/2 left-1/2 z-[70] w-80 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <AlertDialog.Title className="text-heading font-medium text-ink">
            Remove &ldquo;{name}&rdquo;?
          </AlertDialog.Title>
          <AlertDialog.Description className="mt-2 text-body text-ink-muted">
            This deletes the calendar here, along with its events and anything set on it in
            this app — your colour, your default reminders, and any events you created here.
            It does not touch the calendar at Google.
          </AlertDialog.Description>
          {shareCount > 0 && (
            <p className="mt-2 text-body text-danger">
              It&apos;s shared with {shareCount} {shareCount === 1 ? "person" : "people"} — they
              will lose it from their sidebars too.
            </p>
          )}

          <div className="mt-5 flex justify-end gap-2">
            <AlertDialog.Close
              className={buttonClasses({ variant: "outline", color: "secondary", size: "small" })}
            >
              Cancel
            </AlertDialog.Close>
            <Button color="danger" size="small" onClick={onConfirm} loading={isDeleting}>
              Remove
            </Button>
          </div>
        </AlertDialog.Popup>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
