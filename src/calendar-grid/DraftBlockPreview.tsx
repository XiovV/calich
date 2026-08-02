interface DraftBlockPreviewProps {
  top: number;
  height: number;
  left: number;
  width: number;
}

export function DraftBlockPreview({ top, height, left, width }: DraftBlockPreviewProps) {
  return (
    <div
      className="pointer-events-none absolute rounded-shell-sm border-2 border-dashed border-accent bg-accent-soft transition-[top,height,left,width] duration-100 ease-out"
      style={{
        top: `${top}px`,
        height: `${height}px`,
        left: `${left}%`,
        width: `calc(${width}% - 2px)`,
      }}
    />
  );
}
