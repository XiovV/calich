# Shared Calendars appear in the accessor's own CalDAV home-set

Status: accepted

A Calendar reached through a Share (ADR-0034) appears in the accessing principal's **own** calendar-home-set, not the Owner's. Bob's home-set lists every Calendar he has Access to, owned or shared, each under `/dav/{bobID}/calendars/{calendarID}/`.

The same Calendar therefore has one path per principal with Access to it — `/dav/1/calendars/family-uuid/` for Alice and `/dav/2/calendars/family-uuid/` for Bob — backed by one row. Neither principal can see the other's path, so the duplication is invisible to clients.

## Why

This is what the code already does. `toCalDAVCalendar(userID, c)` builds paths from the **authenticated** user's id rather than the Calendar's owner, and `calendarIDFromPath` / `calendarObjectIDFromPath` validate an incoming path against that same authenticated prefix. The moment `CalendarService.List` returns shared Calendars, they appear in the right place with no path changes at all — and the prefix check keeps rejecting attempts to address another principal's namespace, for free.

The read-only half is equally cheap. `applyPrivilegeSetPatch` (`caldavserver/propfind.go`) already overrides go-webdav's hardcoded read+write `current-user-privilege-set` down to `<read/>` when `cal.SourceURL != nil`. A Viewer needs exactly that; the condition widens from "is Subscribed" to "Access is not writable," which is the clamp ADR-0034 already defines. One expression, both cases.

Native clients need no sharing support whatsoever — a shared Calendar is just another collection in the home-set.

## Considered Options

- **Accessor's home-set (chosen).** Zero path changes, no client-side sharing support required, isolation preserved by the existing prefix check.
- **Stay in the Owner's home-set, accessed cross-principal.** Closer to how RFC 6638 and Apple's sharing extensions model it, and it gives a shared Calendar one canonical URL. Rejected: `calendarIDFromPath` would have to accept other users' prefixes, discarding an isolation guarantee currently obtained for free, and real client support for cross-principal collections varies far too much to rely on for the primary path.

## Consequences

- **Collection properties legitimately differ per principal.** `current-user-privilege-set` already does under this ADR, and `calendar-color` will under ADR-0038. This is normal CalDAV and touches nothing in ADR-0025: ETag and CTag track *objects*, and the objects themselves stay byte-identical for every principal (ADR-0036).
- **A client that syncs two accounts against this instance sees the shared Calendar twice**, once per account, as two unrelated collections. That is inherent to per-principal home-sets and is what every server doing it this way produces.
- **Ownership is not expressible over CalDAV.** A client cannot tell that Bob's Family is Alice's Calendar, nor act on it — Owner-only operations (rename, delete, re-share, set source URL) stay web-app actions, consistent with ADR-0032 keeping renaming and recolouring out of PROPPATCH for Subscribed Calendars.
