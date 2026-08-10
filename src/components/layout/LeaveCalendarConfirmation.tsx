import { AlertDialog } from "@base-ui/react/alert-dialog";
import type { Calendar } from "../../lib/calendar";
import { Button } from "../ui/Button";
import { buttonClasses } from "../ui/buttonClasses";

interface LeaveCalendarConfirmationProps {
  calendar: Calendar;
  onConfirm: () => void;
  onClose: () => void;
}

export function LeaveCalendarConfirmation({
  calendar,
  onConfirm,
  onClose,
}: LeaveCalendarConfirmationProps) {
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
            {`Leave "${calendar.name}"?`}
          </AlertDialog.Title>
          <AlertDialog.Description className="mt-2 text-body text-ink-muted">
            You will lose access to this calendar and its events.
            {calendar.ownerName
              ? ` ${calendar.ownerName} keeps the calendar as it is.`
              : ""}
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
              Leave
            </Button>
          </div>
        </AlertDialog.Popup>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
