import { timeToY } from "../lib/gridTime";
import { GRID_Z_CURRENT_TIME } from "./gridStacking";

interface CurrentTimeLineProps {
  now: Date;
  pixelsPerHour: number;
}

export function CurrentTimeLine({ now, pixelsPerHour }: CurrentTimeLineProps) {
  const top = timeToY(now, pixelsPerHour);

  return (
    <div
      className="pointer-events-none absolute inset-x-0 flex items-center"
      style={{ top: `${top}px`, zIndex: GRID_Z_CURRENT_TIME }}
    >
      <div className="size-2 -translate-x-1/2 rounded-shell-pill bg-accent-ink" />
      <div className="h-px flex-1 bg-accent-ink" />
    </div>
  );
}
