import { Dialog } from "@base-ui/react/dialog";
import { Button } from "../ui/Button";
import { buttonClasses } from "../ui/buttonClasses";
import { useAsyncAction } from "../../hooks/useAsyncAction";
import { useBookingLinksStore } from "../../lib/bookingLinksStore";
import type { BookingLink } from "../../lib/bookingLinksApi";

interface DeleteBookingLinkConfirmationProps {
  bookingLink: BookingLink;
  onClose: () => void;
}

// Deletes a Booking Link outright (#322). Nothing is public yet (that
// arrives with #324), so there is no visitor-facing consequence to warn
// about — just the ordinary "this cannot be undone".
export function DeleteBookingLinkConfirmation({ bookingLink, onClose }: DeleteBookingLinkConfirmationProps) {
  const deleteBookingLink = useBookingLinksStore((state) => state.deleteBookingLink);
  const { isSubmitting, error, run } = useAsyncAction();

  async function handleConfirm() {
    await run(async () => {
      await deleteBookingLink(bookingLink.id);
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
          <Dialog.Title className="text-heading font-medium text-ink">Delete {bookingLink.title}?</Dialog.Title>
          <Dialog.Description className="mt-2 text-body text-ink-muted">
            This removes the booking link. This cannot be undone.
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
              Delete link
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
