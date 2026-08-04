import { fromZonedTime, toZonedTime } from "date-fns-tz";

// rrule.js does all of its date math in UTC. To get zone-correct wall-clock
// semantics — a 09:00 series stays 09:00 across a DST transition (ADR-0016)
// — we run the rule in a "floating" UTC frame: a Date whose UTC fields equal
// the wall-clock the source instant represents in a given zone. Because that
// frame has no DST, the rule never drifts; we then resolve each produced
// wall-clock back to a true UTC instant in the same zone. The same frame is
// used to build/parse an RRULE `UNTIL`, so end dates line up with what
// expansion produces. See ADR-0019.

/**
 * The zone recurrence falls back to for a Floating Event (`tzid` absent):
 * the browser's own zone. A single source value, not a user-facing
 * preference yet (ADR-0019).
 */
export function viewerZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone;
}

/** The zone an Event's recurrence expands in: its Anchor zone, or the
 * Viewer zone when it's a Floating Event (`tzid` absent). See ADR-0019. */
export function anchorZone(tzid: string | undefined): string {
  return tzid ?? viewerZone();
}

/**
 * A UTC instant reinterpreted into the floating frame for `zone`: a Date
 * whose UTC fields equal the wall-clock time `instant` represents in `zone`.
 */
export function toFloating(instant: Date, zone: string): Date {
  const zoned = toZonedTime(instant, zone);
  return new Date(
    Date.UTC(
      zoned.getFullYear(),
      zoned.getMonth(),
      zoned.getDate(),
      zoned.getHours(),
      zoned.getMinutes(),
      zoned.getSeconds(),
    ),
  );
}

/**
 * `toFloating`'s inverse: a floating-frame Date resolved back to the true
 * UTC instant it represents in `zone`. A nonexistent wall-clock (the
 * spring-forward gap) shifts forward; an ambiguous one (the fall-back
 * overlap) resolves to the earlier offset — date-fns-tz's defaults, matching
 * RFC 5545 / iOS-macOS Calendar practice (ADR-0019).
 */
export function fromFloating(floating: Date, zone: string): Date {
  const local = new Date(
    floating.getUTCFullYear(),
    floating.getUTCMonth(),
    floating.getUTCDate(),
    floating.getUTCHours(),
    floating.getUTCMinutes(),
    floating.getUTCSeconds(),
  );
  return fromZonedTime(local, zone);
}
