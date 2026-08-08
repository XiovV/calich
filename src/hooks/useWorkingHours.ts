import { useAuthStore } from "../lib/authStore";

export interface WorkingHours {
  start: number;
  end: number;
}

// Working hours (ADR-0039): minutes-since-midnight (0-1439, #136/#137), both
// null means no shading, the default — Day and Week view read this to tint
// the rows outside the range.
export function useWorkingHours(): WorkingHours | null {
  const start = useAuthStore((state) => state.user?.workingHoursStart ?? null);
  const end = useAuthStore((state) => state.user?.workingHoursEnd ?? null);
  if (start === null || end === null) return null;
  return { start, end };
}
