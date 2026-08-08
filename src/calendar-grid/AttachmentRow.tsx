import { Paperclip, RotateCw, X } from "lucide-react";
import type { Attachment } from "../lib/event";
import { formatBytes } from "../lib/formatBytes";
import { IconButton } from "../components/ui/IconButton";

/** One Attachment row's state (#132, ADR-0040): already uploaded (server
 * data), mid-upload (a real percentage, not a spinner — XHR-driven), or
 * failed (the original File retained, so retry never re-prompts the file
 * picker). draftId is stable across re-renders even before the server has
 * minted a real id. */
export type AttachmentDraft =
  | { draftId: string; status: "uploaded"; attachment: Attachment }
  | {
      draftId: string;
      status: "uploading";
      filename: string;
      sizeBytes: number;
      progress: number;
    }
  | {
      draftId: string;
      status: "error";
      filename: string;
      sizeBytes: number;
      file: File;
      message: string;
    }
  | {
      // A file picked/dropped while creating a new Event, held in memory
      // until the create POST succeeds — there is no Event id to upload
      // against yet (#132, ADR-0040).
      draftId: string;
      status: "pending";
      filename: string;
      sizeBytes: number;
      file: File;
    };

interface AttachmentRowProps {
  draft: AttachmentDraft;
  /** Names the uploader on a shared Calendar, mirroring how an Event shows
   * its creator (#118). */
  showUploader: boolean;
  onDownload: () => void;
  onRemove: () => void;
  onRetry: () => void;
  // A read-only Attachment list still lets a Viewer/Subscribed-Calendar
  // caller download, just not add/remove (#132, ADR-0040).
  disabled?: boolean;
}

function draftFilename(draft: AttachmentDraft): string {
  return draft.status === "uploaded" ? draft.attachment.filename : draft.filename;
}

function draftSizeBytes(draft: AttachmentDraft): number {
  return draft.status === "uploaded" ? draft.attachment.sizeBytes : draft.sizeBytes;
}

export function AttachmentRow({
  draft,
  showUploader,
  onDownload,
  onRemove,
  onRetry,
  disabled,
}: AttachmentRowProps) {
  const uploaderUsername =
    draft.status === "uploaded" ? draft.attachment.uploadedByUsername : undefined;

  return (
    <div className="flex items-center gap-2">
      <Paperclip className="size-4 shrink-0 text-ink-muted" />
      <div className="min-w-0 flex-1">
        {draft.status === "uploaded" ? (
          <button
            type="button"
            onClick={onDownload}
            className="block truncate text-left text-label-sm text-ink hover:underline"
            title={draftFilename(draft)}
          >
            {draftFilename(draft)}
          </button>
        ) : (
          <p className="truncate text-label-sm text-ink" title={draftFilename(draft)}>
            {draftFilename(draft)}
          </p>
        )}
        <p className="text-label-sm text-ink-muted">
          {formatBytes(draftSizeBytes(draft))}
          {showUploader && uploaderUsername ? ` · Added by ${uploaderUsername}` : null}
          {draft.status === "uploading" ? ` · Uploading… ${Math.round(draft.progress * 100)}%` : null}
          {draft.status === "error" ? ` · ${draft.message}` : null}
        </p>
      </div>
      {draft.status === "error" && !disabled && (
        <IconButton aria-label="Retry upload" onClick={onRetry}>
          <RotateCw className="size-4" />
        </IconButton>
      )}
      {draft.status !== "uploading" && !disabled && (
        <IconButton aria-label="Remove attachment" onClick={onRemove}>
          <X className="size-4" />
        </IconButton>
      )}
    </div>
  );
}
