import type { TimeFormat } from "./authApi";

// Time format (ADR-0039): the date-fns pattern every call site that formats
// a time itself renders through, so the hour gutter, Event chips, timed
// Event blocks and the Month drag preview agree with the User's Preference
// instead of a hardcoded 12-hour string or the browser locale.
export function timePattern(timeFormat: TimeFormat): string {
  return timeFormat === "12h" ? "h:mm a" : "HH:mm";
}
