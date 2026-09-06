import { TriangleAlert } from "lucide-react";

interface WriteBackErrorIndicatorProps {
  // The per-Event permanent-failure marker (#291, ADR-0075, ADR-0076) —
  // undefined/empty renders nothing. A human-readable reason once a queued
  // Write-back push has exhausted its retries and will never reach the
  // Provider on its own.
  reason?: string;
}

// WriteBackErrorIndicator is the grid's own signal that an edit here never
// reached the Provider (#291) — a small warning badge, mirroring
// AttachmentIndicator's own shape, shown beside a Month-view chip or an
// all-day block. Renders nothing while healthy.
export function WriteBackErrorIndicator({ reason }: WriteBackErrorIndicatorProps) {
  if (!reason) return null;
  const label = `Couldn't sync to Google: ${reason}`;
  return (
    <span title={label} className="ml-auto shrink-0">
      <TriangleAlert aria-label={label} className="size-3 text-danger" />
    </span>
  );
}
