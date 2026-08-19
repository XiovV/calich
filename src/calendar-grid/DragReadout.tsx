import { useLayoutEffect, useRef, useState } from "react";
import { formatDragReadout } from "../lib/dragReadout";
import { useTimePattern } from "../hooks/useTimePattern";
import { GRID_Z_DRAG_READOUT } from "./gridStacking";

const GAP_PX = 8;

interface DragReadoutProps {
  top: number;
  height: number;
  left: number;
  width: number;
  start: Date;
  end: Date;
  columnWidth: number;
  isLastColumn: boolean;
}

/** The label beside a block being moved or resized on the hourly grid,
 * giving the start, end and duration a release would commit (issue #191).
 * Anchored to the block's right edge, flipping to its left only in the
 * grid's last day column — everywhere else, overflowing into the
 * neighbouring column is fine and simply paints over it. Stacking order
 * comes from gridStacking.ts (issue #222), so it still yields to the
 * sticky header when scrolled underneath it. */
export function DragReadout({
  top,
  height,
  left,
  width,
  start,
  end,
  columnWidth,
  isLastColumn,
}: DragReadoutProps) {
  const timePattern = useTimePattern();
  const ref = useRef<HTMLDivElement>(null);
  const [measuredWidth, setMeasuredWidth] = useState(0);

  // Deliberately no dependency array: the label's text (and so its width)
  // changes on every drag frame, so it must re-measure every render, not
  // just on mount.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useLayoutEffect(() => {
    if (ref.current) setMeasuredWidth(ref.current.offsetWidth);
  });

  const blockRightPx = (columnWidth * (left + width)) / 100;
  const roomToRightPx = columnWidth - blockRightPx;
  const flipped =
    isLastColumn && measuredWidth > 0 && roomToRightPx < measuredWidth + GAP_PX;

  const horizontalStyle = flipped
    ? { right: `calc(${100 - left}% + ${GAP_PX}px)` }
    : { left: `calc(${left + width}% + ${GAP_PX}px)` };

  return (
    <div
      ref={ref}
      className="pointer-events-none absolute rounded-shell-sm border border-border bg-surface px-1.5 py-0.5 text-label-sm text-ink whitespace-nowrap shadow-elevation-2 transition-[top,left,right] duration-100 ease-out"
      style={{
        top: `${top + height / 2}px`,
        transform: "translateY(-50%)",
        zIndex: GRID_Z_DRAG_READOUT,
        ...horizontalStyle,
      }}
    >
      {formatDragReadout(start, end, timePattern)}
    </div>
  );
}
