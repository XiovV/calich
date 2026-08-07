# Feature Map

A snapshot of what this calendar server (Go backend + CalDAV) does and does not do, as of branch `calendar-subscriptions`. Sourced from `CONTEXT.md`, all ADRs in `docs/adr/`, and the code under `server/internal/`.

## Core Entities

- **User** — single account per instance today, but modeled as a full row (bootstrap `admin`/`admin`, forced password change, or env-supplied credentials). Carries email (for reminders) and a synced-device-reminders toggle.
- **Session** — refresh-token-backed web login session.
- **AppPassword** — per-user, hashed, revocable credential for CalDAV Basic auth.
- **Calendar** — `id, user_id, name, color, created_at`. Color is arbitrary hex, not an enum.
- **Event** — Master/Override/Exception model (see below), with title, start/end, all-day flag, tzid, rrule, description, location.
- **EventException** — cancelled occurrence (EXDATE).
- **EventReminder** — `(event_id, offset_minutes, channel)`.
- **FiredReminder** — exactly-once firing ledger.
- **Notification** — persisted in-app record from a fired reminder.
- **Sync** — the `change_seq`/tombstone machinery backing CalDAV CTag/ETag/sync-token.

## Auth

Two independent mechanisms, since the web SPA and native CalDAV clients need different credential types:

- **Web app**: JWT access token (in-memory) + httpOnly/Secure refresh cookie. Login/refresh/logout, forced password change on bootstrap, email + synced-device-reminders settings.
- **CalDAV**: HTTP Basic over App Passwords, validated by separate middleware in front of `/dav/*`. Passwords are hashed, listable/creatable/revocable, shown once on creation.
- No open sign-up, no OAuth/SSO, no Digest auth (deliberately rejected — Basic-over-TLS deemed sufficient).

## Calendar Features

- CRUD via REST (`/api/calendars`).
- Arbitrary hex color (`#RGB`/`#RRGGBB`/`#RRGGBBAA`), canonicalized — not a fixed palette (an earlier 8-color enum was removed).
- Default calendars seeded on first use.
- Client-supplied UUID ids.
- ICS export: single calendar, or all calendars as a zip.
- ICS import (see Import/Export below).

## Event Features

- CRUD with `from`/`to` range filtering.
- **Recurrence via RRULE (RFC 5545)**, stored verbatim as a string. Expansion happens client-side for rendering; the server independently re-implements expansion only for CalDAV time-range queries and reminder firing.
- **Master / Override / Exception model**: a Master carries the RRULE; an Override is a full Event row keyed by `(parent_id, recurrence_id)` replacing one occurrence; an Exception is an EXDATE suppressing one occurrence. Supports "this and following" series splits (reparent).
- **All-day events**: date-only, half-open range, no timezone. Cannot span multiple days (out of scope for now).
- **Timezone model**: UTC instants + nullable tzid anchor. Three states — named IANA zone, `Etc/UTC` (absolute), or `NULL` (floating, expands in viewer's zone).
- **Reminders**: unconstrained `(offsetMinutes, channel)` pairs, no dedupe. Channels: `notification` (in-app) and `email` only. All-day reminders anchor to 09:00, not midnight.
- Description and location free-text fields.
- Per-event ICS export: whole series, or a single flattened occurrence.

## CalDAV Protocol Support

Built on `emersion/go-webdav`'s `caldav` package + `emersion/go-ical`, mounted at `/dav/`, with `/.well-known/caldav` auto-discovery.

- **Discovery**: current-user-principal, calendar-home-set, per-calendar collections, per-series objects.
- **PROPFIND**: via the library, plus custom XML patching for `getctag` and Apple/DAVx⁵ `calendar-color`.
- **PROPPATCH**: fully hand-rolled (the library hardcodes 501). Settable: `calendar-color`, `displayname` (rename). Atomic per RFC 4918 §9.2 — all-or-nothing, `424 Failed Dependency` on partial failure, `403` on `<remove>` or unknown properties.
- **REPORT**: `calendar-query` (time-range filtered, expanded at query time), `calendar-multiget`, and a hand-rolled `sync-collection` (RFC 6578) since the library doesn't support it.
- **GET/PUT/DELETE** on calendar objects, decomposing an uploaded `.ics` into Master + Overrides + EXDATEs in one transaction. `If-Match`/`If-None-Match` conditional headers, checked against a freshly recomputed ETag.
- **ETag/CTag/sync-token** all derived from one global `change_seq` counter — no separate change-log table.
- One `.ics` object = one whole series (Master + Overrides + EXDATEs), sharing the Master's UUID as UID. Overrides are never independently addressable.
- **Calendar creation over CalDAV is not supported** (`501`) — calendars are web-app-only.
- Reminder double-fire mitigation: Email always server-fired; Notification gated by a per-user opt-out toggle once a device is CalDAV-synced.

## Sync / Import / Export

- Shared iCalendar codec used by both CalDAV and ICS import/export. Emits VTIMEZONE for every referenced TZID by probing Go's zone-transition data (not a full IANA zone database) — may render historical pre-transition-change events an hour off, matching Google/Apple behavior.
- **ICS export**: read-only, reuses the shared codec.
- **ICS import**: multipart upload, `.ics` or `.zip`, 10MB cap, dry-run preview mode.
  - **Copy-in, not restore**: mints a fresh UUID per series, discards the foreign UID — no dedupe, re-importing duplicates everything.
  - Targets: `new` (create calendar), `existing` (single `.ics` only), `skip`.
  - Per-series failure isolation — a malformed VEVENT is skipped and reported, rest of the file still imports.
  - Unresolvable TZID degrades to a floating event (reported, not silently dropped).
  - VALARMs import faithfully on both channels.
  - Import summary (skipped/adjusted/ignored counts, reminder counts) is part of the UX contract.

## Notifications / Reminders

- Single background scheduler ticking on `(lastTick, now]`, in-memory `lastTick` reset on restart — **no catch-up** after downtime, missed fires are dropped by design.
- Exactly-once dispatch via a fired-reminder ledger keyed by `(reminderID, occurrenceStart)`.
- Two delivery channels: `NotificationDispatcher` (persisted, surfaced via `/api/notifications`), `EmailDispatcher` (SMTP, offered only when configured server-side and the user has an email set). A `LogDispatcher` exists as a no-op/test seam.
- No Web Push yet (named as a possible future layer on the same Notification records).

## Explicit Non-Goals / Limitations

- **Single-user instance** — no open sign-up; schema supports multi-user later, but the rule "one account" is enforced today.
- **No Digest auth for CalDAV** — Basic-over-TLS only.
- **CalDAV round-trips are lossy**: `CATEGORIES`, `ATTENDEE`/`ORGANIZER`, `STATUS`, `CLASS`, `URL`, `GEO`, custom `X-` properties, and non-before-start VALARM triggers are silently dropped on every PUT — no sidecar store.
- **No per-event color** — RFC 7986 `COLOR` on VEVENT deliberately rejected.
- **No CTag propagation for calendar-color changes** — convergence is undefined, last-write-wins.
- **ICS import is not backup/restore** — no dedupe or UID preservation at import time (Subscriptions plan to reopen this narrowly, see below).
- **All-day events cannot span multiple days.**
- **Reminder channels limited to Notification and Email** — no `AUDIO` or other VALARM actions.
- **No reminder catch-up after process downtime.**
- **Partial CalDAV property retrieval not implemented** — `ListCalendarObjects` always returns full objects, ignoring component/property selection.
- **Calendar creation not supported over CalDAV.**
- A read-only `webcal://` ICS-feed design was considered and fully rejected in favor of going straight to two-way CalDAV — never shipped.

## In Progress: Calendar Subscriptions (design accepted, not yet implemented)

Branch `calendar-subscriptions` currently contains two accepted ADRs and a no-behavior-change refactor — **no working feature code yet**.

- **ADR-0032**: a Subscribed Calendar will be an ordinary `calendars` row carrying a source URL (not a separate entity), read-only (enforced in `EventService`, not the schema), refreshed by a server-side poller, with per-subscription opt-in VALARM honoring and CalDAV visibility as a read-only collection. Explicitly *not yet* addressed: SSRF/private-address blocking on subscription URLs, and subscription credentials will be stored in plaintext (both named as accepted risk for the current single-user deployment, to revisit before multi-user).
- **ADR-0033**: Refresh (poll-and-reconcile) will add a new External UID column (unique per calendar) and reconcile series-by-series — upsert / tombstone-if-absent / skip-if-unparseable-but-present — rather than delete-and-reinsert, to avoid mass `change_seq` churn. Conditional GET short-circuits unchanged feeds. Hourly default poll, `REFRESH-INTERVAL`/`X-PUBLISHED-TTL`-aware backoff, exponential backoff to ~24h ceiling on failure, manual refresh bypasses both.
- **What's actually landed so far**: `repository.CalendarFields` / `service.CalendarWrite` structs introduced so `Create`/`Update` take one struct instead of positional `name, color` args — pure groundwork for adding subscription fields later without another signature change. No Subscription field, no Refresh/reconciliation logic, no `external_uid` column, and no poller exist in code yet.
