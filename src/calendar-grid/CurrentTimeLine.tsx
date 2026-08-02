import { timeToY } from "../lib/gridTime";

interface CurrentTimeLineProps {
  now: Date;
  pixelsPerHour: number;
}

export function CurrentTimeLine({ now, pixelsPerHour }: CurrentTimeLineProps) {
  const top = timeToY(now, pixelsPerHour);

  return (
    <div
      className="pointer-events-none absolute inset-x-0 z-10 flex items-center"
      style={{ top: `${top}px` }}
    >
      <div className="size-2 -translate-x-1/2 rounded-shell-pill bg-accent" />
      <div className="h-px flex-1 bg-accent" />
    </div>
  );
}
