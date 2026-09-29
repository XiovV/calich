// A Task's Priority (#311, ADR-0083, CONTEXT.md): held as VTODO's raw
// `PRIORITY` value (0-9), never an app-specific enum, so a value written by
// a future native/CalDAV client would survive untranslated. None/Low/Medium/
// High is a presentation mapping applied at the read (and write) edge, on
// the de facto convention most iCalendar clients (Apple, Thunderbird) share
// and RFC 5545 §3.8.1.9 groups by: 0 is undefined ("None"), 1-4 is the
// "highest" band, 5 is the middle value, 6-9 is the "lowest" band.
export type PriorityLevel = "none" | "low" | "medium" | "high";

export const PRIORITY_LEVELS: readonly PriorityLevel[] = ["none", "low", "medium", "high"];

export const PRIORITY_LEVEL_LABELS: Record<PriorityLevel, string> = {
  none: "None",
  low: "Low",
  medium: "Medium",
  high: "High",
};

/** The presentation level a raw 0-9 `PRIORITY` value maps to for display. */
export function priorityLevelFromValue(value: number): PriorityLevel {
  if (value === 0) return "none";
  if (value <= 4) return "high";
  if (value === 5) return "medium";
  return "low";
}

/**
 * The raw value this app writes for `level`, picked from the middle of each
 * band (1, 5, 9) so a later re-read maps back to the same level — never
 * derived from whatever raw value a Task happened to already carry, since a
 * User picking "Low" from a menu is choosing the level, not a number.
 */
export function priorityValueForLevel(level: PriorityLevel): number {
  switch (level) {
    case "none":
      return 0;
    case "high":
      return 1;
    case "medium":
      return 5;
    case "low":
      return 9;
  }
}

/**
 * This level's rank for ordering — **not** the raw value, which runs the
 * other way (a lower number is a *higher* priority, and 0 is no priority at
 * all rather than the lowest one). Ascending: None, Low, Medium, High.
 */
export function priorityRank(value: number): number {
  switch (priorityLevelFromValue(value)) {
    case "none":
      return 0;
    case "low":
      return 1;
    case "medium":
      return 2;
    case "high":
      return 3;
  }
}
