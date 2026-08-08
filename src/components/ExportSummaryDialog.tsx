import { AlertDialog } from "@base-ui/react/alert-dialog";
import type { ExportSummary } from "../lib/icsApi";
import { formatBytes } from "../lib/formatBytes";
import { Button } from "./ui/Button";
import { buttonClasses } from "./ui/buttonClasses";

interface ExportSummaryDialogProps {
  summary: ExportSummary;
  isSubmitting?: boolean;
  onConfirm: () => void;
  onClose: () => void;
}

/**
 * The Export summary's pre-flight dialog (#134, ADR-0041): shown only when
 * a would-be export would have to omit at least one Attachment for being
 * too large to inline. Nothing oversized means this never mounts — the
 * download proceeds with no dialog and no new friction, which is the point
 * of a pre-flight rather than a post-hoc toast (ADR-0041's rationale
 * mirrors ADR-0030's Import summary: a human is watching a one-time act, so
 * the loss is disclosed while they can still act on it).
 */
export function ExportSummaryDialog({
  summary,
  isSubmitting,
  onConfirm,
  onClose,
}: ExportSummaryDialogProps) {
  const hiddenCount = summary.count - summary.oversized.length;

  return (
    <AlertDialog.Root
      open
      onOpenChange={(open) => {
        if (!open && !isSubmitting) onClose();
      }}
    >
      <AlertDialog.Portal>
        <AlertDialog.Backdrop className="fixed inset-0 z-40 bg-ink/20" />
        <AlertDialog.Popup className="fixed top-1/2 left-1/2 z-50 w-96 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <AlertDialog.Title className="text-heading font-medium text-ink">
            {summary.count === 1 ? "1 attachment won't be included" : `${summary.count} attachments won't be included`}
          </AlertDialog.Title>
          <AlertDialog.Description className="mt-2 text-body text-ink-muted">
            These files are too large to include in the downloaded file. Everything else exports
            as usual.
          </AlertDialog.Description>

          <ul className="mt-3 flex flex-col gap-1.5 text-label-sm text-ink-muted">
            {summary.oversized.map((attachment) => (
              <li
                key={`${attachment.eventId}-${attachment.filename}`}
                className="flex items-center justify-between gap-2"
              >
                <span className="truncate text-ink">{attachment.filename}</span>
                <span className="shrink-0">
                  {formatBytes(attachment.sizeBytes)} · {attachment.eventTitle}
                </span>
              </li>
            ))}
          </ul>
          {hiddenCount > 0 && (
            <p className="mt-1.5 text-label-sm text-ink-muted">
              and {hiddenCount} more
            </p>
          )}

          <div className="mt-5 flex justify-end gap-2">
            <AlertDialog.Close
              className={buttonClasses({ variant: "outline", color: "secondary", size: "small" })}
              disabled={isSubmitting}
            >
              Cancel
            </AlertDialog.Close>
            <Button size="small" loading={isSubmitting} onClick={onConfirm}>
              Continue
            </Button>
          </div>
        </AlertDialog.Popup>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
