import { endOfWeek, format, isSameMonth, startOfWeek } from "date-fns";
import type { ActiveView } from "./shellStore";

function formatWeekLabel(date: Date): string {
  const start = startOfWeek(date);
  const end = endOfWeek(date);
  if (isSameMonth(start, end)) {
    return `${format(start, "MMM d")} – ${format(end, "d, yyyy")}`;
  }
  return `${format(start, "MMM d")} – ${format(end, "MMM d, yyyy")}`;
}

export function formatDateLabel(date: Date, view: ActiveView): string {
  switch (view) {
    case "day":
      return format(date, "MMMM d, yyyy");
    case "month":
      return format(date, "MMMM yyyy");
    case "year":
      return format(date, "yyyy");
    case "week":
      return formatWeekLabel(date);
  }
}
