import { format } from "date-fns";

interface EventVisualProps {
  title: string;
  start: Date;
  end: Date;
  colorClass: string;
}

export function EventVisual({ title, start, end, colorClass }: EventVisualProps) {
  return (
    <div
      className={`h-full w-full overflow-hidden rounded-shell-sm px-1.5 py-1 text-left text-ink-inverse ${colorClass}`}
    >
      <p className="truncate text-label-sm font-medium">{title}</p>
      <p className="truncate text-label-sm opacity-90">
        {format(start, "h:mm a")} – {format(end, "h:mm a")}
      </p>
    </div>
  );
}
