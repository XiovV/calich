// rrule.js does all of its date math in UTC. To get local wall-clock semantics
// — a 09:00 series stays 09:00 even across a DST transition (ADR-0016) — we run
// the rule in a "floating" UTC frame: a Date whose UTC fields equal the source
// Date's *local* fields. Because that frame has no DST, the rule never drifts;
// we then reinterpret each produced instant's UTC fields back as local time.
// The same frame is used to build/parse an RRULE `UNTIL`, so end dates line up
// with the Occurrences expansion produces.

/** A local Date reinterpreted into the floating UTC frame (UTC fields = local fields). */
export function toFloating(date: Date): Date {
  return new Date(
    Date.UTC(
      date.getFullYear(),
      date.getMonth(),
      date.getDate(),
      date.getHours(),
      date.getMinutes(),
      date.getSeconds(),
    ),
  );
}

/** A floating-frame Date read back as local time (local fields = its UTC fields). */
export function fromFloating(date: Date): Date {
  return new Date(
    date.getUTCFullYear(),
    date.getUTCMonth(),
    date.getUTCDate(),
    date.getUTCHours(),
    date.getUTCMinutes(),
    date.getUTCSeconds(),
  );
}
