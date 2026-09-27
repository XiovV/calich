// A curated fallback used only when the runtime lacks
// Intl.supportedValuesOf (older Safari) — every evergreen browser this app
// otherwise targets has it.
const FALLBACK_TIME_ZONES = [
  "Etc/UTC",
  "America/New_York",
  "America/Chicago",
  "America/Denver",
  "America/Los_Angeles",
  "America/Sao_Paulo",
  "Europe/London",
  "Europe/Berlin",
  "Europe/Sarajevo",
  "Europe/Moscow",
  "Africa/Cairo",
  "Asia/Dubai",
  "Asia/Kolkata",
  "Asia/Shanghai",
  "Asia/Tokyo",
  "Australia/Sydney",
  "Pacific/Auckland",
];

interface IntlWithSupportedValuesOf {
  supportedValuesOf?: (key: "timeZone") => string[];
}

// Every IANA timezone name the runtime knows about, for the Availability
// Schedule editor's timezone picker (#320, ADR-0085) — nothing in this
// codebase already lists timezones, since Anchor zone (ADR-0019) isn't
// editable from the web app's own UI today.
export function listTimeZones(): string[] {
  try {
    const values = (Intl as IntlWithSupportedValuesOf).supportedValuesOf?.("timeZone");
    if (values && values.length > 0) return values;
  } catch {
    // Fall through to the curated fallback below.
  }
  return FALLBACK_TIME_ZONES;
}

// The caller's own browser-detected IANA zone — used only to seed the
// Default Availability Schedule's timezone the one time it doesn't exist
// yet (#320, ADR-0085), the same "detected from browser, correctable on the
// page" pattern CONTEXT.md's Availability Schedule entry describes for a
// Booking Link visitor.
export function detectBrowserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone;
  } catch {
    return "Etc/UTC";
  }
}
