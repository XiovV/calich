import { format } from "date-fns";
import type { TimeFormat } from "./authApi";

// Time format (ADR-0039): the date-fns pattern every call site that formats
// a time itself renders through, so the hour gutter, Event chips, timed
// Event blocks and the Month drag preview agree with the User's Preference
// instead of a hardcoded 12-hour string or the browser locale.
export function timePattern(timeFormat: TimeFormat): string {
  return timeFormat === "12h" ? "h:mm a" : "HH:mm";
}

// A full date-and-time stamp, e.g. "Aug 20, 2026, 6:47 PM" (ADR-0039, #236)
// — the app's short date (ImportPreviewDialog, SubscribeCalendarModal)
// paired with timePattern, for the one or two surfaces (the Notification
// feed, its browser-notification mirror) that show a point in time that
// isn't necessarily today and so need the date alongside it. Never reaches
// Date.prototype.toLocaleString, which renders in the browser's locale
// instead of the User's Preference.
export function formatDateTime(date: Date, timeFormat: TimeFormat): string {
  return format(date, `MMM d, yyyy, ${timePattern(timeFormat)}`);
}
