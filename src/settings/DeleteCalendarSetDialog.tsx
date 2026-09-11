import { Dialog } from "@base-ui/react/dialog";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { useCalendarSetsStore } from "../lib/calendarSetsStore";
import type { CalendarSet } from "../lib/calendarSetsApi";

interface DeleteCalendarSetDialogProps {
  calendarSet: CalendarSet;
  onClose: () => void;
}

// Deletes a Calendar Set outright (#301, ADR-0082). Destroys no Calendar and
// no Event — tidying a private view is never a destructive act.
export function DeleteCalendarSetDialog({ calendarSet, onClose }: DeleteCalendarSetDialogProps) {
  const deleteCalendarSet = useCalendarSetsStore((state) => state.deleteCalendarSet);
  const { isSubmitting, error, run } = useAsyncAction();

  async function handleConfirm() {
    await run(async () => {
      await deleteCalendarSet(calendarSet.id);
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
          <Dialog.Title className="text-heading font-medium text-ink">Delete {calendarSet.name}?</Dialog.Title>
          <Dialog.Description className="mt-2 text-body text-ink-muted">
            This removes the calendar set. No calendar and no event is deleted, and this cannot be undone.
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
              Delete set
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
