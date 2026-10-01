import { addDays, endOfWeek, format, isSameMonth, startOfDay, startOfWeek } from "date-fns";
import type { Day as WeekStartsOn } from "date-fns";
import { AGENDA_WINDOW_DAYS } from "./agenda";
import type { ActiveView } from "./shellStore";

function formatWeekLabel(date: Date, weekStartsOn: WeekStartsOn): string {
  const start = startOfWeek(date, { weekStartsOn });
  const end = endOfWeek(date, { weekStartsOn });
  if (isSameMonth(start, end)) {
    return `${format(start, "MMM d")} – ${format(end, "d, yyyy")}`;
  }
  return `${format(start, "MMM d")} – ${format(end, "MMM d, yyyy")}`;
}

// formatAgendaLabel is the loaded span, e.g. "Sep 30 – Oct 14, 2026" (#330).
// `today` stands in for `date` here: Agenda isn't driven by the Selected
// date (CONTEXT.md's Active view), so the caller passes the anchor it
// actually uses — today — rather than selectedDate.
function formatAgendaLabel(today: Date): string {
  const start = startOfDay(today);
  const end = addDays(start, AGENDA_WINDOW_DAYS - 1);
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
    case "agenda":
      return formatAgendaLabel(date);
  }
}
