import { useAuthStore } from "../lib/authStore";
import { timePattern } from "../lib/timeFormat";

// Time format (ADR-0039): the date-fns pattern every call site formatting a
// time reads from, shared so the fallback used before the User has loaded
// lives in exactly one place.
export function useTimePattern(): string {
  const timeFormat = useAuthStore((state) => state.user?.timeFormat ?? "24h");
  return timePattern(timeFormat);
}
