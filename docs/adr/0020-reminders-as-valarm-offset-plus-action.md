# Reminders as iCalendar VALARMs (offset + action), round-tripping through CalDAV

A Reminder is modeled as a trigger offset plus a delivery action — `ACTION:DISPLAY` (in-app Notification) or `ACTION:EMAIL` — mapping one-to-one onto an iCalendar `VALARM` inside the Event's `VEVENT`, so it round-trips through CalDAV unchanged. This continues ADR-0016 (recurrence as `RRULE`) and ADR-0019 (timezone as `TZID`): domain concepts that must survive a CalDAV round-trip are stored in their iCalendar shape, not a bespoke one.

## Why

CalDAV is the next major workstream, and ADR-0013 explicitly named alarms as the persisted home for the Reminder concept. A bespoke reminder model now would mean rework and a lossy mapping once CalDAV ingest lands. An `(offset, action)` pair is a faithful, minimal projection of `VALARM` that the UI can still present as a preset list.

## Decisions

- **Channels**: Notification (`DISPLAY`) and Email (`EMAIL`). `AUDIO` and other actions are out of scope.
- **Offset** is stored normalized as signed minutes before the Occurrence start. For all-day Events the offset is measured from **09:00 on the start date**, not midnight, so reminders never fire in the middle of the night.
- **Many per Event, unconstrained** — no cap, no dedupe. Reminders live on the Event row in a child table (`event_reminders`), not a JSON blob, so the phase-2 scheduler can query them.
- **Recurrence**: a Master's Reminders apply to every Occurrence; creating an Override copies the Master's set, then edits it independently. Reminder edits ride the existing scope machinery (This / This-and-following / All).
- **UI** offers a fixed preset list plus a custom offset entry, mirroring the Custom recurrence dialog.
- **Exotic client-authored triggers** (end-relative, absolute datetime, non-before-start) are deferred to the CalDAV-ingest grilling; phase 1 authors only before-start offsets.

## Consequences

- A CalDAV client faithfully replicates whatever list we store, including duplicates — the cost of the no-dedupe choice.
- Once CalDAV syncs, the device fires its own `DISPLAY` alarms from the synced `VALARM`; server-side firing of the Notification channel then risks double notifications for synced users. Resolving that is deferred to the CalDAV pass — see ADR-0021, which keeps the firing engine separable for exactly this reason.
