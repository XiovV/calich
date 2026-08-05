# CalDAV server built on emersion/go-webdav, shipped read-first

Status: accepted

We implement the CalDAV server committed to in ADR-0013 on top of **`emersion/go-webdav`** (its `caldav` package) and **`emersion/go-ical`**, rather than hand-rolling the protocol. The library owns the WebDAV/XML surface — `PROPFIND`, the `REPORT`s (`calendar-query`, `calendar-multiget`, `sync-collection`), depth handling, multistatus bodies — and we implement its `caldav.Backend` interface over the existing repository/service layer. `go-ical` handles VEVENT/VALARM/RRULE/TZID parsing and serialization.

## Why a library

The hard, interop-sensitive part of CalDAV is the protocol plumbing (XML, multistatus, PROPFIND/REPORT routing, sync tokens), not the domain mapping. Our storage already mirrors iCalendar row-for-row (RRULE master, RECURRENCE-ID overrides, EXDATE exceptions, TZID anchor, VALARM reminders — ADR-0016/0019/0020), so the `Backend` methods (`Calendar`, `ListCalendarObjects`, `GetCalendarObject`, `PutCalendarObject`, `DeleteCalendarObject`, `QueryCalendarObjects`) map almost 1:1 onto existing services. Hand-rolling would mean owning 100% of the protocol surface and its per-client interop bugs — a poor trade for a solo self-hosted project. Accepted cost: we inherit the library's interface shape and any spec gaps become our patches.

## URL layout and discovery

- Mounted under a **`/dav/` prefix**, registered in the chi router **ahead of the SPA catch-all** (`r.Handle("/*", …)`) and **outside** the JWT `RequireAuth` middleware — it uses the CalDAV Basic-auth middleware instead (ADR-0024). This is the placement ADR-0012 had already reserved for the feed endpoint.
- URLs are **keyed by user id, not singleton** — principal `/dav/principals/{userId}/`, calendar-home `/dav/calendars/{userId}/`, collections `…/{calendarId}/`, objects `…/{masterId}.ics`. One principal exists today (ADR-0010), but the id in the path costs nothing now and avoids a migration if multi-user ever lands, matching ADR-0010's philosophy.
- A tiny **`/.well-known/caldav`** handler (also ahead of the catch-all) redirects to `/dav/`, so email-style auto-discovery works; `current-user-principal` resolves from the Basic-auth'd user.
- Adds `description` and `location` columns to `events` — modeled by every native client, additive migration, no design tension.

## Read-first phasing

- **Phase 1** — app-password auth (ADR-0024) + discovery + read path (`PROPFIND` / `calendar-query` / `GET`), serving existing events as `VCALENDAR`s with ETag/CTag/sync-token (ADR-0025) so clients poll efficiently. No `PUT`/`DELETE`.
- **Phase 2** — the write path: `PutCalendarObject` decompose, `DeleteCalendarObject` + tombstones, `If-Match` conflict handling, and the notification-dedup toggle (ADR-0027), which only matters once devices actively fire alarms.

This is **not** the read-only `webcal://` ICS feed ADR-0013 rejected — that was a separate throwaway stack (feed tokens, subscribe UI). This is the real CalDAV server, shipped read-first so the interop-heavy discovery/auth/serialization is validated against real clients before ingest is built. Every line survives into Phase 2.
