# The iCalendar codec takes a serialization target

Status: accepted — amends ADR-0031

ADR-0031 extracted one iCalendar codec shared by CalDAV and ICS import/export, and rejected a caller flag in terms that were meant to last: *"a codec producing two different byte-streams depending on who asked is two encoders wearing a trenchcoat, and they would drift."*

Attachments (ADR-0040) break the premise that one byte-stream can serve both callers. **The codec gains an explicit serialization target — CalDAV or Calendar file — and the difference is confined to the `ATTACH` property.**

## Why the premise fails

ADR-0031's rule holds when the two outputs *can* be identical. For attachments they cannot be, by construction:

- Over CalDAV, `ATTACH` carries an RFC 8607 managed URI. The `MANAGED-ID` and the attachments server URL are scoped to the server that minted them and mean nothing anywhere else.
- A Calendar file is defined in `CONTEXT.md` as *"a standalone `.ics` document exchanged with the world outside this instance"*. Standalone is the operative word. An export whose `ATTACH` points at `https://your-calendar.example/dav/attachments/…` is not standalone — the recipient gets a URL they cannot fetch and usually cannot even reach.

Emitting the managed URI in both would have preserved ADR-0031 to the letter while contradicting the glossary's definition of the thing being produced. There is no single byte-stream that is both a valid sync object and a valid file to hand to a stranger.

## What actually differs

**A Calendar file inlines the bytes** — `ATTACH;ENCODING=BASE64;VALUE=BINARY;FMTTYPE=…;FILENAME=…`. **A Calendar object references them** — `ATTACH;MANAGED-ID=…` with the URI value.

Everything else — `VTIMEZONE` emission, `RRULE`, `RECURRENCE-ID`, `VALARM`, all of it — comes from the same unconditional path it does today. The target is a named parameter on the encoder, not a boolean at a call site: ADR-0031's anti-drift argument was really a warning about hidden, incidental divergence, and one property varying by an explicit, documented target is not that.

## The inline ceiling is the import cap

An Attachment larger than `maxImportUploadBytes` (10MB) is **omitted from the export and reported**, not inlined.

Choosing the same number as the import cap is the point: **a file this instance produces is a file this instance could accept back.** Any other ceiling makes the two operations quietly asymmetric, and asymmetry between export and import is exactly what turns "migrate my calendar" into a support question.

Uncapped inlining was rejected on size — with ADR-0040's 25MB × 10 limits, one series could produce a 330MB `.ics`, and "export all Calendars as a zip" multiplies that by the number of Calendars.

## Export gains a summary, and it is a pre-flight

ADR-0030 made the Import summary part of the contract rather than a diagnostic, on the grounds that *"a human is watching a one-time act"*. That reasoning applies identically to export, which until now had no way to say anything at all. Silently omitting a 30MB attachment from a file the user is about to hand to a colleague is precisely the kind of invisible loss ADR-0030 decided against for the other direction.

But "mirror the Import summary" does not survive contact with the code, and saying so is the point of this section. Import has somewhere to put a summary because it has a confirm step: dry run, preview, confirm. **Export is one click straight to a download.** A summary shown afterwards arrives when the file is already in the user's Downloads folder and there is nothing left to decide.

So the Export summary is a **pre-flight**: a metadata-only check runs before anything is generated, and

- if nothing will be dropped, the download proceeds exactly as it does today — no dialog, no toast, no new friction on the overwhelmingly common path;
- if something will be dropped, a dialog names it and asks the user to continue.

Disclosure lands while the user can still act on it, which is what made the Import summary worth building. An export that loses nothing should not have to say so.

A post-hoc toast was rejected as transient — miss it and you hand over an incomplete file believing it complete. A manifest inside the archive was rejected as unreachable in the case that matters most: a single-Event `.ics` from the event modal is not a zip and has nowhere to carry one.

## Consequences

- **Import ingests inline `ATTACH`** under ADR-0040's size and count limits, and ignores URI `ATTACH` — fetching a foreign URL server-side is a request-forgery surface, and the target usually needs credentials anyway. Both outcomes are counted in the Import summary. Without this the symmetry above is a slogan rather than a property.
- **A same-instance export/import round trip still is not a restore.** ADR-0030 stands: fresh UUIDs are minted, nothing is deduplicated, and re-importing your own export produces a second copy of everything — now including a second copy of every attachment's bytes.
- **CalDAV output is unchanged by this ADR.** No ETags churn, unlike ADR-0031's own rollout.
- **The next request for a codec flag has a precedent to point at.** That is the real cost of amending ADR-0031, and it is accepted knowingly. The bar this decision sets is not "the caller would prefer different bytes" but "the two outputs cannot be identical because one of them is scoped to this server" — a test that attachments pass and that almost nothing else will.
