# Write-back is a queued, field-scoped PATCH

Status: accepted — amends ADR-0050 (scopes) and ADR-0052 (read-only); the three edit scopes are built out in ADR-0078

A Linked Calendar is **writable**. Events created, edited and deleted in this app are dispatched to the Provider as **Write-back**: the outbound counterpart to Refresh, queued through the `outbox` rather than performed inside the request, and expressed as `events.patch` — never `events.update`.

This reverses ADR-0050's scope choice and ADR-0052's "every Source is read-only today". Both anticipated the reversal; neither had shipped, so it costs an edit rather than a re-consent.

## The scope reversal

ADR-0050 requested `calendar.events.readonly` and argued against holding a write grant ahead of need — *"the consent screen would overstate what the app does at exactly the moment a user is deciding whether to trust it"*. That principle is intact and this ADR obeys it; only the input changed, because the app now writes.

The grant is **`calendar.events` + `calendar.calendarlist.readonly`**. Still not the broad `calendar` scope, which additionally permits creating and deleting calendars and editing their ACLs — none of which this app does.

**Writability is per-Calendar, not per-Connection.** Google returns `accessRole` on each `calendarList` entry, and a calendar merely shared *to* the account comes back `reader`; Holidays and Birthdays are never writable at all. So a sidebar in which some Linked Calendars accept edits and some do not is the correct rendering of reality, and `accessRole` is re-read on every Refresh. This is not the `accessRole` dependency ADR-0052 rejected — that rejection was about letting a third party's ACL govern *reading and re-sharing here*. A write is refused by Google with 403 regardless; the only choice is whether the UI knows before the failure or after it.

## PATCH-only is the load-bearing rule

Write-back carries exactly: title, start, end, `allDay`, `tzid`, `RRULE`, description, location, URL.

**Event colour is deliberately not in that list**, and not for the reason the others are absent. ADR-0029 makes it an arbitrary hex; Google offers eleven fixed event colours, so pushing means snapping a user's choice to the nearest of eleven. Instead the mapping stays one-way: Google's `colorId` seeds the Event colour inbound, tracked with **per-Event shadow state exactly as ADR-0032 tracks a Subscription's calendar name** — an untouched Event follows the Provider's recolours, a recoloured one stays put, and there is no third state. Without the shadow, a locally recoloured Event silently reverts on the next Refresh, which is the failure this costs two columns to avoid. Dropping the inbound mapping altogether was the alternative and was declined: people who colour their Google events mean something by it, and importing them as a uniform wall reads as a broken import.

It carries **nothing else** — not Reminders (personal per-User here, ADR-0064; pushing one User's private cue would change the event for the account holder), not Attachments (ADR-0040 stores bytes, Google wants Drive links, and ADR-0052 already declined to model those), not Attendees, and not the Provider fields this app never mapped: `conferenceData`, `extendedProperties`, `guestsCanModify`, visibility, transparency.

`events.update` serializes the **whole** event, so a PUT that changed only a title would wipe the Meet link, the guest list and the visibility of an event this app cannot even display. Field-scoped PATCH is the single mechanism that lets ADR-0052's *"Attendees are not mirrored onto Linked Calendars"* survive contact with a writable Calendar. **It is enforced at the Provider seam so that no call site can choose otherwise** — a rule this important must not be a convention each caller re-observes.

## Queued, not synchronous

The local write commits and the push is enqueued in the `outbox` that already exists for mail (ADR-0060), retried with the poller's backoff.

Synchronous dispatch is the more honest design — a failed push refuses the edit and nothing is ever out of step. It was rejected because it makes Google's availability into this app's availability, on an instance a person is running specifically so their calendar does not depend on somebody else's uptime. A LAN-only deployment would refuse every edit.

**The failure surface is therefore part of this decision, not a follow-up.** A pending push needs no UI; it lands in seconds. A **permanently failed** one — 403 on a `reader` calendar, a revoked token — gets a per-Event marker in the grid *and* raises the Connection's ADR-0033 needs-attention state. "My edit never reached Google and nothing told me" is strictly worse than the refused edit synchronous dispatch would have given, and it is the only way this trade goes wrong.

## When Write-back cannot run

A Connection whose token is revoked or expired — including ADR-0051's seven-day Testing-status trap — **refuses new edits** on its Linked Calendars until it is reconnected, surfacing the Connection's ADR-0033 needs-attention state as the reason and a Reconnect action beside it.

This looks like a contradiction of the queued design and is not. Queueing exists so an edit survives a Provider that is *briefly* unreachable; it was never meant to absorb a week of work against a grant that no longer exists. Accepting edits indefinitely means they appear to succeed for days and then replay all at once against a calendar that has moved on — every one of them landing in the 412 path simultaneously, in a fight the user never knew was happening. Refusing is not disabling the Connection, which ADR-0033 forbids: last-good state is preserved, nothing is deleted, and reads continue.

**Reconnect reuses the existing Connection row**, preserving its Linked Calendars, cursors and per-series etags. Minting a new one would orphan every Linked Calendar and force a re-import of calendars the User already picked.

## Conflicts and the echo

- **Push with `If-Match` on the per-series etag ADR-0052 stores.** On **412**, refetch the event, reconcile it locally, and re-issue the PATCH with the fresh etag — bounded to three attempts before the Event goes needs-attention. Because the push is field-scoped, this is a field-level merge, not a data-losing overwrite: our fields win, the Provider's untouched fields survive. Writing blind with no `If-Match` was the alternative and would clobber fields this app does not model.
- **The write response is applied as if it were a Refresh result.** The API returns the updated event and its new etag; storing it means our own write does not come back in the next Delta Refresh as a change. Skipping this makes every single edit look like a remote change to every CalDAV client in the house, re-downloading a series nobody touched.

## The three edit scopes

ADR-0066 compiles a scoped edit into Series operations; those map onto the Provider as:

- **All events** → PATCH the master.
- **This event** → PATCH that instance.
- **Delete this event** → PATCH that instance to `status: cancelled`. **Never an `EXDATE` line in Google's `recurrence` array.** Both representations exist at Google and mixing them drifts — one representation, chosen once.
- **This and following** → no API operation exists for it. The split is performed by hand: `UNTIL` the original series, create a second recurring event, and adopt the **new Provider event id** onto the re-anchored local series. This is the only one of the four that is not a single call, and the only one whose failure leaves a half-split series.

Detaching an instance for the first time needs an instance id that is not yet in our store. **Call `events.instances` filtered by `originalStart`** rather than constructing the documented `{eventId}_{timestamp}` form — the shortcut works until an all-day or DST edge, which is a bug that surfaces only in real data.

## Guests this app cannot see

ADR-0052 parked the Attendee widening with the words *"this returns with write-back"*. **It does not return here.** Widening `attendees.user_id` to nullable is a migration touching every authorization path — ADR-0052's own warning is that an unresolved Attendee must never confer visibility and that *every* path must be re-checked, not just the invite path. That is its own release.

What ships instead is a **read-only guest count** on the Event, held as a plain field and explicitly not an Attendee row, and **`sendUpdates=none`** on every push.

The cost is stated plainly: dragging a meeting to a new time changes it at Google and emails nobody, so those guests' calendars are stale until the event's Google owner touches it. Shipping write-back with *no* signal that an event has guests was the trap — a bare time block that is secretly a fourteen-person meeting is the same silent-failure class ADR-0053 exists to prevent. The badge closes it for the price of one integer.

## Consequences

- **"Has a Source" and "may not be written here" are no longer the same question.** ADR-0052 drew the mode/existence distinction ahead of need and this is the need arriving. Every guard that reads `Source IS NOT NULL` as "read-only" is now wrong, and the failure mode is refusing an edit the Provider would have accepted.
- **A Share on a Linked Calendar is clamped to Viewer.** An Editor's write would execute as the connecting User's Google identity — the only place in this app where one User's action runs as another's at a third party. That deserves its own answer (a per-Share writable flag? attribution in the description?) and V1 does not need to give one.
- **Attachments stay banned on a Calendar with a Source**, unchanged from ADR-0040 — but its stated reason is gone, so the surviving one is recorded here. The ban is no longer "the Calendar is read-only"; it is that an Attachment held only by this instance, on an Event that demonstrably syncs elsewhere, is invisible from every other client the User opens that Event in, including the phone showing them the Provider's copy. Offering it would be worse than withholding it.
