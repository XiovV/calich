# Create and delete Write-back, and the queued-delete snapshot

Status: accepted — extends ADR-0075 (Write-back) and ADR-0076 (a pending Write-back is invisible to the reconciler); scoped recurring edits extend it further in ADR-0078

Creating and deleting an Event on a writable Linked Calendar now pushes to the
Provider, alongside the edit push ADR-0075 shipped. A create queues
`events.insert`; a delete queues `events.delete`. Both ride the same `outbox`
as the PATCH, drained by the same Worker.

This is the other half of ADR-0075's "a Linked Calendar is writable" — and it
is what makes ADR-0076's central hazard actually reachable, because a create
is the only thing that produces an Event the Provider has never heard of.

## Create: POST with a client id, then adopt it

A locally created Master commits with **no `ExternalUID`**. The push is an
`events.insert` carrying the same field-scoped `googleEventPatchBody` a PATCH
does — so an insert can no more express an Attendee, a `conferenceData` block
or a visibility change than an edit can.

The insert supplies a **client-generated id** (`googleClientEventID` — a hash
of the local Event id, always inside Google's `base32hex` id grammar). This
makes it idempotent: an insert that reaches Google but whose response is lost
would otherwise be retried by the outbox and mint a *duplicate* event; with a
client id the retry answers `409` instead, which `insertEvent` resolves to
the already-created event. The id Google returns is **adopted onto the local
row** (`EventRepository.AdoptProviderIdentity`), applied as a Refresh result
exactly as ADR-0075's PATCH echo is, so the next Refresh reconciles by it.

Scoped to a **plain Master**: an Override needs a Provider instance id this
app does not resolve yet (ADR-0075's `events.instances` note), and a Linked
Calendar's Events carry no Attendees this app mirrors (ADR-0052), so a create
naming one is refused rather than silently dropping the guests.

## Delete: DELETE, from a snapshot

A deleted Master's local row is **gone** by the time the push drains. Unlike a
PATCH or POST — which `SendWriteBack` rebuilds from the Event's own live state
— a DELETE row therefore carries a snapshot: the Calendar's id and the
Master's `ExternalUID` and last-known etag. This mirrors the mail `CANCEL`
snapshot (ADR-0059) for the same reason: a withdrawal must outlive what it
withdraws. The Calendar itself survives the Event's deletion, so its Source
and Connection are still re-resolved from the snapshot's `CalendarID` at send
time rather than frozen into it.

`events.delete` is sent **unconditionally** — no `If-Match`. A delete is a
strong intent and there are no fields left to preserve from a concurrent
Provider edit the way a PATCH's `If-Match` guards. A `404`/`410` is success:
the end state the User asked for already exists.

A DELETE is queued for **every** Master deleted on a writable Linked Calendar,
even one whose `events.insert` has not drained yet — its Provider id is the
same deterministic `googleClientEventID`, so the delete cleans up an event the
insert created (or is about to), and no-ops on Google's `404` for one that was
never sent. This is what closes the create/delete race (below).

If the push cannot be delivered — the Calendar is no longer writable, or its
Connection is gone — `sendWriteBackDelete` raises the Source's
needs-attention marker rather than returning a silent success. Unlike a
PATCH/POST that can't be delivered (a genuine no-op), a silently dropped
delete is *undone* by the next Refresh, which still lists the event.

Deleting one Occurrence of a recurring series (an Override, or an Exception
via `AddException`) was parked here — `PATCH status: cancelled` against an
instance id this app did not resolve yet — and is built out in ADR-0078.

## The pending set, precisely

ADR-0076 established that a series with a queued push is **subtracted from the
reconciler's input** before absence is computed. Create and delete each add a
case:

- **A queued create** — the row has no `ExternalUID`, so it never appears in
  the reconciler's `existing` set (which is keyed by `ExternalUID`) and can
  never be a tombstone candidate. The protection is structural, not a check.
  The pending-edit set still lists its local id, harmlessly.
- **A queued delete** — the row is gone, so it is not in `existing` either,
  and cannot be tombstoned. But a Full Refresh still listing the series, or a
  Delta batch carrying an ordinary update for it, would land in the
  reconciler's "no stored counterpart → new series" branch and **re-create
  the Event the User just deleted**. So the set of `ExternalUID`s with a
  pending `events.delete` — read from the DELETE rows' own snapshots, not a
  query over `events` — is dropped from the incoming series before that branch
  runs. Two properties are load-bearing:
  - **Scoped to the reconciling calendar.** A Provider assigns one event id
    to every attendee's copy of an invited event, so an unscoped set would
    drop — and, in Full mode, *tombstone* — a live series sharing that id in
    another Linked Calendar, possibly a different User's. The snapshot carries
    the Calendar id for exactly this filter.
  - **Not folded into the unparseable set.** There is no stored series for
    that to protect, and marking it would suppress a genuine same-id series.

Both sets derive from `outbox` rows in `pending` status, so queued, retrying
and mid-backoff all count, and a permanently `failed` row falls out:

- A permanently failed **create** leaves a local-only Event carrying the
  ADR-0075 write-back-error marker. With no `ExternalUID` it stays invisible
  to the reconciler — tombstoning freshly authored content Google merely
  rejected would be worse than surfacing the marker and letting the User
  retry (edit) or delete it.
- A permanently failed **delete** stops protecting its `ExternalUID`, so the
  next Refresh re-creates the series from the Provider's still-live copy. That
  is correct: the delete never reached Google, and the Source is raised into
  needs-attention (the only surface left, since there is no Event row to
  mark).

## Consequences

- **This needs its own table tests, because nothing else will find them** —
  exactly as ADR-0076 said of the edit case. Two assertions are the executable
  form of this document: one locally created Event with a pending push,
  meeting a Full Refresh over a listing without it, produces **zero**
  tombstones; and a queued delete, meeting a Delta Refresh carrying an update
  for that series, does **not** resurrect the Event.
- **The DELETE row supersedes any pending PATCH/POST for the same Event.**
  `EnqueueWriteBackDelete` drops them first: an edit or a not-yet-sent create
  is moot once the Event is deleted, and leaving the row would push a change
  for an Event the DELETE is about to remove.
- **The create/delete race is closed, not merely accepted.** Because the
  create push uses a deterministic Provider id and `Delete` always queues a
  DELETE against that same id, an `events.insert` in flight at the instant a
  local delete commits is cleaned up by the DELETE that follows it — whether
  the insert lands before or after the delete. `AdoptWriteBackIdentity`
  finding its row already gone is a no-op, not an error, for the same reason.
- **Editing a never-pushed Master re-enqueues its create.** `Update` on a
  linked Master with no `ExternalUID` yet enqueues an `events.insert` (not a
  PATCH) and clears any stale failure marker — the User's way to retry a
  create push that permanently failed.
