interface DraftBlockPreviewProps {
  top: number;
  height: number;
}

export function DraftBlockPreview({ top, height }: DraftBlockPreviewProps) {
  return (
    <div
      className="pointer-events-none absolute inset-x-0 rounded-shell-sm border-2 border-dashed border-accent bg-accent-soft"
      style={{ top: `${top}px`, height: `${height}px` }}
    />
  );
}
