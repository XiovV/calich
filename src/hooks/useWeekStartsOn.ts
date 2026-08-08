import type { Day } from "date-fns";
import { useAuthStore } from "../lib/authStore";

// Week start (ADR-0039): the date-fns weekStartsOn index every grid and mini
// calendar reads from, shared so the fallback used before the User has
// loaded lives in exactly one place.
export function useWeekStartsOn(): Day {
  return useAuthStore((state) => state.user?.weekStart ?? 1) as Day;
}
