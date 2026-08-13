# CalDAV round-trips are lossy: columns are the sole source of truth

Status: accepted

When a client PUTs a `.ics` carrying iCalendar data we don't model — `CATEGORIES`, `ATTENDEE`/`ORGANIZER`, `STATUS`, `CLASS`, `GEO`, custom `X-` properties, embedded `VTIMEZONE`, or `VALARM` triggers ADR-0020 deferred (end-relative, absolute-datetime) — **we normalize it away**. `URL` is no longer one of these: ADR-0063 models it as a decomposed column like `description`/`location`, stored verbatim and round-tripped in full. The decomposed columns (title, times, `tzid`, `rrule`, `description`, `location`, `url`, reminders, overrides, exceptions) are the single source of truth; `GetCalendarObject` reserializes purely from them. There is no sidecar store of unmodeled properties.

## Why accept the loss

A single sidecar-free source of truth keeps web edits and CalDAV edits symmetric and the code simple. This is a single-user self-hosted instance (ADR-0010) where the owner controls which clients connect, so the blast radius of dropping exotic properties is small and acceptable — for now.

## The cost, and the one guard we keep

- Unmodeled client data is **silently destroyed on the first round-trip** (a client sets `CATEGORIES`, syncs, they vanish).
- Left unguarded this can cause **re-sync loops**: the client PUTs, gets back a lossily-normalized object with a mismatched ETag, and refetches forever. **Mitigation, treated as part of this decision: always derive the ETag from the normalized reconstruction** (what `GetCalendarObject` will serve), never from the client's raw bytes. Then the PUT-response ETag matches the next `GET`, so clients accept the normalization instead of looping.

## Considered and deferred

- **An `unknown_props` sidecar** that stashes unmodeled properties on PUT and re-attaches them on GET, giving lossless round-trips. Rejected for now to avoid a second source of truth and the fiddliness of faithful re-attachment (property ordering, params, un-modeled `VALARM` sub-components). Revisit if a client whose data matters starts losing fields.
