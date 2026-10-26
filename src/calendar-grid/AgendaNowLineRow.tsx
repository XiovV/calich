interface AgendaNowLineRowProps {
  label: string;
}

// Today's amber now-line (#332) — the list-shaped counterpart to the grid's
// CurrentTimeLine, labelled with the current time since Agenda has no
// vertical axis to place an unlabelled line against.
export function AgendaNowLineRow({ label }: AgendaNowLineRowProps) {
  return (
    <div className="flex items-center gap-2 px-2 py-1">
      <span className="text-label-sm font-medium text-accent-ink">{label}</span>
      <span aria-hidden className="h-px flex-1 bg-accent-ink" />
    </div>
  );
}
