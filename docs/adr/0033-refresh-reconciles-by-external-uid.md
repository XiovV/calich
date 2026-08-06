# Refresh reconciles by External UID

Status: accepted

A Refresh does not replace a Subscribed Calendar's Events wholesale. It stores the foreign iCalendar `UID` of each series as an **External UID** and reconciles the fetched Calendar file against it series by series, so an unchanged series is left untouched. A conditional GET sits in front of the whole thing, so the common case — a feed that has not changed — costs one `304` and zero writes.

## Why

The obvious implementation is delete-everything-and-reinsert, and it is genuinely simpler: series reconstruction from a Calendar file is a solved problem, and correctness is trivial because there is no prior state to be wrong about.

It is also the wrong unit. Every Event would take a fresh id on every Refresh, so `change_seq` would bump on all of them, and CalDAV's change tracking (ADR-0025) would report the entire collection as deleted-and-recreated on every cycle. A phone subscribed to a 5,000-Event feed would re-download all of it, plus a tombstone per old object, every time the publisher touches one Event. Gating full replacement behind a conditional GET only reduces how *often* that happens; a feed that changes daily still resyncs completely, daily.

Reconciling by `UID` costs a column, a unique index, and a diff. ADR-0030 predicted precisely this shape when it ruled backup/restore out of scope: *"most likely via an `external_uid` column and upsert-on-import, not by changing the id."*

## Decisions that follow from it

- **The row id stays a minted UUID; the foreign `UID` goes in its own column.** ADR-0030's actual invariant is preserved — an Event's id is its CalDAV `UID` and its `{masterId}.ics` path segment, and the API rejects non-UUID ids. Foreign UIDs (`…@google.com`, Outlook's 200-character hex) never reach a URL. Only Subscribed Calendars populate the column; it is unique per Calendar, not globally, since two feeds may legitimately carry the same `UID`.
- **The series is the reconciliation unit,** matching ADR-0025's Calendar object exactly. Diff granularity and CalDAV resource granularity are the same thing, so a changed series produces exactly one changed CalDAV object. Overrides within a series match on `UID` plus `RECURRENCE-ID`.
- **Reconciliation has three buckets, not two.** *Parsed* → upsert. *Absent from the feed* → tombstone. *Present in the feed but unparseable* → *leave the existing rows alone* and count it as skipped. The third bucket is the whole reason to write this down: ADR-0030's per-series tolerance and this ADR's tombstone-what-is-missing rule, composed naively, silently **delete** an Event the moment a publisher emits one malformed `VEVENT` — the skip counter being its only trace. Unparseable is not absent.
- **Freshness is decided by a server-side poller, never by a client.** Hourly by default, backing off to the publisher's `REFRESH-INTERVAL` (RFC 7986) or `X-PUBLISHED-TTL` when it asks for less, staggered so many Subscriptions do not fire on the same tick. Refreshing lazily on web-app read was rejected for the same reason proxying was in ADR-0032: it makes data freshness depend on the observer, and Reminders and CalDAV clients are not observers.
- **Failure preserves the last good state and never disables the Subscription.** Exponential backoff to a ~24h ceiling, reset on success, always bypassed by a manual refresh. A feed that 404s for a week may be a publisher outage; deleting a User's Subscription and its Events on that evidence is unrecoverable. Errors are surfaced in two classes, because they need different sentences: *needs attention* (401, 404 — a human must fix something) and *retrying* (timeout, 5xx, DNS).
- **The whole feed is stored, with no ingest window.** Bounding at ingest sounds cheap until the boundary moves: a sliding window ages Events out on its own, so every Refresh emits tombstones for Events that did not change and that the publisher still lists — which CalDAV clients correctly interpret as real deletions, forever. The 10MB fetch cap inherited from import is the practical bound.

## Consequences

- **Reconciliation is where the bugs will be.** It is written as a pure function over (existing series, incoming series) returning upserts, skips, and tombstones — no database, no clock — so the three-bucket rule can be table-tested exhaustively. This follows `reminder.DueAll` and `recurrence.Expand`.
- **This is the second feature to lean on #80's deferral, and it leans harder.** Import was a deliberate act on data the User chose; a Subscription can add several thousand Events to the unwindowed `fetchEvents` payload because someone pasted one URL. #80's analysis still holds — client-side expansion cost scales with *recurring Masters*, not total Events — but the 5,000-Event check should be re-run with a fat Subscription in place.
- **Manual refresh must bypass the conditional GET's short-circuit as well as the backoff,** or "Refresh now" on a feed the server believes is unchanged does visibly nothing, which reads as a broken button.
