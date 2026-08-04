# Timed Events stored as a UTC instant plus a TZID anchor

Status: accepted

A timed Event stores `start`/`end` as canonical **UTC instants** alongside a nullable **`tzid`** anchor column. The instant is the moment everyone agrees on; the anchor preserves the event's wall-clock intent for recurrence, DST, and CalDAV round-trip. All-day Events are unaffected — they remain date-only and timezone-free (ADR-0017).

## What was collapsed before

The app treated one thing — "the browser's ambient zone" — as both the zone an event's wall-clock is defined in and the zone it's displayed in. That works for a single viewer who never leaves their zone, but it cannot express shared Events viewed across zones (a 15:00 Berlin meeting shown as 09:00 in New York) or preserve the `TZID` a CalDAV client sends. This ADR splits that one thing into two:

- **Anchor zone** (`tzid` on the Event) — where the wall-clock is defined; drives recurrence + DST expansion; preserved on edit.
- **Viewer zone** — browser-detected; drives the display shift; applied only at render.

Driver: CalDAV pulls "per-event timezone" into scope (ADR-0013), and shared multi-user Events across zones make per-viewer display a real requirement rather than a hypothetical.

## The model

- **Canonical truth**: `start`/`end` are UTC instants. Range queries and ordering stay trivial and correct (no zone conversion to compare).
- **`tzid` semantics** (covers CalDAV's three `DTSTART` forms):
  - a named IANA zone (e.g. `Europe/Berlin`) — a **zoned** Event; recurrence expands in that zone.
  - `Etc/UTC` — an **absolute** UTC instant; recurrence expands in UTC (no DST).
  - `NULL` — a **Floating Event**; renders and expands recurrence in the Viewer zone. This is the pre-timezone behavior, so a null-tzid Event runs the existing code path unchanged.
- **Create**: the web UI stamps the anchor = creator's Viewer zone automatically. No per-event zone picker yet; the column and CalDAV ingest already accept any `TZID`, so adding a picker later is purely additive.
- **Wire format**: `start`/`end` are emitted as UTC `…Z` strings with a sibling `tzid` field (absent/`null` = floating). UTC on the wire makes it structurally impossible for the client to render off the anchor's offset instead of the Viewer zone. All-day stays date-only.
- **Expansion**: rrule.js is retained; `date-fns-tz` is added for zone↔instant conversion. `floatingTime.ts` moves from "browser-ambient floating" to explicit anchor-zone → UTC → viewer-zone conversion. DST-boundary resolution uses `date-fns-tz` defaults — forward-shift a nonexistent wall-clock, take the earlier offset for an ambiguous one — matching RFC 5545 practice and iOS/macOS Calendar, so CalDAV round-trips stay consistent.

## Considered and rejected

- **UTC instant only, no `tzid`**: loses wall-clock intent. A UTC-anchored weekly Event drifts an hour after the clocks change, and the `TZID` a client sent can't be handed back. Rejected.
- **Wall-clock string + `tzid`, derive the instant on read** (CalDAV's zoned form as canonical storage): faithful, but every server-side range query needs zone conversion to compare, and a DST-ambiguous wall-clock has no single instant to store. Rejected in favor of resolving to a UTC instant at write time.

## Migration

Additive `ALTER TABLE ADD COLUMN tzid TEXT`, backfilling `NULL`. No existing `start`/`end` value is touched. Pre-existing Events become Floating Events, which exactly preserves their current behavior (render in the viewer's zone) and makes no false claim about a zone the server never knew. A later edit or CalDAV sync upgrades a row to a real zone.
