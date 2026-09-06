import { format } from "date-fns";
import { Paperclip, TriangleAlert } from "lucide-react";
import type { CalendarBlockStyle } from "../lib/calendarColors";
import { useTimePattern } from "../hooks/useTimePattern";

interface EventVisualProps {
  title: string;
  start: Date;
  end: Date;
  blockStyle: CalendarBlockStyle;
  isPast?: boolean;
  hasAttachments?: boolean;
  // The per-Event permanent-failure marker (#291, ADR-0075, ADR-0076) — an
  // edit here that exhausted its retries and will never reach the Provider
  // on its own. Shown as a badge rather than buried in Settings, since this
  // is the User's only chance to notice and act.
  writeBackError?: string;
}

export function EventVisual({
  title,
  start,
  end,
  blockStyle,
  isPast,
  hasAttachments,
  writeBackError,
}: EventVisualProps) {
  const timePattern = useTimePattern();
  const hasBadge = hasAttachments || Boolean(writeBackError);
  return (
    <div className="relative h-full w-full overflow-hidden rounded-shell-sm bg-surface">
      <div
        style={blockStyle}
        className={`h-full w-full px-1.5 py-1 text-left ${isPast ? "opacity-60" : ""}`}
      >
        <p
          className={`truncate text-label-sm font-medium ${hasBadge ? "pr-4" : ""}`}
        >
          {title}
        </p>
        <p className="truncate text-label-sm opacity-90">
          {format(start, timePattern)} – {format(end, timePattern)}
        </p>
      </div>
      <span className="absolute top-1 right-1 flex shrink-0 items-center gap-0.5">
        {writeBackError && (
          <span title={`Couldn't sync to Google: ${writeBackError}`}>
            <TriangleAlert
              aria-label={`Couldn't sync to Google: ${writeBackError}`}
              className="size-3 text-danger"
            />
          </span>
        )}
        {hasAttachments && <Paperclip className="size-3 opacity-90" />}
      </span>
    </div>
  );
}
