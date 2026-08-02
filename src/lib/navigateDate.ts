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
  }
}
