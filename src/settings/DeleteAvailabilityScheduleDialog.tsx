import { Dialog } from "@base-ui/react/dialog";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { useAvailabilitySchedulesStore } from "../lib/availabilitySchedulesStore";
import type { AvailabilitySchedule } from "../lib/availabilitySchedulesApi";

interface DeleteAvailabilityScheduleDialogProps {
  schedule: AvailabilitySchedule;
  onClose: () => void;
}

// Deletes an Availability Schedule outright (#320, ADR-0085). Destroys no
// Booking Link and no Event on its own — nothing in this codebase
// references a Schedule yet (#322 is the first thing that will).
export function DeleteAvailabilityScheduleDialog({ schedule, onClose }: DeleteAvailabilityScheduleDialogProps) {
  const deleteSchedule = useAvailabilitySchedulesStore((state) => state.deleteSchedule);
  const { isSubmitting, error, run } = useAsyncAction();

  async function handleConfirm() {
    await run(async () => {
      await deleteSchedule(schedule.id);
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
          <Dialog.Title className="text-heading font-medium text-ink">Delete {schedule.name}?</Dialog.Title>
          <Dialog.Description className="mt-2 text-body text-ink-muted">
            This removes the schedule. This cannot be undone.
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
              Delete schedule
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
