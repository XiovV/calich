import { HOURS_IN_DAY } from "../lib/gridTime";

interface TimeAxisProps {
  pixelsPerHour: number;
}

export function TimeAxis({ pixelsPerHour }: TimeAxisProps) {
  const hours = Array.from({ length: HOURS_IN_DAY }, (_, hour) => hour);

  return (
    <div
      className="relative w-14 shrink-0"
      style={{ height: pixelsPerHour * HOURS_IN_DAY }}
    >
      {hours.map((hour) => (
        <div
          key={hour}
          className="absolute right-2 -translate-y-1/2 text-label-sm text-ink-muted"
          style={{ top: hour * pixelsPerHour }}
        >
          {hour === 0
            ? ""
            : new Date(2000, 0, 1, hour).toLocaleTimeString(undefined, {
                hour: "numeric",
              })}
        </div>
      ))}
    </div>
  );
}
