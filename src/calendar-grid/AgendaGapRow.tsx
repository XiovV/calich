interface AgendaGapRowProps {
  label: string;
}

// A Gap (#334, CONTEXT.md) — "nothing scheduled here", not "available", so
// it is rendered as plainly muted text rather than anything that invites a
// click (there is deliberately no create-from-Gap).
export function AgendaGapRow({ label }: AgendaGapRowProps) {
  return <p className="px-2 py-1.5 text-label-sm text-ink-muted">{label}</p>;
}
