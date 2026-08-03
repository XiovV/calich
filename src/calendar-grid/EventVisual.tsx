import { format } from "date-fns";

interface EventVisualProps {
  title: string;
  start: Date;
  end: Date;
  colorClass: string;
  isPast?: boolean;
}

export function EventVisual({
  title,
  start,
  end,
  colorClass,
  isPast,
}: EventVisualProps) {
  return (
    <div className="h-full w-full overflow-hidden rounded-shell-sm bg-surface">
      <div
        className={`h-full w-full px-1.5 py-1 text-left text-ink-inverse ${colorClass} ${isPast ? "opacity-60" : ""}`}
      >
        <p className="truncate text-label-sm font-medium">{title}</p>
        <p className="truncate text-label-sm opacity-90">
          {format(start, "h:mm a")} – {format(end, "h:mm a")}
        </p>
      </div>
    </div>
  );
}
