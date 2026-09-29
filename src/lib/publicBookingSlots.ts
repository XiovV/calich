import { toZonedTime } from "date-fns-tz";
import { format } from "date-fns";
import type { TimeFormat } from "./authApi";
import { timePattern } from "./timeFormat";

// The public Booking Link page's own shared derivation (#324, ADR-0085,
// ADR-0087): zone conversion, day grouping and time formatting, kept out of
// the page component itself so the month grid's "which days have slots" and
// the slot list's "what time is this, in whose zone" logic is unit-testable
// on its own — slots themselves come from the backend (#323); this module
// only ever reshapes what it already received.

// A slot instant's calendar date in zone, e.g. "2026-08-20" — the visitor's
// own local day, not the host's Anchor zone (ADR-0085's Schedule zone) and
// not the browser's own zone unless zone happens to equal it. Used both to
// mark a day available on the month grid and to filter the slot list down
// to the selected day.
export function zonedDayKey(slot: Date, zone: string): string {
  return format(toZonedTime(slot, zone), "yyyy-MM-dd");
}

// Every day key (zonedDayKey) that has at least one slot, in zone — the
// month grid's "available" marker (#324's "A month grid marks days with
// slots as available and days without as unavailable").
export function daysWithSlots(slots: Date[], zone: string): Set<string> {
  const days = new Set<string>();
  for (const slot of slots) {
    days.add(zonedDayKey(slot, zone));
  }
  return days;
}

// slots that fall on day (compared in zone), sorted chronologically — the
// slot list beside the grid for whichever day is selected.
export function slotsForDay(slots: Date[], day: Date, zone: string): Date[] {
  const key = zonedDayKey(day, zone);
  return slots.filter((slot) => zonedDayKey(slot, zone) === key).sort((a, b) => a.getTime() - b.getTime());
}

// A slot's time-of-day in zone, formatted per the visitor's 12h/24h choice —
// never the slot's UTC or host-zone time, since the whole point of the
// picker is showing the visitor their own wall-clock (ADR-0085).
export function formatSlotTime(slot: Date, zone: string, timeFmt: TimeFormat): string {
  return format(toZonedTime(slot, zone), timePattern(timeFmt));
}
