# Commit-on-click dialogs do not get a Save button

Status: accepted

ADR-0067 named two write disciplines and drew the line per write: **optimistic** when the client already knows the result and the surface the caller is looking at *is* the feedback, **server-first** when the result comes from the server or a dialog is open to receive the failure. That ADR was about stores. It stops one layer short of the thing a User actually touches.

**A dialog whose every control commits on click is a different kind of object from a dialog you submit, and the two must not share a footer.** A form dialog stages edits and a Save button is the moment they become real; Cancel is a genuine undo, because nothing has happened yet. A commit-on-click dialog has already written by the time you look at the footer. There is nothing left for Save to commit and nothing Cancel could take back.

So: **a dialog built from server-first writes gets one dismissal button, and no Save/Cancel pair.** The sharing dialog's is `Done`.

## The evidence this was already true, unnoticed

Sharing lived as a section at the bottom of the Edit calendar dialog, below Name, Color, Subscription URL and two Default reminders editors. It never participated in that dialog's Save. `handleGrant`, `handleUserRoleChange`, `handleRevokeUser` and their two Group siblings each fired their own request on click.

The store writes behind them are the server-first ones ADR-0067 names — `shareCalendar` and the two revokes — while the dialog additionally paints its own roster and puts it back if the request is refused. Which discipline a given control uses turns out not to matter here: what the footer has to reckon with is only that the write already happened.

Cancel therefore did not undo a revoke, and Save did not commit a grant. That inconsistency was invisible only because Name and Color dominated the dialog and nobody read the footer as a promise about the section three fields down.

The strongest evidence, though, is #126: the sidebar's share-count badge opened the Edit modal with `initialFocus: "sharing"`, driving a `ref` and a mount-only `scrollIntoView` whose whole job was getting the caller past the rest of a dialog to a section they could not otherwise find. A deep link built to skip a dialog's contents is a load-bearing complaint about where the destination lives.

Pulling sharing into its own dialog makes the mismatch impossible to hide: alone in a modal, a footer offering Save and Cancel over five controls that have all already written would be an outright lie.

## Considered Options

- **Keep sharing in Edit and give the section a visual boundary** — a heading, a rule, a nested card saying "changes here apply immediately". Rejected: it is an apology for the structure rather than a fix, it leaves the deep link in place, and the footer still lies, just more quietly.
- **Give the Share modal a real Save by staging grants and revokes.** Rejected for what ADR-0049 already rejected: batching needs a new endpoint or a client-side queue, staged-vs-committed state, and a dirty-close guard — machinery nothing else in this client has, bought to make a footer consistent rather than to serve anyone sharing a Calendar.
- **`Close` rather than `Done`.** A coin toss, settled on the grounds that `Done` reads as finishing a task and `Close` as abandoning one — and abandoning is precisely what this dialog cannot offer.

## Consequences

- **Sharing is its own modal,** opened from a `Share` item in the calendar row's kebab menu (second position, after `Edit`) and from the share-count badge. `CalendarSharingSection.tsx` became `ShareCalendarModal.tsx` and absorbed the dialog chrome; the grant/revoke/role logic is unchanged.
- **`initialFocus` is gone entirely** — the prop, the `ref`, the mount-only effect and its `eslint-disable`. Once Share is a first-class door there is nothing to scroll to, and the Edit modal keeps no breadcrumb: a "Manage sharing" link would recreate the original problem, sharing still appearing to live under Edit, now two clicks deep.
- **The Edit calendar modal stays a form.** It keeps Save and Cancel, because Name, Color, Subscription URL, KeepAlarms and the Default reminders lists are all staged until Save. This ADR does not ask it to change; it asks the two kinds not to be mixed in one dialog.
- **`Dialog`, not `AlertDialog`.** An operating surface is not a destructive confirmation, so Escape and outside-click are plain dismissals, exactly like `Done`.
- **The next modal has a rule to reach for.** Build it from server-first writes and it gets one dismissal button; stage its edits and it gets Save and Cancel. A dialog that wants both is really two dialogs.
