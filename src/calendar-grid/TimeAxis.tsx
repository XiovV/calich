import { format } from "date-fns";
import { HOURS_IN_DAY } from "../lib/gridTime";
import { useTimePattern } from "../hooks/useTimePattern";

interface TimeAxisProps {
  pixelsPerHour: number;
}

export function TimeAxis({ pixelsPerHour }: TimeAxisProps) {
  const timePattern = useTimePattern();
  const hours = Array.from({ length: HOURS_IN_DAY }, (_, hour) => hour);

  return (
    <div
      className="relative w-18 shrink-0"
      style={{ height: pixelsPerHour * HOURS_IN_DAY }}
    >
      {hours.map((hour) => (
        <div
          key={hour}
          className="absolute right-2 -translate-y-1/2 whitespace-nowrap text-label-sm text-ink-muted"
          style={{ top: hour * pixelsPerHour }}
        >
          {hour === 0 ? "" : format(new Date(2000, 0, 1, hour), timePattern)}
        </div>
      ))}
    </div>
  );
}
