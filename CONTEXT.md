# Calich

Domain vocabulary for the calendar app — navigation chrome, view state, and the sidebar calendar list.

## Language

**Active view**:
The granularity at which the calendar is currently displayed — Day, Week, Month, or Year. Drives the main content grid (later) and the top bar's date label format. Starts each Session at the User's Default view.
_Avoid_: mode, display mode

**Selected date**:
The single date the shell is currently focused on, shown in the mini calendar and driving the main grid. Distinct from "today" — navigating away doesn't change what "today" means.
_Avoid_: current date, active date

**Calendar**:
A named, independently-toggleable collection that groups events (e.g. "Work", "Personal"), carrying one Calendar color and exactly one Owner. The unit of both ownership and sharing — an Event has neither of its own, inheriting both from the Calendar it belongs to. Not the app itself, and not the view/grid. See ADR-0034.
_Avoid_: calendar list item, source

**Calendar color**:
The single color a Calendar is displayed in, inherited by every Occurrence it contains — an arbitrary sRGB value with an alpha channel, not a choice from a fixed set. Set by the Owner and shared by every client rather than being the Owner's own private preference: whoever writes last wins, there is no conflict detection, and no bound on how long two clients may disagree. Every other User with Access sees their own personal override instead — auto-assigned the moment they first view the Calendar (from the Swatches, or a random color once those run out) and freely repickable afterward. There is no state in which a shared Calendar tracks the Owner's color for someone else. See ADR-0029, ADR-0038, ADR-0057.
_Avoid_: calendar colour (in prose), color enum

**Swatch**:
One of the eight curated colors the web app offers as one-click quick picks when choosing a Calendar color or an Event color. A convenience for the person picking, never a constraint on what either color may be — a Calendar or Event whose color matches no Swatch is ordinary, not invalid, and is what a native client setting its own color normally produces.
_Avoid_: palette color, preset, theme color

**Calendar toggle**:
The sidebar control that shows or hides a Calendar's Occurrences on the grid, rendered in that Calendar's own resolved color rather than a theme accent: filled with a contrasting check when showing, an unfilled ring in the same color when hidden, so color identity survives hiding a Calendar. A sidebar-specific control, not a mode of the app's generic checkbox — its color is data, not a theme constant, and a Calendar whose color matches no Swatch still needs it to render that color.
_Avoid_: swatch, checkbox (in prose, for this control specifically), dot (the coloured dot it replaced)

**Event color**:
An optional color on a Master or Override, in the same arbitrary-hex value space as Calendar color, that wins outright over the Calendar's color for that Occurrence when set. Absent by default, in which case the Occurrence renders in whatever Calendar color would otherwise resolve for the viewer — an Event does not have to have a color, and most don't. A Calendar's color is shared, resolved-per-viewer state (Owner's value, or that viewer's personal override, ADR-0038); an Event color has no such per-viewer layer — it is one value, set by an Editor, seen identically by everyone with Access. See ADR-0029, ADR-0043.
_Avoid_: event colour (in prose), custom color, color override (ambiguous with Calendar color's per-User override, which is a different mechanism)

**Event URL**:
The single optional link on an Event pointing at more information about it — a ticket, a document, a video call address someone pasted. Stored exactly as written and never rejected, so a native client's `message://` or a feed's vendor scheme survives a round-trip unchanged; offered as a click-through only when it is `http` or `https`, and shown as plain text otherwise. Deliberately not a Join button (that is iCalendar's `CONFERENCE`, which this app does not model, having no conferencing to put in it) and deliberately not an Attachment, which is bytes this instance holds. Exactly one per Event, because the iCalendar `URL` it maps to permits no more; Description is where several links go. See ADR-0063.
_Avoid_: link, event link, web link, conference link (that is a different thing this app does not have), attachment URL

**Event**:
The stored, saved unit — a titled time block belonging to a single Calendar, with a start, an end, an optional Recurrence rule, and an optional Event color. Carries no Reminders of its own: those belong to each User who can see it, not to it. Its start/end define the first occurrence and the duration every Occurrence inherits. A non-recurring Event is a series of one. Distinct from the Occurrences it produces on the grid.
_Avoid_: appointment, meeting, entry

**Recurrence rule**:
The rule on an Event describing how it repeats, stored as an iCalendar `RRULE` string (RFC 5545) so it round-trips through CalDAV unchanged. Absent on a non-recurring Event. See ADR-0016.
_Avoid_: repeat pattern, schedule, recurrence pattern

**Occurrence**:
A single dated instance produced by expanding an Event's Recurrence rule over a date window. Occurrences are computed, never stored; they are what the grid actually renders (an Event chip or timed block is an Occurrence, not an Event). A non-recurring Event yields exactly one Occurrence. Identified by `(eventId, occurrenceStart)`, where `occurrenceStart` is the datetime the rule produced (its iCalendar `RECURRENCE-ID`).

**Master**:
The Event row that carries the Recurrence rule and anchors a series. Its title/start/end/calendar/Event color are the defaults every Occurrence inherits unless an Override replaces them, and it is the row a series' Attachments hang off. A non-recurring Event is a Master with no rule.
_Avoid_: parent event, root event

**Override**:
A stored Event row that replaces one Occurrence of a series with its own full title/start/end/calendar/Event color, keyed to the Occurrence it replaces by its original start (iCalendar `RECURRENCE-ID`). Produced by editing "this event" on a recurring Occurrence. A complete standalone instance, not a diff.
_Avoid_: exception (that's the cancellation), detached event, instance edit

**Exception**:
A cancelled single Occurrence — the rule still generates that slot, but it is suppressed from the grid (iCalendar `EXDATE`). Produced by deleting "this event" on a recurring Occurrence. Distinct from an Override, which replaces rather than removes.
_Avoid_: deleted occurrence, skip, override

**Series operation**:
One of the ordered steps a scoped edit (This event/This and following/All events) on a recurring Occurrence compiles down to — override an Occurrence, re-anchor a series at a split point, or put a single Event's fields wholesale. The same operation list is realized two ways: applied to the local cache, and dispatched to the API in order, so the two can never drift apart. See ADR-0016.
_Avoid_: edit step, mutation, transform

**Save plan**:
What pressing Save on the Event modal compiles down to before anything is written — a refusal, a close, a Reminders-only write, one of three creates, an Event update, a scoped series edit, or a request to ask the User for a scope or to confirm discarding a series' Overrides. Recomputed from the form's current state rather than captured when Save was pressed, so confirming a scope re-enters the same planner with the scope now known. Distinct from a Series operation, which is what a scoped edit compiles to one tier below, once the scope is settled. See ADR-0066.
_Avoid_: save action, submit handler, intent, command

**All-day Event**:
An Event that occupies whole dates rather than a time range, flagged by `allDay`. Stored with the iCalendar half-open date convention — `start` is the date, `end` is the exclusive next day (a single-day all-day Event spans one day; a multi-day one spans more). Its start/end are wall-clock dates, serialized as date-only strings and never timezone-converted. Rendered in the all-day lane, not on the hourly grid — a multi-day one gets a chip in every day it touches, in the lane and in Month view, clicking any of them opens the same Event. Creating one that spans more than a day is not yet reachable from the Event modal; only import and the API produce one today. See ADR-0017, ADR-0069.
_Avoid_: full-day event, date event

**All-day lane**:
The horizontal strip above the hourly grid in Day and Week view where all-day Occurrences are shown, separate from the timed grid below it.
_Avoid_: all-day row, header row

**Draft block**:
A proposed time window for a not-yet-saved Event, before it is confirmed via the creation modal — whether it originated from dragging on the grid or from a default (e.g. the sidebar's Create button). Discarded, not saved, if the modal is cancelled.
_Avoid_: pending event, temp event

**Drag readout**:
The label shown beside a block while it is being dragged on the hourly grid, giving the start, end and total duration the gesture would commit. Live — it reflects the snapped times at every moment of the drag, not the times the Occurrence had when it started. Appears only once the gesture passes the click-vs-drag threshold, so clicking a block to open it shows nothing. Absent from the all-day lane and Month view, where a drag changes neither the time of day nor the duration and there is nothing to report.
_Avoid_: drag tooltip, time label, drag hint

**Reminder**:
A single cue that fires a fixed offset before an Occurrence's start, on one Channel, belonging to **one User on one Event** — never to the Event itself. Each User with Access to the Event's Calendar, and each Attendee of it, carries their own zero or more, invisible to everyone else: the person who created the Event reminds nobody but themselves. Absent ones fall back to that User's Default reminders for the Calendar, unless they have explicitly saved an empty list for this Event. Maps to one iCalendar `VALARM` (`TRIGGER` for the offset, `ACTION` for the Channel), and CalDAV serves each principal the set resolved for them, so a device fires its own User's timing. See ADR-0020, ADR-0064.
_Avoid_: alert, event reminder (an Event has none), shared reminder

**Default reminders**:
The Reminders one User has set on one Calendar rather than on any Event — two lists, one for timed Events and one for all-day ones, since an all-day offset counts back from 09:00 on the date rather than midnight. Apply to every Event on that Calendar where that User has saved no list of their own, resolved when Reminders are read rather than copied onto Events, so changing them moves every Event the User has not personally touched. Per-User state on a Calendar, set beside that User's color override and living in the same place as it. The replacement for the retired fan-out: each person states their own intent once, on the Calendar, instead of an Event's author stating it for everyone. See ADR-0064.
_Avoid_: calendar reminder, default alarm, notification settings, reminder preference (Preferences are a different, Calendar-independent concept)

There is deliberately **no term for one User's personal modifier over an Event's Reminders.** ADR-0036's "Reminder override" — one offset, one Channel, one muted flag layered over a shared list — is retired: a Reminder is personal outright, so retuning is just editing your own, and muting is saving an empty list. The word survives only in `user_event_reminders`-era code awaiting removal.

**Channel**:
The delivery method of a Reminder — **Notification** (shown in-app) or **Email** (sent to the User). Corresponds to the iCalendar `VALARM` `ACTION` (`DISPLAY` / `EMAIL`). A property of a Reminder and of nothing else: an Invitation has no Channel and is not governed by one, so Channel does not explain where every piece of mail or every Notification comes from.
_Avoid_: type, kind, action (in prose)

**Notification**:
The persistent in-app record of something that concerns one User. Lives in the Notification feed and, if a tab is open, is mirrored as a browser notification. Exactly two things produce one: a Reminder on the Notification Channel firing (where the Reminder is the rule and the Notification the delivered instance), and the User being made an Attendee of an Event. Deliberately nothing else — being removed as an Attendee, an Event moving, and another Attendee's Response all produce none, the last because the Attendee list already shows every Response and a fourteen-person invite would otherwise mean fourteen entries. The Organizer never gets one for their own Event, since an Organizer is never an Attendee. Distinct from a toast, which is a transient client-only UI message (e.g. an error). See ADR-0021, ADR-0061.
_Avoid_: alert, toast (for this), push

**Day cell**:
A single day's box in the Month view grid. Holds that day's Event chips and is the target of create-clicks and drag-to-move drops.
_Avoid_: day box, tile

**Event chip**:
The compact, single-line rendering of an Event inside a Day cell, drawn in its Calendar's color and showing start time and title. The Month-view counterpart to the grid's timed event block.
_Avoid_: pill, event bar

**Adjacent-month day**:
A Day cell belonging to the month before or after the one being displayed, shown to fill the fixed six-row grid. Rendered dimmed but fully live — its Events show, and it accepts creates, edits, and drops like any other Day cell.
_Avoid_: overflow day, padding day, out-of-month day

**Mini-month**:
A compact, read-only month shown in the Year view — day numbers only, no Event chips. One of twelve laid out in a grid.
_Avoid_: month thumbnail, small calendar

**Event-presence dot**:
The marker on a Mini-month day indicating it has at least one visible Event. A density hint only — it carries no Calendar color and no count.
_Avoid_: event indicator, badge

**Anchor zone**:
The IANA timezone an Event's wall-clock is defined in, stored as the `tzid` on the Event (the `TZID` a CalDAV client sends). Drives Recurrence rule expansion and DST — "every Mon 10:00 Europe/Berlin" fires at 10:00 Berlin wall-clock across a DST change. Belongs to the Event and is preserved when the Event is edited; only an explicit zone choice changes it. `Etc/UTC` means an absolute instant; absent means a Floating Event. See ADR-0019.
_Avoid_: event timezone, source zone, origin zone

**Viewer zone**:
The IANA timezone the current viewer sees times in — detected from the browser and applied only at render, shifting each Event's instant to local wall-clock (a 15:00 Berlin meeting shows as 09:00 to a New York viewer). Belongs to the session, not the Event, and follows the viewer as they travel. Distinct from the Anchor zone. See ADR-0019.
_Avoid_: local zone, display timezone, user zone

**Floating Event**:
A timed Event with no Anchor zone (`tzid` absent), whose wall-clock is interpreted in the Viewer zone rather than any fixed zone — it has no single absolute instant and expands recurrence in the viewer's wall-clock (iCalendar floating time, e.g. "take meds at 09:00 wherever I am"). The default for Events predating the timezone model. Distinct from an all-day Event, which is date-only. See ADR-0019.
_Avoid_: local event, zoneless event, naive event

**Theme preference**:
The user's chosen appearance — Light, Dark, or System — persisted locally and applied by toggling a `.dark` class on the document root. System defers to the OS setting; Light and Dark pin the appearance regardless of the OS. Despite the name, deliberately **not** one of the Preferences: it is stored per-device rather than on the User, so it does not follow them to another browser. See ADR-0014, ADR-0039.
_Avoid_: color scheme, mode, dark-mode toggle

**Settings**:
The overlay where a User adjusts everything that isn't a Calendar or an Event — their Preferences, their Account, their Reminder delivery, their app passwords, their Connections, import and export, and (for the Workspaces they administer) its Members and Groups. Opens *over* the calendar rather than replacing it: the grid stays where it was, and closing returns the User to exactly the view they left. Divided into a **Personal** group, whose Sections concern the one User, and a **Workspace** group, whose Sections concern the active Workspace and everyone in it. Each Section is individually addressable, so one can be linked to and survives a reload. See ADR-0049.
_Avoid_: settings page, preferences (that's the narrower concept below), config screen

**Section**:
One named part of Settings, reached from its rail and shown one at a time — never a flat scroll of all of them. The unit that carries a Settings group membership and its own address.
_Avoid_: tab, panel, settings page

## Preferences

**Preferences**:
The per-User settings that shape how the calendar is displayed to one User — Week start, Default view, Time format, and Working hours. Stored on the User and served to every browser they log in from, so a Preference follows the User rather than the device. None of them changes what an Event *is*: two Users with Access to one Calendar see the same Occurrences at the same instants, formatted differently. Theme preference is deliberately not one. See ADR-0039.
_Avoid_: settings (Settings is the modal these live in, not the concept), config, options, profile

**Week start**:
The weekday a displayed week begins on — the first column of the Month grid, the first day of the Week view, the leftmost column of the mini calendars, and the leading chip in the Custom recurrence dialog's "Repeat on" picker. Stored as a day index 0–6 (Sunday–Saturday). A viewer's Preference and nothing more: it never reaches an Event's Recurrence rule, whose `WKST` stays at the iCalendar default, so one Event yields the same Occurrences for every viewer and for every CalDAV client. See ADR-0039.
_Avoid_: first day, week offset, WKST (that belongs to the rule, not the viewer)

**Default view**:
The Active view a User's session starts at. Seeds Active view when a Session is established and is never written back — switching to Month mid-session changes Active view alone, and the next load returns to the Default view. Distinct from Active view, which is where the User is now, and deliberately not a memory of where they last were. See ADR-0039.
_Avoid_: initial view, starting view, preferred view, last view

**Time format**:
Whether times are rendered as 12-hour or 24-hour, applied wherever this app formats a time itself — Event chips, timed Event blocks, the hour gutter, and the Email a Reminder sends. Deliberately does not reach the browser's native time input, which renders in the browser's own locale and cannot be told otherwise. See ADR-0039.
_Avoid_: clock format, hour format, locale (this is one axis of a locale, not a locale)

**Working hours**:
The daily time range (minute-of-day precision) a User treats as their working day, shading the time outside it in Day and Week view. A visual hint only: every hour of the day stays rendered, live, and clickable, and no Occurrence is ever hidden, clipped, or scrolled out of reach by it. Absent by default, in which case nothing is shaded. Carries no availability meaning — it does not affect free/busy, scheduling, or Reminders. See ADR-0039.
_Avoid_: business hours, office hours, availability, day bounds

## Authentication

**User**:
The account record a Session belongs to, validated by the backend. An instance's first User is always bootstrapped (ADR-0010); every subsequent one either self-registers (gated by `ENABLE_SIGNUPS`, ADR-0044) or is created by accepting a Workspace Invite. A User is Active or Disabled, and belongs to zero or more Workspaces. There is no instance-wide role on a User — authority over other Users exists only inside a Workspace, as a Role on that Workspace's membership. Identified by Email; named, for display, by Name.
_Avoid_: account, profile

**Email**:
A User's unique, required login identifier — what a person types to sign in, what CalDAV Basic auth authenticates against (ADR-0024), and what a Share, a Workspace Invite, or an Attendee invite names to resolve an account. Case-insensitive throughout: one address is one account, at the login form and at the invite field alike, so a stray capital can neither create a second account nor split one person into two Attendees. Also the Email-Channel Reminder's recipient (ADR-0021) — every User always has one, so Email-Channel availability depends only on whether the self-hoster has SMTP configured, not on anything per-User. See ADR-0047, ADR-0058.
_Avoid_: username, login, identifier (in prose)

**Name**:
A User's display-only label — shown wherever a User is named in the UI (Share lists, the User directory, Workspace membership, Event/Attachment attribution). Not unique: two Users may share one. Never used for authentication or to resolve an account. See ADR-0047.
_Avoid_: username, display name (redundant once "Name" is the established term)

**Disabled**:
The reversible account state in which a User cannot log in, refresh a Session, or authenticate over CalDAV, and receives no Reminders. Self-chosen and self-reversible only — with no instance-wide Admin (ADR-0044), nobody can disable another User's account for them. Blocked while the User is the sole Owner of any Workspace that still has other Members in it. Everything they own stays live and unchanged for everyone else. Distinct from deletion, which removes the account and demands an explicit disposition for the Calendars it owned. See ADR-0037, ADR-0044.
_Avoid_: suspended, deactivated, locked, banned

**Session**:
The logged-in state for one User, established by the backend after validating credentials and outliving every Access token issued within it. Present or absent — an expired Access token is not a partial Session, because a live Session silently yields another one. A Session ends only by logging out, by expiring, or by being invalidated: a password change ends every Session the User had, and issues a new one to whoever made the change.
_Avoid_: login state, auth state

**Access token**:
The short-lived credential attached to authenticated requests, held in memory only on the client and re-obtained on page load via the Refresh token.
_Avoid_: auth token, bearer token

**Refresh token**:
The credential used to obtain a new Access token without re-entering credentials, via `refreshAccessToken`. Issued by the backend as an `httpOnly`, `Secure` cookie — see ADR-0009.
_Avoid_: renewal token

**App password**:
A per-User, revocable credential a native calendar client uses to authenticate over CalDAV (HTTP Basic), distinct from the login password and shown to the User only once when generated. Stored hashed, one row per generated credential, so a single device can be revoked without touching the account password. The successor to the abandoned feed token. See ADR-0024.
_Avoid_: app-specific password (in prose), device password, API key

## Workspaces

**Workspace**:
The account-management and billing boundary: a named container a User owns or belongs to, holding a group of Calendars (ADR-0045) and the Members who can be given Access to them. A self-hosted instance may host several independent Workspaces side by side; a User may belong to several Workspaces at once, switching between them in the UI, which changes the entire visible Calendar and member list. See ADR-0044.
_Avoid_: instance (an instance may host many Workspaces), account (that's the User), organization, team

**Workspace Role**:
What a `WorkspaceMember` row grants over *that Workspace's membership and settings* — **Owner**, **Admin**, or **Member**. Grants no Calendar Access by itself: Owner and Admin resolve Access exactly like a Member does, through ownership or a Share. Owner alone may manage billing, delete the Workspace, and grant or revoke Admin. Admin may invite and remove Members (not Owner, not another Admin), manage Groups, create workspace-facing shared Calendars, and configure workspace settings. See ADR-0044.
_Avoid_: permission, admin (ambiguous with the retired instance-wide Admin, ADR-0037), role (say Workspace Role to distinguish from a Calendar Share's Role, ADR-0034)

**Invite**:
A single-use, expiring credential a Workspace's Owner or Admin issues for an email address, granting that email a Membership in the Workspace once accepted. Workspace-scoped, not account-level, Calendar-level or Event-level — a deliberately different concept from both Share and Invitation, which is why all three may share the English word "invite" in conversation without being the same thing in this glossary. Accepting one also sweeps up any outstanding email-shaped Attendee rows for that address on Events in the Workspace (ADR-0058). If no User exists with that email, accepting creates one (name, email, password) and joins them to the Workspace in one step; if one already exists, accepting simply adds the Membership. See ADR-0044.
_Avoid_: invite token (in prose — the credential and the act are one term here), invitation, activation link

## Sharing

**Owner**:
The single User a Calendar belongs to — the only one who may rename, recolour, delete, or share it, or give it a Source. Implicit rather than granted: an Owner has no Share. Every Calendar has exactly one, and it does not change except by an explicit transfer when the Owner's account is removed from the Calendar's Workspace or deleted. Unchanged by Workspace Role — a Workspace Admin who creates a Calendar owns it personally, the same as anyone else. See ADR-0034, ADR-0044, ADR-0045.
_Avoid_: creator, admin (that's a Workspace Role), author

**Share**:
The grant binding one Calendar to one User or Group with one Role. What an Owner creates and revokes, and what a User may renounce to leave a Calendar. The Owner never has one. Its target must be a User or Group belonging to the Calendar's own Workspace. See ADR-0034, ADR-0045.
_Avoid_: invite (that word is reserved for the Workspaces section's Invite — getting a Membership, not Access to a Calendar), permission, membership, ACL entry

**Group**:
A named set of a Workspace's Members (e.g. "Tech team"), created and membership-managed by that Workspace's Owner or Admin, usable as a Share target in place of a single User. Access granted to a Group is resolved dynamically against current membership — joining or leaving the Group changes Access immediately, with no Calendar re-shared. See ADR-0045.
_Avoid_: team (reserved loosely for describing groups in prose; Group is the modelled term), role (a Group is a set of people, not a permission level)

**Role**:
What a Share permits: **Viewer** (read the Calendar's Events and download their Attachments) or **Editor** (create, edit, and delete them, and add or remove their Attachments). Neither governs Reminders, which are personal to every User with Access and settable by a Viewer on an Event they may not otherwise touch (ADR-0064). Neither permits managing the Calendar itself — rename, delete, re-share, revoke, and giving it a Source are Owner-only. See ADR-0034, ADR-0040.
_Avoid_: permission, level, access level (Access is the resolved value, not this), Workspace Role (a different axis — see Workspaces)

**Access**:
The resolved answer to "what may this User do with this Calendar" — Owner, Editor, Viewer, or None. Computed on demand, never stored: ownership first, then the highest Role from any direct Share or a Share on a Group the User currently belongs to, then None, and finally clamped to read-only if the Calendar carries a Source. The single question every permission check asks, replacing the single-user era's "does this row's user match". See ADR-0034, ADR-0045.
_Avoid_: permission, privilege (that's the CalDAV property), rights

There is deliberately **no term for a Calendar someone shared with you.** "Shared Calendar" points both ways — the Owner shares it out, the recipient sees it shared in — and would sit confusingly beside Subscribed Calendar, which is also a Calendar you see and may not write. Say "a Calendar you have Editor Access to" instead. UI copy may still read "Shared with me"; that is a label, not a term.

**Organizer**:
The User who created an Event — its iCalendar `ORGANIZER` counterpart to `ATTENDEE`, and deliberately never one of its own Attendees. Held on the Event itself rather than as an Attendee row, so an Organizer carries no Response, cannot be invited, removed, or decline their own Event, and an Event whose Organizer's account was deleted simply has none — typing their own address into the invite field is rejected rather than quietly dropped. Distinct from a Calendar's Owner, which is an Access concept on a different object. Named on the wire as `ORGANIZER`, whose address differs by where the Event is being serialized to (ADR-0059). See ADR-0055.
_Avoid_: creator, owner (that's the Calendar's), host, chair

**Attendee**:
A party invited to one specific Event, independent of any Calendar Access — the invite itself is the visibility grant, scoped to that Event alone. Identified either by a User or, when no account here matches, by a bare email address. There is deliberately **no separate term for the second kind**: the Invitation, the Response set and the `ATTENDEE` line are identical either way, and all that differs is whether this instance happens to hold an account for them. An address is resolved against the Members of the Event's own Workspace when the invite is written, and an outstanding email-shaped Attendee converts to its User if that address later joins the Workspace — a fallback we wrote because we couldn't find their account has no reason to outlive the account appearing. May be invited individually, by typing an address, or via a Group (expanded to its current members at invite time into individual Attendee rows, since a response has to survive later Group membership changes rather than tracking membership dynamically the way a Group Share does); a Group never contains an outside address. Only a User-backed Attendee receives Notifications and may set Reminders here — an emailed one is reminded by their own calendar client, which holds the Invitation. Still not mirrored onto a Linked Calendar, now because a Refresh-written Attendee must be provably incapable of generating an Invitation and that guarantee isn't built. See ADR-0046, ADR-0052, ADR-0058, ADR-0059.
_Avoid_: invitee, guest, external attendee (as a term — say "an Attendee who isn't a Member" in prose), participant

**Response**:
An Attendee's answer to their invite — **Needs-Action** (the default, unanswered), **Accepted**, **Declined**, or **Tentative** — iCalendar's `PARTSTAT` values, modelled in full for lossless CalDAV round-tripping even though the web UI leads with only Accept/Decline. Set in the app by a Member, and by replying to the Invitation in an ordinary mail client by anyone — the same four values arrive either way, so an emailed answer needs no translation. See ADR-0046, ADR-0059.
_Avoid_: RSVP (the act, not the stored value), status (ambiguous with account state)

**Invitation**:
The iCalendar message this instance emails an Attendee — a `METHOD:REQUEST` that an ordinary mail client renders as an invite card carrying its own Accept/Decline/Tentative. The only RSVP surface an emailed Attendee has, since this app publishes no link they could click instead. Carries the Event but no `VALARM`, so the recipient's client applies its own default alarm — the Organizer has no Reminders to send them in the first place, since Reminders belong to Users rather than to the Event (ADR-0064). Re-sent with an incremented `SEQUENCE` when the Event materially changes — start, end, Recurrence rule, or all-day — and withdrawn by a `METHOD:CANCEL` when the Attendee is removed or the Event deleted. Reaches every Attendee, Member or not: it is the one message a User cannot turn off, because not receiving it means missing a meeting. Distinct from an Invite, which grants Workspace membership, and from a Reminder, which is a cue before an Occurrence rather than a request for an answer. See ADR-0059.
_Avoid_: invite (reserved for the Workspaces section's Invite), event invite, meeting request, iMIP (that's the mechanism)

## Attachments

**Attachment**:
A file uploaded to this instance and held against one Master, shown on every Occurrence of that series. Always bytes this instance stores — a link to a file elsewhere is not an Attachment. Such a link is representable, as the Event URL, but only as a link: one per Event, never downloaded, never sized, never inlined into a Calendar file, and never emitted as `ATTACH`. Belongs to the series rather than to any one Occurrence, so an Override cannot carry one of its own: "this week's slides" is deliberately unrepresentable. Downloadable by anyone with Access, added and removed by an Owner or Editor, and never present on a Calendar with a Source. See ADR-0040.
_Avoid_: file, upload, document, enclosure, asset

**Managed ID**:
The identifier CalDAV addresses an Attachment by — RFC 8607's `MANAGED-ID`, minted by this instance and meaningful only within it. Also the Attachment's id and the name of its file on disk: one value, so there is no mapping to keep honest. Only ever minted, never accepted from a client, which is what makes "you may not reuse someone else's Attachment" true by construction rather than by a check. See ADR-0040.
_Avoid_: attachment id (they are the same thing — say Managed ID), handle, token

## Sync

**Calendar object**:
The unit CalDAV addresses — one `.ics` resource per Event *series*, sharing a single iCalendar `UID` (the Master's id) and bundling the Master, all its Overrides, and its Exceptions in one file. The CalDAV-visible counterpart to the many rows a series occupies and to the Occurrences it expands to: one Calendar object, one series, many rows. Exposed at `{masterId}.ics`. Its Attachments are referenced by `ATTACH`, never carried inside it. See ADR-0025, ADR-0040.
_Avoid_: calendar resource, ics file, vevent

## Interchange

**Calendar file**:
A standalone `.ics` document exchanged with the world outside this instance — downloaded to hand to a person or another app, or uploaded to bring foreign events in. Standalone is the operative word: it carries its Attachments' bytes inside it, because a reference to this instance means nothing to whoever receives it. Distinct from a Calendar object: that is the sync unit CalDAV addresses (always exactly one series, referencing its Attachments rather than carrying them), whereas a Calendar file may hold one Event, one whole Calendar, or a foreign app's entire export. See ADR-0030, ADR-0041.
_Avoid_: ics file, export file, calendar export

**Import summary**:
The counted account of what an import did to a Calendar file's contents — what was skipped (and why), what was imported but altered, and what was ignored as unmodelled. Shown before the import is confirmed and again after it runs. The user's only view of a deliberately lossy translation, so it is part of the feature rather than a diagnostic. See ADR-0030.
_Avoid_: import report, import log, import result

**Export summary**:
The Import summary's mirror: the counted account of what leaving this instance will cost a Calendar file's contents — currently the Attachments too large to inline. Shown *before* the file is produced and only when there is something to say, so an export that loses nothing stays a single click. Exists for the same reason the Import summary does, and only that reason: a human is watching a one-time act, so the loss is disclosed while they can still act on it rather than being silently normalized. See ADR-0041.
_Avoid_: export report, export warning, export preview (there is nothing to preview when nothing is lost)

## External sources

**Source**:
The mark on a Calendar meaning "these Events come from somewhere else, and this is not where they are written". Exactly two kinds: a Subscription and a Connection-derived link. The single thing every read-only guard, the poller and the CalDAV privilege set ask about, so a Calendar has zero or one. Read-only is a property of a Source's *mode* rather than of a Source existing, because both kinds are read-only today and neither is required to stay that way. See ADR-0052.
_Avoid_: external source (redundant), origin, upstream, sync config

**Subscription**:
The kind of Source that binds a Calendar to an external `.ics` URL, together with the state a Refresh needs and leaves behind — poll cadence, last success, last error, and whether the feed's alarms are kept. One Calendar, one URL. The thing a User adds and removes; the Calendar it binds is what they see. See ADR-0032, ADR-0052.
_Avoid_: feed (see Subscribed Calendar), subscribed URL

**Subscribed Calendar**:
A Calendar whose Source is a Subscription. An ordinary Calendar in every respect a User can see — named, coloured, toggleable, rendered on the grid, exposed over CalDAV — except that it is read-only: its Events are written only by Refresh, never by the web app, the API, or a native client. Read-only for its Owner and for every Editor alike, since a Source clamps Access to read-only for everyone. Distinct from a Calendar whose Events merely arrived by import, which is an ordinary Calendar the User owns outright. See ADR-0032, ADR-0034.
_Avoid_: external calendar, remote calendar, read-only calendar, feed — "Calendar feed" meant the opposite direction (a feed this instance publishes) in the superseded ADR-0012, so the word points both ways in this repo and is best avoided entirely.

**Provider**:
An external calendar system this instance can be authorized against — Google today, Microsoft next. Reached through its own API rather than through CalDAV, since Microsoft has never supported CalDAV and a per-Provider seam is therefore forced regardless. Absent from the UI on any instance whose self-hoster has not supplied OAuth credentials for it. See ADR-0050, ADR-0051.
_Avoid_: integration, service, backend, platform

**Connection**:
One User's authorized link to one account at one Provider, carrying the tokens, that account's email, the granted scopes, and whether it is live, expired or revoked. The kind of Source that yields *many* Calendars from a single grant — which is what distinguishes it from a Subscription, and why it is an entity above Calendar rather than more columns on one. Belongs to the User, not to a Workspace: one grant serves every Workspace they are in. See ADR-0052.
_Avoid_: account link, integration, OAuth grant (that is the mechanism, not the thing), provider account

**Linked Calendar**:
A Calendar mirrored from one calendar of a Connection. Ordinary and read-only in exactly the way a Subscribed Calendar is, and Shareable to Workspace Members like any other Calendar, but distinguished by what stands behind it: a Connection the connecting User authorized, not a URL anyone can paste. Owned by that User even when the underlying calendar was merely shared to them at the Provider. Left out of its Owner's CalDAV home-set by default, since they almost certainly sync that calendar natively already. See ADR-0052, ADR-0054.
_Avoid_: mirrored calendar, connected calendar (too close to Connection), Google calendar, external calendar

**Refresh**:
One fetch-and-reconcile cycle against a Source: for a Subscription a conditional GET that usually ends in "unchanged", for a Connection a cursored request that usually returns nothing changed. Either way, a per-series reconciliation against the Events already stored. Deliberately not called sync — Sync in this repo means CalDAV's two-way, change-tracked exchange, and a Refresh is one-way and driven by a poller. Runs in one of two modes, and which one it is decides whether an absent series may be tombstoned. See ADR-0033, ADR-0053.
_Avoid_: sync, poll, update, pull

**Full Refresh**:
The Refresh mode that reconciles a complete listing: everything the Source still carries is present, so a series absent from it has genuinely been deleted and is tombstoned. Used for a Source's first Refresh and to recover from an expired cursor — never by wiping and re-inserting, which would re-mint every row id and make every CalDAV client re-download the collection. See ADR-0053.
_Avoid_: full sync, initial sync, resync

**Delta Refresh**:
The Refresh mode that reconciles only what a Provider reports as changed since a cursor. Absence means *unchanged*, so deletions are applied only where the Provider states one explicitly, and tombstone-by-absence is structurally unreachable rather than merely switched off. The distinction is not a nicety: applying Full Refresh's rules to delta input deletes an entire Calendar. See ADR-0053.
_Avoid_: incremental sync, partial refresh

**External UID**:
The foreign identifier a Refresh matches a series on — a feed's iCalendar `UID`, or a Provider's own event id — stored alongside the Event rather than as its id. The id stays a minted UUID, so a Calendar with a Source keeps the `{masterId}.ics` shape ADR-0025 requires and a 200-character foreign id never reaches a URL. The narrow reopening of ADR-0030's discard-the-foreign-UID rule, and the reason a Refresh can leave an unchanged series untouched instead of rewriting the whole Calendar. See ADR-0033.
_Avoid_: source UID, foreign ID, original UID

## Deployment

**Data directory**:
The single directory (`DATA_DIR`, default `/data`) under which the backend stores all persistent state — the SQLite database and every Attachment's bytes, plus any future runtime data. The one path a self-hoster needs to mount as a volume, and now the only place where losing it loses data the database cannot describe.
_Avoid_: data dir (in prose), storage path
