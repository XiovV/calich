import { AlertDialog } from "@base-ui/react/alert-dialog";
import type { Calendar } from "../../lib/calendar";
import { Button } from "../ui/Button";
import { buttonClasses } from "../ui/buttonClasses";

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
  // A Subscribed Calendar's Events are a mirror of the feed, not data this
  // app owns — removing it un-mirrors the feed rather than deleting
  // anything, so the copy says so instead of reading as data loss (#83).
  const isSubscribed = Boolean(calendar.sourceUrl);

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
            {isSubscribed
              ? `Unsubscribe from "${calendar.name}"?`
              : `Delete "${calendar.name}"?`}
          </AlertDialog.Title>
          <AlertDialog.Description className="mt-2 text-body text-ink-muted">
            {isSubscribed
              ? `This stops mirroring the feed and removes its ${eventCount} event${eventCount === 1 ? "" : "s"} from this app — the source calendar is unaffected.`
              : eventCount > 0
                ? `This will also delete ${eventCount} event${eventCount === 1 ? "" : "s"} on this calendar.`
                : "This calendar has no events."}
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
              {isSubscribed ? "Unsubscribe" : "Delete"}
            </Button>
          </div>
        </AlertDialog.Popup>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
