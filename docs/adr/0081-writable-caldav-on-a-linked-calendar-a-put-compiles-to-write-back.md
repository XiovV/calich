# Writable CalDAV on a Linked Calendar: a PUT compiles to Write-back pushes

Status: accepted — extends ADR-0075, ADR-0077, ADR-0078 (Write-back) and ADR-0080 (Exposure); the two blockers it depends on (#297, #298) have shipped

The Owner of an exposed, writable Linked Calendar can now edit it from a real CalDAV client — a phone, a desktop calendar app — with every change carried to Google by the Write-back machinery ADR-0075/0077/0078 already built for the web app. `PutSeries`, CalDAV's own write path, used to refuse a Connection Source outright, independent of Exposure or Mode (ADR-0074's original posture, echoed unchanged when #299 was parked). That refusal is now conditional: it proceeds where Exposure is on and Access can write, and still 404s where it isn't — matching the direct-path posture ADR-0080 already established, rather than confirming the Calendar exists and refusing the write on it.

## The core problem, and why it needs a diff

A CalDAV `PUT` replaces a **whole Calendar object** — Master, every Override, every Exdate — every time, even when a phone's own scoped edit ("this event", "this and following") touched only one Occurrence. The web app instead compiles a scoped edit into ADR-0016's Series operations and queues one push per operation. CalDAV has no such vocabulary: the object arriving is the *result* the client wants, not the steps that produced it.

So `PutSeries` diffs the incoming `SeriesWrite` against the series' currently stored state, and derives Write-back pushes from what actually changed — never from what a naive "push on every PUT" would send. Without the diff, editing one Occurrence from a phone would also re-push an unchanged Master on every save, because the resent object still carries the Master's own fields even though they didn't change.

## The six deltas

Four map onto Write-back methods that already exist, unchanged from ADR-0075/0077/0078:

| Delta | Push |
| --- | --- |
| Master's own pushed fields changed | existing master `PATCH` (or `POST`, if it has never reached the Provider) |
| Override added or changed | existing `INSTANCE` |
| Exdate added | existing `CANCEL_INSTANCE` |
| Series newly created | existing `POST` |

Two are refused outright, before `upsertSeries` writes anything:

| Delta | Disposition |
| --- | --- |
| Override removed, and its Occurrence isn't also newly excepted | `403`, refused |
| Exdate removed | `403`, refused |

**The two refusals happen before the local write, not after.** A partially-applied `PUT` that pushed some deltas and dropped others is the exact failure this design exists to prevent — refusing up-front (`buildPutSeriesWriteBack`, called before `upsertSeries` ever runs) leaves the client holding its own copy and the store untouched. Both are expressible at Google (patch an instance back to the master's values; patch a cancelled instance back to confirmed) and both are deliberately deferred: they are the two rarest things a phone does, and adding them later does not change the diff's shape.

**Removing an Override is not always a revert.** The standard CalDAV shape for "delete this already-modified Occurrence" is exactly what a naive reading of "Override removed" would refuse: the Override VEVENT drops out of the object, and an `EXDATE` line appears for its `RECURRENCE-ID` in the same `PUT`. That combination is not a revert-to-rule request — it's an ordinary cancellation, and the diff recognizes it as one: an Override missing from the incoming write is refused only when its `RECURRENCE-ID` isn't *also* a newly-added Exdate. When it is, the Exdate's own `CANCEL_INSTANCE` push already covers it, and the Override row is simply deleted locally (`upsertSeries`' existing cleanup, unchanged).

## Exposure gates the write, not just the read

`PutSeries` resolves Exposure (ADR-0080) the same way `AccessWithExposure`/`RequireExposedAccess` already do for reads: absent from the caller's own home-set, a `PUT` to the collection 404s — it was never there to write into. This is why the answer is `repository.ErrNotFound`, not a 403: a Calendar the caller can't see behaves identically whether the write is refused or the Calendar never existed. Once Exposure says yes, the ordinary write guard (`requireWritableCalendar`'s Access clamp, `requireLiveConnection`'s dead-grant refusal) applies exactly as it does to the REST API's `Create`/`Update`/`Delete`/`AddException` — this ADR adds no new authorization rule, only a new caller of the existing one.

`DeleteCalendarObject` needed no new write logic at all: `EventService.Delete` already builds the `events.delete` push for a writable Linked Calendar's Master (ADR-0077). Only its *visibility* check (`currentObjectETag`) needed to stop blanket-refusing a Connection Source and start asking Exposure instead.

## Threading the plan into `upsertSeries`

`upsertSeries` is shared by four callers (`PutSeries`, `writeSeries`, `createSubscribedSeries`, `updateSubscribedSeries`), only one of which ever targets a writable Linked Calendar. Rather than forking the write path, `upsertSeries` takes an optional `*putSeriesWriteBack` plan — nil for the other three — and enqueues each push in the same transaction as the row it accompanies (ADR-0018), right after that row is written and its id is known: the Master's own push after `Create`/`Update`, an Override's `INSTANCE` push after its own `Create`/`Update` (using the freshly-minted id on a create), and every Exdate's `CANCEL_INSTANCE` push after the exdate-replace block. A rolled-back write therefore can never leave a push queued — the two are the same commit.

## Why the Notification

ADR-0079's thesis is that *"a mechanism that reports nothing is indistinguishable from a system with nothing to report."* A phone receives success for the local write and has no protocol affordance to be told later that the push to Google failed. Folding a marker into the serialized object is not an option: stored columns are the sole source of truth (ADR-0026), and a marker in a summary would return on the next `PUT` and corrupt the Event's title.

So `markWriteBackPermanentlyFailed` — already responsible for the per-Event marker and the Source's needs-attention state — now also raises a Notification (ADR-0061), widening `kind` with `writeback_failed`. **Coalesced on the Source, not the Event**: `notifications` gains a nullable `calendar_id` (mirroring `event_id` becoming nullable for exactly this one kind) and a partial unique index on `(user_id, calendar_id) WHERE kind = 'writeback_failed'`, so a dead grant failing every one of its queued pushes at once raises exactly one Notification — re-raising upserts the existing row (fresh title, fresh `fired_at`, `seen` reset) rather than adding a second. This also closes the same gap for web-app edits, which today are discoverable only by noticing the marker on the grid.

## Known edge — recorded, not fixed

**"This and following" from a CalDAV client.** The protocol has no such operation, so a client performs it as two unrelated `PUT`s: the master gains an `UNTIL`, then a new Calendar object appears under a new `UID`. Both map onto existing pushes (a Master `PATCH`, then a `POST` for the new series), so Google ends up correct — but the local re-parent of Overrides past the split point never happens, and they remain on the now-bounded master. This is the same shape ADR-0078 already recorded for the web app's own "this and following" split, extended here to a client that reaches the same seam through two separate `PUT`s instead of one coordinated frontend action.

## Seams

The boundary sits at the outbox, matching how the codebase already divides Write-back testing (ADR-0078's own precedent): `caldavserver` over real HTTP asserts a `PUT`/`DELETE` queued the right outbox rows (`writeback_put_test.go`); `service` asserts `PutSeries`' diff against stored state produces the right plan (`event_put_series_writeback_test.go`), reusing `event_writeback_test.go`/`event_scoped_writeback_test.go`'s own fixtures. No new seams, and deliberately no fixture wiring Google into the CalDAV env — the send side (resolving an instance id from `events.instances`, the PATCH actually reaching Google) is unchanged from ADR-0078 and already covered there.

## Consequences

- **`ErrLinkedCalendarWriteUnsupported` is unaffected** — it still names the writes this ADR doesn't touch (an Attendee via Create, a CalDAV-shaped `ImportSeries` write, a Create/Update/`ReparentFrom` crossing a Connection Calendar's boundary). `PutSeries` never calls `ImportSeries`, so a CalDAV `PUT` was never subject to it in the first place; this ADR replaces `PutSeries`' own separate blanket refusal instead.
- **A new sentinel, `ErrLinkedCalendarWriteBackRevertUnsupported`**, names the two refused deltas — mapped to `403` in `caldavserver` (mirroring `ErrCalendarReadOnly`'s own mapping) rather than folded into the existing one, since the two refusals mean something different (a supported Calendar, an unsupported *edit shape*) from a Calendar that's read-only outright.
- **`ErrConnectionNeedsReconnect` reaches CalDAV for the first time**, mapped to `409` — mirroring the REST API's own mapping for the same sentinel (`handlers/event.go`) — for both `PutCalendarObject` and `DeleteCalendarObject`.
- **The Owner is, in practice, the only principal who reaches this path.** ADR-0075 clamps a Share on a Linked Calendar to Viewer, so `requireWritableCalendar`'s Access clamp already excludes an Editor accessor from ever reaching `buildPutSeriesWriteBack` — Exposure decides *visibility* for every principal (an accessor's own default is exposed, ADR-0080), but only the Owner's Access ever clears the write bar.
