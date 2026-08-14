# Two write disciplines, and one optimistic write

Status: accepted

This client writes two ways, and until now neither was named.

**Optimistic.** Paint the change, ask the server, put the cache back if it refuses, and say so in a toast. Twelve places do this: six in `eventsStore`, five in `calendarsStore`, one in `notificationsStore`.

**Server-first.** Ask the server, set state from what it returns, and let a rejection propagate to the caller, which shows it inline through `useAsyncAction`. Every write in `groupsStore`, `workspaceMembersStore`, `workspacesStore` and `appPasswordsStore` does this — and so do `subscribeCalendar`, `refreshCalendar`, `shareCalendar` and the two revokes, which live in `calendarsStore` beside five optimistic ones.

The second discipline has no duplication at all: there is nothing to share, because each of those writes is four lines. The first had twelve hand-written copies of the same five steps, and they had drifted.

## When each applies

The split is not per store, which is why it was easy to miss — `calendarsStore` uses both. It is per write, and it turns on two questions.

**Does the client already know the result?** A renamed Event, a recoloured Calendar, a Notification marked seen: the client has the whole new value before it asks. A subscribe does not — the server fetches the feed and returns a Calendar that did not exist. Neither does a refresh, which returns a summary. There is nothing to paint optimistically when the value comes back from the server.

**Is anyone positioned to read an inline error?** An optimistic write is for a surface the User is already looking at, where the change appearing *is* the feedback and a toast is the only place a failure could go. A server-first write is for a dialog that stays open, owns its own error line, and can name what specifically went wrong — a bad feed URL, a duplicate Group name.

So: **optimistic when the client knows the result and the surface is the feedback; server-first when the result comes from the server or a dialog is open to receive the failure.** A new write answers those two questions before picking.

## What the module owns, and what it does not

`makeOptimisticWrite` owns *when* to revert and *what to say*. Each site owns *how* to apply and how to undo.

That division is deliberate and the tempting consolidation is wrong. Ten of the twelve sites snapshot a whole slice and restore it wholesale; `addEvent` and `addCalendar` instead remove the one failed id. Making all twelve wholesale would introduce a bug in those two: `addEvent`'s optimistic path is fire-and-forget, so another write can interleave before it fails, and restoring the array captured beforehand would silently discard that other change. The correct inverse is site-specific, so `revert` is a closure rather than a snapshot the module takes for itself.

## The Access-change policy is bound per store, not imported

A write refused with 403 or 404 usually means the caller's Access changed underneath them — a Share revoked or downgraded, or the Event already gone (#116). That deserves better than "Failed to update event.": it names the Calendar and refetches Calendars so the sidebar stops lying.

Resolving the name requires reading `calendarsStore`, and `calendarsStore` is one of this module's own callers. Rather than widen the import cycle — `eventsStore` and `calendarsStore` already form one, tolerable only because both accesses are lazy `getState()` calls inside function bodies — the policy is a parameter. `makeOptimisticWrite(accessChangeMessage)` binds it once per store; the twelve sites pass only what differs.

It is optional because one caller genuinely has none: a Notification has no Calendar to name, so `notificationsStore` binds nothing. That third case is what makes this a seam rather than an assertion — two adapters bind the policy and one deliberately does not.

## Considered Options

- **Zustand middleware wrapping each store.** Rejected: it would sit in front of every action including the server-first ones, which need none of it, and the wrapping would have to distinguish them anyway.
- **A helper taking `set`/`get` and a patch, computing the revert itself.** Rejected: it cannot express `updateCalendar`'s second apply or `editOccurrence`'s op-list projection, and computing a generic inverse means snapshotting slices — the exact thing that breaks `addEvent`.
- **Leaving `notificationsStore` out** as a single instance in a different domain. Rejected: one copy left behind is where the next divergence starts, and it is the purest example of the shape.
- **Importing `accessChangeMessage` directly.** Rejected on the cycle above. The parameter costs one line per store and keeps this module near-leaf.
- **Folding in the Calendar cascades.** `deleteCalendarCascade` and `leaveCalendarCascade` are near-identical and exist only to consume the boolean this module returns. That contract is preserved, so they need no change; deduplicating them is its own worthwhile refactor and does not belong in a diff whose question is "did behaviour hold".

## Consequences

- **`calendarsStore` gains Access-change handling it did not have.** Revoke someone's Share while they have a Calendar open, and deleting it said "Failed to delete calendar." and refetched nothing, leaving a sidebar that still showed it. It now names the Calendar and refetches, exactly as an Event write has since #116. This is the one intended behaviour change.
- **Every optimistic write reports whether it landed.** Three return contracts — `void`, `boolean`, and rethrow — collapse to one `Promise<boolean>`. Callers that ignore it are unaffected.
- **`addEvent`'s staged-Attendees path is not an optimistic write and does not use this.** It awaits the create before painting anything, precisely so a rejected invite never shows an Event whose invitations silently did not go out (#187, ADR-0055), and it rethrows so the modal can show its own banner. By the rule above it is server-first, and it stays that way.
- **`isAccessChangeError` moves out of `eventsStore`.** It was private there and is now the module's own.
- **The store tests are left as they are.** Each write action's "rolls back and toasts on failure" test is now partly redundant with `optimisticWrite.test.ts`, and that redundancy is the evidence that this refactor preserved behaviour. Thinning them belongs to a later change, if at all.
