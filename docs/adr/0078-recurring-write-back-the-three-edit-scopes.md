# Recurring Write-back: the three edit scopes

Status: accepted — extends ADR-0075 (Write-back) and ADR-0077 (create/delete Write-back)

The three edit scopes — **This event**, **This and following**, **All events** —
work on a writable Linked Calendar exactly as they do on an ordinary one. ADR-0075
already chose how each maps onto the Provider; this ADR records how that mapping
is built, and the one place it is genuinely uneven.

## The mapping onto Google

- **All events** → `events.patch` on the master. Unchanged from ADR-0075/#290.
- **This event** → `events.patch` on that instance. A new or edited Override.
- **Delete this event** → `events.patch { status: "cancelled" }` on that
  instance. An `AddException`, or deleting an existing Override.
- **This and following** → no Provider call of its own. Performed by hand.

Three of the four are a single call. The frontend already compiles each scope
into ordinary `create` / `update` / `addException` / `reparentSeries` requests
(ADR-0016's Series operations), so the backend's whole job is to stop refusing
those on a Connection Source and queue the right push.

## Instance ids are fetched, never constructed

An Override stores only its series' shared `ExternalUID` (the master's Google id)
and its own `RECURRENCE-ID`. To patch one instance we need that instance's own
composite Provider id, which is not in our store the first time.

We **call `events.instances`** filtered by `originalStart` and read the id (and
the If-Match etag) off the single result. The documented shortcut — building
`{eventId}_{basic-format-timestamp}` by hand — works right up until an all-day
series (the timestamp is a date, not a datetime) or a DST-crossing occurrence
(the wall-clock offset shifts under a fixed UTC instant), which is a bug that
only ever surfaces in someone's real data. `formatGoogleOriginalStart` reuses
`encodeGoogleEventDateTime` verbatim so the filter value byte-matches how the
same instant is encoded everywhere else.

## A cancellation is a `status: cancelled` instance, never an `EXDATE`

Google represents "this Occurrence is gone" two ways: a cancelled instance
object, and an `EXDATE` entry in the series' `recurrence` array. Both exist;
mixing them drifts until a series ends up cancelled twice. We chose the instance
representation once:

- `CANCEL_INSTANCE` patches the instance to `status: cancelled`.
- A **master edit's `recurrence` array is exactly the one `RRULE` line** —
  `encodeGoogleRecurrence` no longer folds in stored Exdates, and
  `buildGooglePatch` lost its `exdates` argument. A local Exdate that this app
  reconciled in from Google originated as a cancelled instance there and stays
  one; re-sending it as an `EXDATE` line would be the double-representation the
  ADR forbids. (A pre-existing Google `EXDATE` written by some other CalDAV
  client would be dropped on the first master edit here — an accepted cost of
  choosing one representation, and `AddException` writes a cancelled instance for
  every deletion this app makes.)

## "This and following" is a hand-built split

There is no Provider operation for it. The frontend dispatches three calls, and
each already writes back on its own:

1. `update(oldMaster, rrule += UNTIL)` → an ordinary master `events.patch`.
2. `create(newMaster)` → `events.insert`; the returned id is adopted onto the
   re-anchored local series (`AdoptProviderIdentity`), exactly as #292's create.
3. `reparentSeries(oldMaster, newMaster, splitStart)` → a **purely local** row
   move. `ReparentFrom` queues nothing of its own.

It is the only scope that is not one call, and the only one whose failure leaves
a **half-split** series: the old series bounded at Google, the new one not
created. This is detectable and recoverable, not silently left behind — a
permanently failed `events.insert` marks the new Master with the ADR-0075
write-back-error marker and raises its Source into needs-attention, and editing
that Master re-enqueues the insert (the retry path #292 already built). What
stays refused is a `ReparentFrom` that would **cross the Provider boundary** (one
side a Linked Calendar, the other not): that needs a Create- or Delete-shaped
push this app does not build for that direction.

## The outbox rows

Two methods join `PATCH`/`POST`/`DELETE` on a `writeback` row (migration
`00005`): `INSTANCE` and `CANCEL_INSTANCE`. Each carries an
`OutboxWriteBackInstanceSnapshot` (the Calendar id, the Master's local id, the
Occurrence's `RECURRENCE-ID`, and the Override's local id when there is one) in
the shared `snapshot` column.

Their **`event_id` column is the Master's local id**, not the Override's. This is
load-bearing: the reconciler's pending-set protection (ADR-0076) is keyed on
`MasterID`, so an instance push in flight protects the **whole series** from a
Full Refresh reverting it or a Delta batch re-applying Google's stale copy —
with no change to the reconciler at all. `SendWriteBack` rebuilds an `INSTANCE`
push from the Override's own live fields at send time (like a `PATCH`); a
`CANCEL_INSTANCE` needs only the `RECURRENCE-ID` and the master's `ExternalUID`.

## Known limitation: "All events" that discards children

An **All events** edit that changes the *recurrence pattern* (not just its
`UNTIL`/`COUNT`) forces the "All events" scope and discards the series' local
Overrides and Exceptions (ADR-0016) — the frontend warns first. On a Linked
Calendar that discard is local only: the push is the master `events.patch` with
the new `RRULE`. Google itself drops modified/cancelled instances that no longer
fall on the changed rule, but instances that *still* match it survive there, and
the next Full Refresh re-creates the discarded local rows from them.

This is not fixed here. Cleaning it up means enumerating every Provider instance
and cancelling the ones the User discarded — its own piece of work, outside this
ticket's acceptance criteria, and reachable only through a path the frontend
already gates behind an explicit "this removes your changes to individual events"
warning. It is recorded so it is a known edge, not a surprise.

## Consequences

- **`ErrLinkedCalendarWriteUnsupported` shrinks** to the writes that are still
  genuinely unbuilt: an Attendee on a create, a CalDAV-shaped `ImportSeries`
  write, an Event moved across a Connection Calendar's boundary, and a
  boundary-crossing `ReparentFrom`.
- **An instance push that races its own series' create** (a scoped edit made
  before the `events.insert` drained) returns a retryable error until the master
  has an `ExternalUID`. In the ordinary case the insert is a lower outbox id and
  drains first; the outbox Worker's per-`event_id` dedup also serializes the two.
- **Echo suppression** stores the fresh instance etag on the Override row after
  an `INSTANCE` push; a `CANCEL_INSTANCE` needs none — Google's returned
  `status: cancelled` instance maps straight back to the Exdate the local series
  already holds, so the next Refresh is a no-op.
