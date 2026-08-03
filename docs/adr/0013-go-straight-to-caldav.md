# Go straight to two-way CalDAV; skip the read-only ICS interim

Status: supersedes ADR-0012

We reversed the plan to ship a read-only iCalendar subscription feed first. Two-way **CalDAV** — create, edit, and delete Events from native calendar apps (iOS/macOS/Android) with changes syncing back to the server — is the sync approach.

## Why the reversal

This is a single-user self-hosted instance (ADR-0010) with no need to share read-only calendars with other people, so the read-only feed was purely a stepping stone toward CalDAV. Most of its surface — the per-User capability feed token, the feed-settings API, and the `webcal://` subscribe UI — would be discarded once CalDAV's HTTP Basic / app-specific-password auth and account-based onboarding replace it. The only genuinely reusable piece, the iCalendar renderer (`Event` → `VEVENT`), will be built as part of CalDAV regardless.

We accept a longer time-to-first-sync (weeks, not days) in exchange for zero throwaway work.

## What this pulls into scope

CalDAV requires work the read-only feed deliberately avoided, to be settled in a dedicated grilling/spec pass:

- **Schema expansion** on Events — description, location, recurrence (`RRULE`), all-day, per-event timezone, and alarms (giving the `Reminder` concept a persisted home).
- **An auth path native clients accept** — HTTP Basic/Digest, ideally app-specific passwords, distinct from the web app's JWT + refresh-cookie flow.
- **The CalDAV/WebDAV protocol surface** — `PROPFIND`, `REPORT` (calendar-query / calendar-multiget), `PUT`/`DELETE` on event resources, principal and calendar-home discovery, and `ctag`/`etag`/`sync-token` change tracking. Library (`emersion/go-webdav`) vs hand-rolled is an open decision.
- **Ingesting client-authored iCalendar** back into the domain model, with `ETag`/`If-Match` conflict handling.
