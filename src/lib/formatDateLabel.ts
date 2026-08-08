import { endOfWeek, format, isSameMonth, startOfWeek } from "date-fns";
import type { Day as WeekStartsOn } from "date-fns";
import type { ActiveView } from "./shellStore";

function formatWeekLabel(date: Date, weekStartsOn: WeekStartsOn): string {
  const start = startOfWeek(date, { weekStartsOn });
  const end = endOfWeek(date, { weekStartsOn });
  if (isSameMonth(start, end)) {
    return `${format(start, "MMM d")} – ${format(end, "d, yyyy")}`;
  }
  return `${format(start, "MMM d")} – ${format(end, "MMM d, yyyy")}`;
}

export function formatDateLabel(date: Date, view: ActiveView, weekStartsOn: WeekStartsOn): string {
  switch (view) {
    case "day":
      return format(date, "MMMM d, yyyy");
    case "month":
      return format(date, "MMMM yyyy");
    case "year":
      return format(date, "yyyy");
    case "week":
      return formatWeekLabel(date, weekStartsOn);
  }
}
