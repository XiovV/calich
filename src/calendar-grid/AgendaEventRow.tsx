import { Video } from "lucide-react";
import { agendaSecondLineText, type AgendaRow } from "../lib/agenda";
import { toOpaqueHex } from "../lib/calendarColors";

interface AgendaEventRowProps {
  row: AgendaRow;
  onClick: () => void;
}

// The row and Join are siblings, not nested — a <button> can't legally
// contain an <a>, so Join sits beside it, absolutely positioned over the
// same hover group, revealed only when a Conference URL is present (#330).
export function AgendaEventRow({ row, onClick }: AgendaEventRowProps) {
  return (
    <div className="group relative flex items-start rounded-shell-sm hover:bg-surface-hover">
      <button
        type="button"
        onClick={onClick}
        className={`flex min-w-0 flex-1 items-start gap-3 px-2 py-1.5 text-left ${row.dimmed ? "opacity-60" : ""}`}
      >
        <span
          aria-hidden
          className="mt-0.5 h-full min-h-[2.5rem] w-1 shrink-0 rounded-shell-pill"
          style={{ backgroundColor: toOpaqueHex(row.color) }}
        />
        <span className="min-w-0 flex-1">
          <span className="block text-label-sm text-ink-muted">{row.timeLabel}</span>
          <span className="flex min-w-0 items-center gap-1.5">
            <span className="truncate text-body text-ink">{row.title}</span>
            {row.overlaps && (
              <span className="shrink-0 rounded-full bg-warning/10 px-1.5 py-0.5 text-label-sm text-warning">
                Overlaps
              </span>
            )}
          </span>
          <span className="flex items-center gap-1 truncate text-label-sm text-ink-muted">
            {row.dayLabel && <span className="shrink-0">{row.dayLabel} ·</span>}
            {row.hasConferenceUrl && <Video className="size-3 shrink-0" />}
            {agendaSecondLineText(row)}
          </span>
        </span>
      </button>
      {row.hasConferenceUrl && (
        <a
          href={row.occurrence.event.conferenceUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="absolute top-1/2 right-2 -translate-y-1/2 rounded-shell-sm border border-border bg-surface px-2 py-1 text-label-sm text-ink opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 hover:bg-surface-hover"
        >
          Join
        </a>
      )}
    </div>
  );
}
