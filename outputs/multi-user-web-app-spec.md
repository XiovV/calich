# Multi-user support (web app): Access-aware UI, Shares, per-User colour and Reminders, and account administration

Parent: #95

> **Scope: the web app, plus six small API additions it cannot proceed without.** #95 delivered the domain model, schema, API contracts, CalDAV surface and Reminder engine, and deliberately excluded all web app work on the grounds that several of its decisions were open. Those decisions have now been made and are recorded below. Six gaps in the existing API were found while making them; each is small, each blocks a specific piece of web app work, and they are specified here rather than deferred, because the web app cannot be built without them.

Vocabulary (Owner, Share, Role, Access, Admin, Disabled, Reminder override, Subscribed Calendar) is defined in `CONTEXT.md`. Decisions recorded in ADR-0029 and ADR-0032 through ADR-0038 are respected throughout; none is superseded by this spec.

## Problem Statement

After #95, a household can genuinely share Calendars — from their phones. The web app knows nothing about any of it.

Every Calendar the API returns now carries the caller's resolved Access, and none of it is read. The app has no notion of an Owner, no way to grant or revoke a Share, no way to see who else has an account, and no administration whatsoever. A person who has been granted Editor Access to the family Calendar sees it on their phone and cannot see it in the browser.

Worse, the app asks the wrong question about permission in eight places. Its write guard is "is this a Subscribed Calendar" — a predicate that was exactly right when the only read-only Calendar was one you had subscribed to yourself, and that is now wrong in both directions. It permits writes to a Calendar you only have Viewer Access to, which the server will refuse. It is also the predicate that would most plausibly be reached for when adapting these surfaces, which risks replacing one wrong question with another.

There is a specific trap waiting in that adaptation. Access is deliberately lossy about ownership: a Subscribed Calendar clamps its Owner's Access down to Viewer, because a Subscription is read-only for everyone including the person who created it. Owner-only management — rename, recolour, delete, share, revoke, bind a Subscription — is guarded separately and ignores that clamp. A web app that gates its Calendar management affordances on Access would therefore strip the Subscription editing, refresh and delete controls from the very person who owns them, silently regressing work delivered by #85 and #88. Nothing in the current API payload lets the app avoid this.

The instance is also unadministrable from the browser. Accounts can only be created by an Admin, and the only Admin is whoever bootstrapped the instance — who currently has no interface for creating the second account that the whole of #95 exists to serve.

## Solution

The web app learns to ask "what may I do with this Calendar" instead of "where did this Calendar come from", and gets that answer from the server rather than deriving it.

A single set of predicates replaces the subscription check everywhere a write is gated. Two questions are distinguished, because the server distinguishes them: whether you may write a Calendar's Events, which follows Access, and whether you may manage the Calendar itself, which follows ownership and is unaffected by the Subscription clamp. The Calendar payload gains an explicit ownership flag so the app can answer the second question without re-deriving a rule that lives on the server.

The sidebar grows a third group. Calendars you own, Calendars shared with you, and Calendars you subscribe to are visually distinct, each shared Calendar labelled with whose it is. A Calendar's row offers only what its viewer may actually do: an Owner can rename, recolour, download, delete and share it; someone it was shared with can recolour it for themselves and leave it.

Share management lives inside the dialog that already handles renaming and recolouring, because sharing is the same kind of per-Calendar management. An Owner sees who has Access and with what Role, and can grant, adjust and revoke. Anyone else sees that same dialog reduced to the two things that are theirs: their own colour, and leaving.

A Calendar you cannot write is inert rather than misleading. Its Events render, and they refuse to be dragged, resized, edited or deleted — the gesture never starts rather than starting and being discarded — and the Event dialog states plainly why, which is the one place this spec departs from the silent-removal convention established by #94. A subscriber already knows why their feed is read-only; someone else's Viewer does not.

Reminders on a shared Calendar can be retuned for yourself without touching the Event. This is presented as a distinct control rather than as an editable copy of the Event's own Reminders, because a Reminder override is a modifier applied to each of an Event's Reminders — one offset, one Channel, one muted flag — and not a personal replacement list. It appears only when more than one person would be notified, so a single-user instance never sees it.

Each User's colour choice is their own. An Owner's choice is the Calendar's colour and the default everyone else inherits; anyone else's shadows it for themselves alone, over the same endpoint, with ownership deciding where the write lands — the identical rule ADR-0038 chose for CalDAV, so one sentence describes both surfaces.

Administration gets its own place. Settings gains navigation, and Accounts becomes an item within it, visible only to Admins: the account list with each account's status, account creation, password reset, promotion and demotion, disabling and re-enabling, and deletion with its required disposition and its impact preview.

Finally, the app stops assuming its Access never changes. Calendars and Events refetch when the tab regains focus, so a Share granted while you were elsewhere is there when you return, and a write refused because your Access changed produces a corrected view and an explanation rather than a bare error.

## User Stories

### Seeing what you may do

1. As a User, I want the app to gate every write on my resolved Access rather than on whether a Calendar is Subscribed, so that a Calendar I only have Viewer Access to is not offered edit affordances the server will refuse.
2. As the Owner of a Subscribed Calendar, I want to keep every management affordance — rename, recolour, download, refresh, edit its Subscription, delete, share — so that the read-only clamp on its Events does not cost me control of the Calendar itself.
3. As a User with Editor Access, I want to create, edit and delete Events in a shared Calendar from the browser, so that the web app matches what my phone can already do.
4. As a User with Viewer Access, I want a shared Calendar's Events to render on the grid, so that I can see when the people I share with are busy.
5. As a User with Viewer Access, I want dragging and resizing an Event to do nothing at all, so that no preview appears and snaps back.
6. As a User with Viewer Access, I want the Event dialog to offer neither Save nor Delete, so that the form does not look editable when nothing can be saved.
7. As a User with Viewer Access, I want to be told why an Event is read-only, so that I can distinguish a Calendar someone shared with me read-only from a bug.
8. As a User, I want a Calendar I cannot write to be absent from the Calendar picker when creating an Event, so that I cannot choose an invalid destination.
9. As a User, I want a new Event to default to a Calendar I own rather than to whichever Calendar happens to be first, so that I do not routinely write into someone else's Calendar by accident.
10. As a User with Editor Access, I want the This event / This and following / All events choices on a recurring series in a shared Calendar, so that a shared Calendar is not a lesser experience than my own.
11. As a User with Editor Access, I want a shared Calendar available as an import destination, so that I can bring a file of Events into a Calendar I am trusted to write.
12. As an Editor on a Subscribed Calendar shared with me, I want writes refused exactly as they are for its Owner, so that the read-only guarantee holds uniformly.

### The sidebar

13. As a User, I want Calendars shared with me grouped separately from the ones I own, so that I can tell at a glance whose schedule I am looking at.
14. As a User, I want each shared Calendar labelled with its Owner's name, so that I know who to ask about it.
15. As a User, I want a Subscribed Calendar that was shared with me grouped with the other Calendars shared with me, so that everything that came from another person is in one place.
16. As a User, I want a shared Calendar's row to offer Leave rather than Delete, so that I cannot mistake renouncing my own access for destroying someone else's Calendar.
17. As a User, I want download and refresh offered only on Calendars I own, so that the row does not advertise actions that are not mine to take.
18. As a User, I want to toggle a shared Calendar's visibility exactly as I toggle my own, so that I can hide someone else's schedule without leaving it.
19. As a User, I want a Calendar that appears mid-session to be visible immediately, so that reloading and returning to the tab produce the same grid.

### Sharing a Calendar

20. As an Owner, I want to share a Calendar with another User from the browser, so that I do not need a native client or the API to do it.
21. As an Owner, I want to choose a person from a list rather than typing a username, so that I cannot grant access to the wrong account through a typo.
22. As an Owner, I want Disabled accounts absent from that list, so that I do not grant access to an account that cannot be used.
23. As an Owner, I want myself absent from that list, so that I am not offered a Share I already supersede.
24. As an Owner, I want to choose Viewer or Editor when sharing, so that I can show someone my schedule without letting them change it.
25. As an Owner, I want to see everyone who currently has Access to my Calendar and with what Role, so that I know who can see it.
26. As an Owner, I want to change someone's Role in place, so that I do not have to revoke and re-grant to adjust trust.
27. As an Owner, I want to revoke a Share, so that someone who no longer needs access loses it.
28. As an Owner, I want sharing controls to appear only on Calendars I own, so that the dialog never suggests I can re-share someone else's Calendar.
29. As a User, I want to leave a Calendar shared with me without involving its Owner, so that I can decline access I do not want.
30. As a User, I want leaving to ask for confirmation, so that I do not lose access to a Calendar by a stray click.
31. As an Owner, I want a failed grant to explain itself — no such User, already the Owner, invalid Role — so that I can correct it rather than guess.

### Colour

32. As a User, I want to set my own colour for any Calendar I can see, so that I can tell it apart from my other Calendars at a glance.
33. As a User, I want my colour choice to apply only to me, so that recolouring a shared Calendar does not change it on anyone else's screen.
34. As an Owner, I want the colour I set to be the Calendar's colour and the default everyone I share with starts from, so that "Family is the green one" is a shared fact until someone changes it deliberately.
35. As a User, I want to clear my personal colour and fall back to the Owner's, so that a change of mind is reversible.
36. As a User, I want every surface — grid, sidebar, month cells, drag previews — to agree on my effective colour, so that a Calendar looks the same everywhere.

### Reminders

37. As a User, I want a shared Event's Reminders to be retunable for me alone, so that I can leave two hours early for something its Owner only needs thirty minutes for.
38. As a User, I want to change a shared Event's Reminder Channel for myself, so that I can get an email where someone else gets an in-app Notification.
39. As a User, I want to mute a shared Event's Reminders for myself, so that a Calendar I follow for visibility does not become a notification firehose.
40. As a User, I want my personal setting presented separately from the Event's own Reminders, so that I never mistake retuning my own alert for changing everyone's.
41. As a User with Viewer Access, I want to see the Event's own Reminders read-only alongside my personal setting, so that I understand what I am overriding.
42. As a User with Editor Access, I want both the Event's Reminders and my own setting to be editable, with the effect of each made obvious, so that I can choose deliberately between changing it for the household and changing it for myself.
43. As a User, I want no personal Reminder control on a Calendar nobody else can see, so that a single-user instance is not cluttered by a setting that cannot mean anything.
44. As a User, I want my personal setting to survive someone else editing the Event, so that I do not have to set it again every time a title changes.
45. As a User, I want clearing my personal setting to return me to the Event's own Reminders, so that the override is reversible.

### Attribution

46. As a User, I want to see who created an Event in a shared Calendar, so that I know who to ask about it.
47. As a User, I want no attribution shown on a Calendar only I can see, so that the app does not tell me what I already know.
48. As a User, I want an Event whose creator's account was deleted to render without attribution rather than breaking, so that account deletion does not damage surviving Events.

### Administration

49. As an Admin, I want a place in the app to manage accounts, so that I do not need the API to create the second account on my own instance.
50. As a non-Admin, I want no administration UI at all, so that the app does not advertise actions I cannot take.
51. As an Admin, I want to see every account with its status — Admin, Disabled, password change pending — so that I know who has access to my server.
52. As an Admin, I want to create an account with a username and a temporary password, so that a household member can log in without me sharing my own credentials.
53. As a newly created User, I want to be forced to change that temporary password on first login, so that what the Admin typed does not remain valid.
54. As an Admin, I want to reset another User's password without knowing their current one, so that someone locked out can get back in.
55. As an Admin, I want to promote another User to Admin and demote them again, so that administration does not depend on one person being available.
56. As an Admin, I want to Disable an account and re-enable it, so that revoking access is reversible and does not destroy data.
57. As an Admin, I want to be prevented from demoting, Disabling or deleting the last remaining Admin, and told why, so that the instance can never become unadministrable.
58. As an Admin deleting a User, I want to be shown which of their Calendars have Shares and how many people would lose access, so that I understand the consequences before confirming.
59. As an Admin deleting a User, I want to be required to choose explicitly between transferring their Calendars to a named User and deleting them, with no default, so that a shared family Calendar is never destroyed by an accepted default.
60. As an Admin, I want deletion to be confirmed deliberately rather than by a single click, so that an irreversible act is hard to perform by accident.
61. As a household member, I want an Admin to have no way to read my Calendars from the admin UI, so that administering the server does not mean reading everyone's private life.

### Access that changes underneath you

62. As a User, I want a Calendar shared with me while my tab was in the background to be there when I return to it, so that I do not have to reload to see it.
63. As a User, I want a Calendar revoked while I was away to disappear when I return, so that I am not looking at a Calendar I can no longer reach.
64. As a User whose Access was revoked mid-edit, I want a failed save to explain that my access changed and to correct the view, so that I am not left with a bare error and a stale grid.
65. As a User whose Role was reduced from Editor to Viewer, I want the edit affordances to disappear once the app notices, so that the UI converges on what the server will actually accept.
66. As a Disabled User, I want the app to return me to the login screen rather than failing opaquely, so that losing access is legible.

## Implementation Decisions

### API prerequisites

Six additions to the existing API. None changes an existing contract; each is additive.

- **Calendar responses gain `isOwner`, `ownerUsername` and `shareCount`.** `isOwner` is un-clamped ownership and is the only correct basis for gating Calendar management, mirroring the server's own split between the Access check and the ownership check. `access` continues to gate Event writes. `ownerUsername` drives the sidebar's grouping labels and cannot be derived, since a non-Owner may not list a Calendar's Shares. `shareCount` is the number of Shares on the Calendar, and is what tells the app whether more than one person would be notified.
- **Share responses gain `userId`.** Revocation is addressed by user id while the listing returns only a username, so the two endpoints cannot currently be used together. The underlying row already carries the id.
- **The current-user endpoint gains the Admin flag.** Without it the app cannot decide whether to render any administration UI.
- **A user directory endpoint, available to any authenticated caller**, returning id and username for enabled Users excluding the caller. This backs the share picker. It is deliberately not the Admin-only account listing, which carries account status that a non-Admin has no business seeing.
- **Event responses gain `createdBy` and `createdByUsername`.** The column exists and the repository already reads it; only the response shape drops it. Null when the creator's account has been deleted, which the existing `ON DELETE SET NULL` guarantees.
- **Calendar update accepts a colour from any caller with Access.** Ownership decides where it lands: an Owner's write updates the Calendar's colour, anyone else's writes their personal colour. Every other field on that endpoint stays Owner-only. This is the same rule ADR-0038 chose for the CalDAV property write, so one sentence covers both surfaces. This item depends on #106 and is specified here but delivered as its own follow-up ticket.

The resulting Calendar payload:

```
{
  id, name, color,          // color is the caller's effective colour
  access,                   // "owner" | "editor" | "viewer" — gates Event writes
  isOwner,                  // un-clamped — gates Calendar management
  ownerUsername,
  shareCount,
  sourceUrl?, lastSyncedAt?, errorClass?, errorMessage?, keepAlarms
}
```

### The permission seam

Three predicates in the Calendar domain module are the only place the web app expresses permission, replacing the subscription check at all eight sites that currently gate a write:

- **May I write this Calendar's Events** — derived from `access`. Subscription read-only-ness folds in for free, since the server already clamps a Subscribed Calendar's Access to Viewer. This is what gates event creation, editing, deletion, drag, resize, the Event dialog's Save and Delete, the Event Calendar picker, and the import destination picker.
- **May I manage this Calendar** — derived from `isOwner`, never from `access`. This is what gates rename, recolour of the Calendar's own colour, download, delete, Subscription editing, refresh, and the sharing section.
- **Why is this read-only** — resolving to a Subscription, to Viewer Access, or to nothing. Used for copy only, never for gating.

The existing subscription predicate survives only where subscription-specific UI genuinely differs — the Subscription URL form, the refresh control, and the unsubscribe-versus-delete wording — and no longer answers any permission question.

This lands first, as a prefactor with no behaviour change on an instance with no Shares.

### Sidebar

Three groups: Calendars you own without a Subscription, Calendars reached through a Share, and Calendars you own with a Subscription. A Calendar that is both Subscribed and shared with you is grouped with the shared ones — whose it is matters more than where its Events come from, and its Subscription controls are not yours in any case.

Shared rows carry their Owner's username. Row actions follow the two predicates above: download and refresh require ownership, and Delete becomes Leave for a non-Owner, with its own confirmation.

The existing grouping variable that currently means "not Subscribed" is renamed, since it will otherwise read as "owned" while meaning something else.

### Share management

A section within the existing Calendar dialog rather than a separate surface, because sharing is per-Calendar management alongside rename and recolour, and because the row already carries as many controls as it can hold.

For an Owner, the section lists everyone with a Share and their Role, with in-place Role changes and revocation, plus a picker and Role selector for granting. For a non-Owner, the whole dialog reduces to a personal colour control and Leave. The dialog's content is driven by `isOwner`, the same flag the server guards on.

### Read-only presentation

Affordances are removed rather than disabled, following #94. The one addition is a single line of explanation in the Event dialog for a Calendar you cannot write, distinguishing Viewer Access from a Subscription. A subscriber knows why their feed is read-only; a Viewer has no way to know.

### Reminders

A Reminder override is a modifier, not a replacement list: one offset, one Channel, one muted flag per User per Event, substituted into each of the Event's Reminders wherever it sets a value. It therefore cannot be presented as an editable copy of the Event's Reminder list, and is presented as a distinct control.

For a Viewer, the Event's own Reminders render read-only above their personal control, which is the only editable thing — unambiguous, because nothing they touch can reach the Event. For an Editor or Owner both are editable, labelled so that the scope of each is obvious.

The control renders only when more than one person would be notified — that is, when the Calendar has Shares or is not yours — so a single-user instance never sees it.

### Colour

One picker, whose meaning follows ownership. An Owner sets the Calendar's colour, which is the default everyone inherits. Anyone else sets a personal colour, with a control to clear it and fall back. Both go through the same update endpoint. The app renders the effective colour the server returns and does not resolve the override itself.

### Administration

Settings gains left-hand navigation. The four existing sections become items within it, and Accounts becomes a further item rendered only for an Admin. The restructure is behaviour-preserving for the existing sections and is independent of every other item in this spec.

The Accounts view is the account list with status, and actions for create, reset password, promote, demote, disable, enable and delete. Deletion is a two-stage flow: the impact preview naming which Calendars carry Shares and how many people lose access, then a required disposition with no default — transfer to a named User, or delete. Guard failures, principally every path to removing the last Admin, are surfaced as explanations rather than generic errors.

### Access that changes underneath you

Calendars and Events refetch when the tab regains focus. Separately, an Event write refused with a not-authorised or not-found status triggers a Calendar refetch, a toast naming the Calendar whose access changed, and dismissal of the dialog that failed.

Newly seen Calendar ids are checked automatically. The app currently checks every Calendar on mount and never persists the checked set, so without this a Calendar arriving through a focus refetch would be invisible until reload — the two paths must agree.

### Delivery order

1. Calendar payload additions and the permission seam. No visible change on an instance with no Shares.
2. Share response id, the user directory, and the share management section. Deliberately early: until a Share can be created in the app, nothing downstream is testable without the API.
3. Sidebar grouping and row actions.
4. Read-only presentation on the grid and in the Event dialog.
5. Focus refetch, refused-write recovery, newly-seen visibility.
6. The personal Reminder control.
7. Event attribution.
8. Settings navigation restructure.
9. The Admin flag and the Accounts view.
10. After #106: the colour endpoint change and the personal colour control.

## Testing Decisions

A good test here asserts what a caller of the module observes — what a predicate returns, what state a store holds after an action, what request an API client issues — and never how the result was produced. Tests are named for the rule they protect.

**Existing seams only. No new seam is introduced.** The project has thirty-six pure module test files and no component tests; introducing a component-testing seam for this feature would be the largest testing decision in the spec and is not warranted, particularly as rendering and affordance behaviour are verified manually in this project by convention.

**The Calendar domain module — the primary seam.** Prior art: the existing Calendar and Calendar-colour module tests, which are pure and table-driven. New coverage:

- The write predicate, the management predicate and the read-only reason as a table over Access × ownership × Subscription presence.
- Specifically that the Owner of a Subscribed Calendar is refused Event writes and permitted Calendar management, which is the trap this spec exists to avoid.
- That a Calendar with no Access is treated as absent rather than as read-only.

**Stores.** Prior art: the existing Calendar, events, app-password and notification store tests, which mock the API module beneath the store and assert on resulting state. New coverage:

- Share grant, Role change, revoke and leave, including that leaving removes the Calendar from local state.
- The refused-write recovery path: a not-authorised or not-found response triggers a refetch and leaves state consistent.
- Newly-seen Calendar ids being added to the checked set, and previously unchecked ones not being re-checked.
- Effective colour being taken from the server response rather than resolved locally.

**API client modules.** Prior art: the existing Calendar and app-password API client tests. New coverage for the shares, user directory, accounts and Reminder override clients: request shape, authorisation header, and error translation.

**Backend prerequisites** are tested at the service and handler layers per #95's established practice — in particular that the ownership flag is un-clamped where Access is clamped, that the user directory excludes Disabled Users and the caller, and that a colour write from a non-Owner writes a personal colour while leaving the Calendar's own colour untouched.

**Not tested automatically:** rendering, affordance presence, dialog flows and the read-only presentation, all of which are verified manually.

## Out of Scope

- **Organisations and multi-tenancy.** Discussed and deliberately deferred. Nothing in this spec forecloses it: the web app never sees a tenancy boundary under any of the strategies considered.
- **Groups or teams.** Shares remain to individual Users, as #95 decided.
- **A component-testing seam.** Considered and rejected above.
- **Persisting the checked-Calendar set** across reloads. The current behaviour of checking everything on load is preserved; only the mid-session case is addressed.
- **Real-time updates.** Focus refetch and refused-write recovery only — no polling, no server-sent events, no websockets.
- **Admin data access** in any form, including impersonation, per ADR-0037.
- **Event attendees and invitations**, per #95.
- **Sharing to people without an account**, per #95.
- **Per-Occurrence Reminder overrides**, per ADR-0036.
- **Audit logging** of who changed what in a shared Calendar.
- **Export scoping changes.** Bulk export already covers only Calendars you own, and its existing copy is accurate.

## Further Notes

- **A bug was found while specifying this and should be filed separately.** A Reminder override is applied per Reminder, and the fired-Reminder ledger is unique on Reminder, Occurrence and User. An Event carrying two Reminders plus an override therefore collapses both to the same offset and Channel for that User, producing two identical notifications at the same instant from two distinct ledger rows. This is #95's story 55 in intent though not in letter. It is a backend fix and does not block this spec.
- **`CONTEXT.md`'s Reminder override entry should be corrected.** It describes the override as a personal *replacement* for an Event's Reminders. The implementation substitutes its offset and Channel into each of the Event's Reminders — a modifier, not a replacement. The current wording actively suggests a design (an editable personal Reminder list) that the schema cannot express, and it is what made that design look feasible during this spec's discussion.
- The ownership trap described in the Problem Statement is the single highest-risk item here. It is invisible on an instance with no Subscriptions, and it silently regresses shipped functionality rather than failing loudly.
- Sharing is deliberately delivered second rather than last. Without it, every downstream item requires the API to construct the state it is meant to display.
- The Settings navigation restructure is independent of everything else in this spec and can be scheduled freely.
