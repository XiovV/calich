import { AlertDialog } from "@base-ui/react/alert-dialog";
import type { Calendar } from "../../lib/calendar";

interface DeleteCalendarConfirmationProps {
  calendar: Calendar;
  eventCount: number;
  onConfirm: () => void;
  onClose: () => void;
}

export function DeleteCalendarConfirmation({
  calendar,
  eventCount,
  onConfirm,
  onClose,
}: DeleteCalendarConfirmationProps) {
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
            Delete "{calendar.name}"?
          </AlertDialog.Title>
          <AlertDialog.Description className="mt-2 text-body text-ink-muted">
            {eventCount > 0
              ? `This will also delete ${eventCount} event${eventCount === 1 ? "" : "s"} on this calendar.`
              : "This calendar has no events."}
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
