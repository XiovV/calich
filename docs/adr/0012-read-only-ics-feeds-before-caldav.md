# Read-only iCalendar feeds now, CalDAV two-way sync later

Status: superseded by ADR-0013 — we chose to go straight to CalDAV and skip this read-only interim.

To get events onto native iPhone/macOS calendar apps, we expose one read-only iCalendar (`.ics`) **Calendar feed** per Calendar that clients subscribe to (`webcal://`), rather than implementing a CalDAV server up front. Feeds are one-way (server → device, read-only on the device); full two-way CalDAV sync is deferred to a later phase.

## Considered Options

- **Read-only ICS subscription feeds (chosen).** Natively supported by every calendar app, self-contained to build (serve one `.ics` document), and works with the current data model unchanged. The cost is that it's one-way and refresh is client-controlled (iOS polls on its own slow schedule).
- **CalDAV (two-way).** The eventual goal, but a much larger build: it needs a new HTTP Basic/Digest auth path, ETag/sync-token machinery, and round-tripping iCalendar fields the schema doesn't store yet (recurrence, timezones, all-day, description/location, alarms). Deferred, not rejected.
- **WebDAV alone.** Not calendar-aware — rejected; CalDAV is the relevant extension.

## Decisions within the feed design

- **One feed per Calendar**, not a combined feed — a subscribed `.ics` URL maps to exactly one device-side calendar with one color and one toggle, so per-Calendar feeds preserve the Calendar abstraction (name via `X-WR-CALNAME`, color via `X-APPLE-CALENDAR-COLOR`).
- **Capability-URL auth**, not HTTP Basic — an unguessable per-User Feed token in the URL path (`/feed/{feedToken}/{calendarId}.ics`), served over HTTPS. Keeps the real account password off the device and is revocable independently of login by rotating the token. Endpoint lives outside the JWT middleware and ahead of the SPA catch-all.
- **A single per-User Feed token**, not per-Calendar tokens — these feeds are for the owner's own devices, not for sharing individual calendars with other people, so coarse all-or-nothing revocation is acceptable. Per-Calendar tokens are the upgrade path if calendar sharing is ever added.
- **The browser builds the `webcal://` subscribe link** from `location`; the API returns only the token and calendar list. The web UI and feeds share one origin, so no `BASE_URL` config is needed.
- **Unbounded event window** — the whole calendar, past and future, at personal scale. `ListByUser` already supports a `from`/`to` range if bounding is ever needed.

## Consequences

- **No alerts on the device.** Reminders aren't persisted in the schema, so feeds carry no `VALARM`; subscribed events won't trigger notifications on the phone.
- **No all-day events, recurrence, or description/location** — none are modeled today; every event maps to a single timed `VEVENT` (`UID` = event id, `DTSTART`/`DTEND` in UTC, `SUMMARY` = title).
- **Freshness is client-controlled** — the device decides when to re-poll (often 15 min to hours on iOS). Removing this lag is a primary reason to pursue CalDAV later.
