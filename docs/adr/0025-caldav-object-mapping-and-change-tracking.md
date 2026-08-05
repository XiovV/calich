# A calendar object is a whole series; change tracking via a global sequence + tombstones

Status: accepted

Defines how CalDAV's addressable unit maps onto our rows, and how the server answers "what changed?".

## One object = one series = one UID

In CalDAV the addressable resource is a **calendar object** — one `.ics` file whose `VEVENT`s all share a single `UID`: the master plus every override (`RECURRENCE-ID`) plus `EXDATE`s, in one file. Our schema stores that as **many rows** (one master, N override rows, M `event_exceptions`). We bridge the identity mismatch by:

- **`UID` = the master row's `id`.** Override rows inherit the master's UID (they are distinguished by `RECURRENCE-ID`), and do not get their own. A series is one object identified by its master id.
- **Resource name = `{masterId}.ics`**, so `GET`/`PUT`/`DELETE` route straight to a master by id (already a UUID — collision-free).
- **`PutCalendarObject` decomposes** the incoming object: the `VEVENT` without a `RECURRENCE-ID` upserts the master; each with a `RECURRENCE-ID` upserts an override (`parent_id`, `recurrence_id`); each `EXDATE` becomes an `event_exceptions` row. `GetCalendarObject` recomposes them into one `VCALENDAR`. This multi-table write is exactly what the ADR-0018 transaction seam exists for.
- **Client-authored UIDs** become the master's `id`. `events.id` is already `TEXT`, so arbitrary client UID strings store fine; the web app's create path keeps generating UUIDs.

Rejected: giving each row its own UID and exposing overrides as separate objects — clients expect the whole series in one file, so this breaks the model.

## Change tracking: one global sequence + tombstones

Clients avoid refetching everything via ETag (per object), CTag (per collection), and sync-token (ordered per collection). We derive all three from data we already write rather than a separate change-log:

- A single **global `change_seq` counter**, bumped on every write to a series (master, override, or exception).
- **ETag** = the series' current `max(change_seq)` (equivalently a hash of its reconstructed object — see ADR-0026); **CTag** = `max(change_seq)` over the calendar; **sync-token** = a `change_seq` high-water mark, so `sync-collection` returns rows with `change_seq > token`.
- **Deletes leave no row to report**, so a **`deleted_objects (calendar_id, uid, change_seq)` tombstone** is written on series deletion; `sync-collection` reports removals from it. Without tombstones a client offline during a delete would keep the event forever.

A global counter (vs per-calendar) stays trivially monotonic; the per-collection sync-token just filters `change_seq > token AND calendar_id = ?`. Tombstones accumulate but are negligible at single-user scale; prune below the oldest live sync-token if ever needed.
