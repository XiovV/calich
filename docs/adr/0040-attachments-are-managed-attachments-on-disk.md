# Attachments are RFC 8607 managed attachments, stored on disk, held by the Master

Status: accepted

An Event can carry files. The bytes live in the Data directory, the metadata lives in SQLite, and CalDAV exposes them through **RFC 8607 managed attachments** rather than through raw `ATTACH` properties.

This is the first blob storage in the app and the first CalDAV extension implemented beyond the core protocol. Both are deliberate; the cheaper options and why they lost are below.

## An Attachment belongs to the Master, not to an Occurrence

An Attachment hangs off the Master row and is shown on every Occurrence of the series. An Override cannot carry one of its own.

This costs a real job — "this week's slides" on one Occurrence of a weekly standup is unrepresentable. It buys the absence of an entire category of confusion. An Override is created the moment anyone edits a single Occurrence's title; if attachments were per-row, that Occurrence would silently start with no files while its siblings kept theirs, for no reason a user could see. Attaching would also need the This event / This and following / All events scope picker, and every operation in `seriesOperation.ts` would have to carry attachments correctly through re-anchoring and splitting.

It also lines up with ADR-0025, where one `.ics` is one series and Overrides are never independently addressable.

## Why RFC 8607 rather than plain `ATTACH`

The alternatives were all worse in a way that mattered:

- **Nothing over CalDAV — web app only.** Cheapest by far and consistent with ADR-0026's stance on unmodelled data. Rejected because a synced device would show an Event with no hint a file exists, which is the failure users report as "the attachment disappeared".
- **Plain `ATTACH` with a URI back to this server.** A string in the serializer. Rejected because there is no defined way for a client to authenticate that fetch, so the link 401s for most users — and a link that fails is worse than no link.
- **Plain inline `ATTACH;ENCODING=BASE64` over CalDAV.** Works everywhere with no auth problem at all. Rejected on sync cost: a 5MB PDF becomes ~6.7MB of base64 inside the series' object, refetched by every device on every unrelated edit to that series, and every ETag churns.

RFC 8607 exists precisely because those three are the only alternatives and all three are bad. It costs a real protocol surface, and it is the one option where a native client can both see the attachment and fetch it with credentials it already has.

`CALDAV:managed-attachments-server-URL` is advertised on the calendar home collection as a path-only `DAV:href`; the client substitutes scheme and authority from its own request, so **the server never needs to know its external base URL** and no new configuration is required.

**Amendment (#142):** that substitution rule is specific to a WebDAV `href` returned inside a PROPFIND response — it does not extend to `ATTACH` itself. `ATTACH`'s `VALUE=URI` is plain iCalendar text sitting in a `.ics` body with no request for a client to resolve a bare path against, so a native client that receives one (rather than following `managed-attachments-server-URL` itself) has no host to fall back on. `ATTACH` is therefore built as a fully-qualified URL, deriving scheme and host from the request that fetched the calendar object (`Host`, `X-Forwarded-Proto`/`X-Forwarded-Host`) — still no server configuration, since the source is the request, not a setting. `managed-attachments-server-URL` itself is unchanged.

## Storage on the Master, serialization onto every VEVENT

RFC 8607's `rid` query parameter is per-instance, which collides with Master-only storage. The resolution:

- Storage is one row on the Master.
- The encoder writes `ATTACH` onto the master VEVENT **and every Override VEVENT** in the Calendar object.
- `attachment-add`/`attachment-remove` with `rid` absent — "all recurrence instances", and the common client default — is therefore honoured exactly.
- `rid=M` is accepted and treated as the series.
- A specific `RECURRENCE-ID` `rid` is **rejected with a precondition error**. An explicit, documented deviation rather than a silent one.

Serializing onto the master alone would have been strictly correct for `rid=M` and simpler in the encoder, but a client expanding the series would then see the attachment vanish on exactly those Occurrences that happen to have an Override.

## A PUT never changes an Attachment

Only the RFC 8607 POST actions add, update or remove one. An incoming `ATTACH` carrying a `MANAGED-ID` this instance did not mint for that series is ignored, exactly as ADR-0026 handles every other unmodelled property, and a `PUT` that omits the `ATTACH` properties does not delete anything.

This closes RFC 8607 §3.7's reuse-across-resources path, which would let a client reference one Attachment from a second series. Supporting it would mean splitting the blob from the reference, refcount-aware deletion, and enforcing the RFC's "only the creator may reuse" rule. **One Attachment, one Master** — attaching the same file to two Events stores it twice, which on a self-hosted instance is a fair trade for a sweeper that only has to ask "does a row exist".

`PutSeries` already updates the master row in place rather than deleting and recreating it, so the rows survive a `PUT` without further work. That is load-bearing and should have a regression test.

## The bytes

```
DATA_DIR/attachments/<first two hex chars of uuid>/<uuid>
```

The UUID is the Attachment's id, its file name, and its `MANAGED-ID` — one value, so there is no mapping to keep honest and no path column to drift. The user's filename is metadata, used only in `Content-Disposition`, so nothing user-supplied ever reaches the filesystem: no traversal, no collisions, no unicode normalization.

Content-addressed storage (`sha256`) was considered for the dedupe. Rejected: it makes the id-to-blob mapping non-1:1, forces refcounting before anything can be deleted, and turns `attachment-update` into "write a new blob" — all to save disk on an instance where disk is the thing the operator already controls.

Mirroring the domain on disk (`attachments/<calendar>/<event>/<filename>`) was rejected for the filename sanitization it drags in, and because moving an Event between Calendars would become a file move that can half-fail.

## The database is the truth; a sweeper reclaims the rest

There is no transaction spanning SQLite and the filesystem, so the ordering is chosen to make the survivable failure the likely one:

- **Upload**: write to a temp file, rename into place, *then* commit the row. A crash leaks bytes. It never leaves a row pointing at a file that isn't there.
- **Delete**: commit the row deletion, *then* unlink best-effort.
- **Sweep**: at startup and on a daily tick, remove files with no row.

The sweeper is not a safety net bolted on — it is the mechanism. Calendar deletion, account deletion (ADR-0037) and series deletion all cascade `events` rows inside SQLite, where no Go code sees which files just became garbage. Requiring every one of those paths to pre-query and unlink would mean any path that forgets leaks silently and forever with nothing to catch it. Letting the cascade happen and sweeping afterwards is the only version that stays correct as more cascade paths get added.

Worst case is wasted disk, which is recoverable. A missing file for a live row is not.

## Access, limits, and serving

**Access** follows ADR-0034 unchanged: Owner and Editor add, update and remove; Viewer downloads; a Subscribed Calendar clamps to read-only and can hold none. Attaching a file is editing an Event, which is what the Editor Role already grants — matching RFC 8607's "restricted to only those calendar users who have access to that calendar object". Its organizer/attendee clauses are moot here, since ADR-0026 drops `ATTENDEE`/`ORGANIZER` entirely. `uploaded_by` is attribution only, `ON DELETE SET NULL`, exactly as `events.created_by` is.

**Limits** are `MAX_ATTACHMENT_SIZE` (default 25MB) and `MAX_ATTACHMENTS_PER_EVENT` (default 10), advertised as RFC 8607's `max-attachment-size` and `max-attachments-per-resource`. Env-configurable rather than the hardcoded const `maxImportUploadBytes` set as precedent, because disk is the one resource that varies wildly between self-hosters and is already the thing they configure. No total-instance quota: a full disk is a self-hoster's own problem in a way it is not for a hosted service.

**Serving** is two routes over one storage read — `/dav/attachments/{managed-id}` under `RequireCalDAVAuth`, and an `/api` route under `RequireAuth` for the web app via the existing `downloadBlob` helper. Each stays in the auth world the router already separates deliberately; the alternative was a hybrid authenticator accepting both Bearer and Basic.

Every response carries `Content-Disposition: attachment`, `X-Content-Type-Options: nosniff` and `Content-Security-Policy: sandbox`, with the stored media type preserved so saved files keep their association and `FMTTYPE` stays honest. **Nothing user-uploaded ever renders inline from this origin.** An uploaded `.html` or `.svg` served inline is stored XSS against the app itself, and an allowlist of "safe" inline types is a place where being wrong once is a full-origin compromise. The web app can still show image thumbnails: it has already fetched the blob, and a blob in an `<img>` executes nothing.

## Consequences

- **The Data directory now holds data the database cannot describe.** Until now, `calendar.db` was the backup story (ADR-0030 says so explicitly). It no longer is on its own — a backup that copies the database and not `attachments/` silently loses every file. This needs saying in the README.
- **Uploads happen after the Event exists.** The web app mints the Event UUID client-side already, but the row must exist before an upload can reference it, so the modal holds `File` objects in memory and uploads them after the create POST succeeds. Cancelling uploads nothing; a failed upload against a saved Event is reported per-file and retried in place. No staging area, and so no second place bytes can live and no sweeper for modals nobody submitted.
- **One upload path uses `XMLHttpRequest` rather than `fetch`.** A deliberate deviation, recorded so nobody "fixes" it: `fetch` cannot report upload progress in Safari or Firefox, and a 25MB upload runs tens of seconds on a home connection — an indeterminate spinner that long reads as a hang and gets the modal closed mid-upload. It is the only such call in the app.
- **The event modal gains a scroll container.** It is a fixed-width popup with no `max-height` today, so a recurring Event with several Reminders already overflows the viewport unrecoverably. Attachments make a latent bug acute rather than causing a new one.
- **Refresh ignores `ATTACH` entirely** (ADR-0033). Import ingests attachment bytes because it is a one-time act with a human watching and an Import summary to disclose what happened; a Refresh is an unattended hourly poller against a URL the instance does not control, with no human, no disclosure channel, and no bound on what it would pull.
- **Export is not covered here.** A managed attachment URI is meaningless outside the server that minted it, so Calendar files need a different representation — see ADR-0041, which amends ADR-0031 to allow it.
- **macOS Calendar (#143) can view and download managed attachments but has no UI to add one.** Its CalDAV client follows `managed-attachments-server-URL` and renders `ATTACH` correctly, so the server side of RFC 8607 is exchanging data with it as designed. Apple's own event-editing UI simply never exposes an "Add Attachment" control over CalDAV, regardless of server support. This is a native-client limitation, accepted as such — not a server gap to re-investigate.
