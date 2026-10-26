import { addDays, addMonths, addWeeks, addYears } from "date-fns";
import type { ActiveView } from "./shellStore";

export function navigateDate(
  date: Date,
  view: ActiveView,
  direction: "prev" | "next",
): Date {
  const amount = direction === "next" ? 1 : -1;
  switch (view) {
    case "day":
      return addDays(date, amount);
    case "week":
      return addWeeks(date, amount);
    case "month":
      return addMonths(date, amount);
    case "year":
      return addYears(date, amount);
    // ‹ / › are hidden in Agenda (#330) — it isn't driven by the Selected
    // date at all — so this is never actually reached from the UI. Handled
    // only so the switch stays exhaustive over ActiveView.
    case "agenda":
      return date;
  }
}
