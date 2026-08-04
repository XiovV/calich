# Server-side scheduler fires reminders; the server learns RRULE expansion

Reminders are fired by a single server-side scheduler — a background ticker in the Go process. Email is sent over SMTP; a Notification-channel Reminder inserts a persistent Notification record the web app displays. To know when a recurring Event's Reminder is due, the server gains occurrence expansion from the `RRULE`, reversing — **for the firing path only** — ADR-0016's decision that recurrence expansion lives solely in the frontend.

## Why

Email cannot be sent from a browser and must fire when the app is closed, so a server-side scheduler is unavoidable regardless of the in-app design. Firing a recurring Event's Reminder requires the next Occurrence start, which until now only the frontend could compute. A single scheduler handles both channels consistently and keeps the frontend a pure display of server-created Notifications rather than a second firing engine.

## Decisions

- **One scheduler, both channels.** Notification → a persistent record shown in an in-app feed, mirrored as a browser notification when a tab is open. Web Push is deferred and would layer onto the same records.
- **Exactly-once** via a fired-ledger keyed by `(reminder, occurrenceStart)`.
- **No catch-up.** Only Reminders that come due while the scheduler is live fire; fires missed during downtime are dropped, not replayed. A deliberate simplicity trade-off for a self-hosted personal calendar.
- **Email config**: recipient is a per-user `email` column; SMTP transport (host/port/user/pass/from) is env config. The Email channel is offered only when both are set — consistent with ADR-0010's refusal to bake in a singleton user.
- **Phasing**: phase 1 persists and round-trips Reminders with nothing firing; this scheduler is phase 2.

## Consequences

- The Go server takes on `RRULE` expansion (a new dependency) for the firing path. Frontend expansion (ADR-0016) is unchanged for rendering; the two must agree on Occurrence starts. This capability also moves the server toward what CalDAV server-side expansion will later want.
- Downtime silently drops missed fires. Acceptable now; revisit if it bites.
- Synced CalDAV devices fire their own `DISPLAY` alarms, so server-side Notification firing can double up for synced users. Deduplication is deferred to the CalDAV grilling — see ADR-0020.
