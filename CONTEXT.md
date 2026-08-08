# Calendar

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
The single color a Calendar is displayed in, inherited by every Occurrence it contains — an arbitrary sRGB value with an alpha channel, not a choice from a fixed set. Set by the Owner and shared by every client rather than being each client's private preference: whoever writes last wins, there is no conflict detection, and no bound on how long two clients may disagree. A User with Access may shadow it with a personal override that applies to them alone. See ADR-0029, ADR-0038.
_Avoid_: calendar colour (in prose), event color (Events have none of their own), color enum

**Swatch**:
One of the eight curated colors the web app offers as one-click quick picks when choosing a Calendar color. A convenience for the person picking, never a constraint on what a Calendar color may be — a Calendar whose color matches no Swatch is ordinary, not invalid, and is what a native client setting its own color normally produces.
_Avoid_: palette color, preset, theme color

**Event**:
The stored, saved unit — a titled time block belonging to a single Calendar, with a start, an end, zero or more Reminders, and an optional Recurrence rule. Its start/end define the first occurrence and the duration every Occurrence inherits. A non-recurring Event is a series of one. Distinct from the Occurrences it produces on the grid.
_Avoid_: appointment, meeting, entry

**Recurrence rule**:
The rule on an Event describing how it repeats, stored as an iCalendar `RRULE` string (RFC 5545) so it round-trips through CalDAV unchanged. Absent on a non-recurring Event. See ADR-0016.
_Avoid_: repeat pattern, schedule, recurrence pattern

**Occurrence**:
A single dated instance produced by expanding an Event's Recurrence rule over a date window. Occurrences are computed, never stored; they are what the grid actually renders (an Event chip or timed block is an Occurrence, not an Event). A non-recurring Event yields exactly one Occurrence. Identified by `(eventId, occurrenceStart)`, where `occurrenceStart` is the datetime the rule produced (its iCalendar `RECURRENCE-ID`).

**Master**:
The Event row that carries the Recurrence rule and anchors a series. Its title/start/end/calendar are the defaults every Occurrence inherits unless an Override replaces them, and it is the row a series' Attachments hang off. A non-recurring Event is a Master with no rule.
_Avoid_: parent event, root event

**Override**:
A stored Event row that replaces one Occurrence of a series with its own full title/start/end/calendar, keyed to the Occurrence it replaces by its original start (iCalendar `RECURRENCE-ID`). Produced by editing "this event" on a recurring Occurrence. A complete standalone instance, not a diff.
_Avoid_: exception (that's the cancellation), detached event, instance edit

**Exception**:
A cancelled single Occurrence — the rule still generates that slot, but it is suppressed from the grid (iCalendar `EXDATE`). Produced by deleting "this event" on a recurring Occurrence. Distinct from an Override, which replaces rather than removes.
_Avoid_: deleted occurrence, skip, override

**Series operation**:
One of the ordered steps a scoped edit (This event/This and following/All events) on a recurring Occurrence compiles down to — override an Occurrence, re-anchor a series at a split point, or put a single Event's fields wholesale. The same operation list is realized two ways: applied to the local cache, and dispatched to the API in order, so the two can never drift apart. See ADR-0016.
_Avoid_: edit step, mutation, transform

**All-day Event**:
An Event that occupies whole dates rather than a time range, flagged by `allDay`. Stored with the iCalendar half-open date convention — `start` is the date, `end` is the exclusive next day (a single-day all-day Event spans one day). Its start/end are wall-clock dates, serialized as date-only strings and never timezone-converted. Rendered in the all-day lane, not on the hourly grid. Multi-day all-day Events are out of scope for now.
_Avoid_: full-day event, date event

**All-day lane**:
The horizontal strip above the hourly grid in Day and Week view where all-day Occurrences are shown, separate from the timed grid below it.
_Avoid_: all-day row, header row

**Draft block**:
A proposed time window for a not-yet-saved Event, before it is confirmed via the creation modal — whether it originated from dragging on the grid or from a default (e.g. the sidebar's Create button). Discarded, not saved, if the modal is cancelled.
_Avoid_: pending event, temp event

**Reminder**:
A single cue attached to an Event that fires a fixed offset before an Occurrence's start, on one Channel. An Event carries zero or more, each with its own offset and Channel. Maps to one iCalendar `VALARM` (`TRIGGER` for the offset, `ACTION` for the Channel) so it round-trips through CalDAV unchanged. See ADR-0020.
_Avoid_: alert

**Reminder override**:
One User's personal modifier applied to *every* Reminder on an Event — one offset, one Channel, one muted flag per User per Event, substituted into each of the Event's Reminders wherever it sets a value. Deliberately not a personal replacement list: a User cannot add a Reminder, remove one, or retune two of them differently, only shift all of them together. A server-side delivery preference, not a change to the Event: the Event's own Reminders are still what every CalDAV client receives as `VALARM`s, so a device that fires its own alarms fires the Event's timing rather than the override. See ADR-0036.
_Avoid_: personal reminder, custom alarm, snooze, personal reminder list (it is one modifier, not a list)

**Channel**:
The delivery method of a Reminder — **Notification** (shown in-app) or **Email** (sent to the User). Corresponds to the iCalendar `VALARM` `ACTION` (`DISPLAY` / `EMAIL`).
_Avoid_: type, kind, action (in prose)

**Notification**:
The persistent in-app record the server creates when a Reminder on the Notification Channel fires. Lives in the Notification feed and, if a tab is open, is mirrored as a browser notification. A Notification-Channel Reminder is the rule; a Notification is the delivered instance it produces. Distinct from a toast, which is a transient client-only UI message (e.g. an error). See ADR-0021.
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

## Preferences

**Preferences**:
The per-User settings that shape how the calendar is displayed to one User — Week start, Default view, Time format, and Working hours. Stored on the User and served to every browser they log in from, so a Preference follows the User rather than the device. None of them changes what an Event *is*: two Users with Access to one Calendar see the same Occurrences at the same instants, formatted differently. Theme preference is deliberately not one. See ADR-0039.
_Avoid_: settings (Settings is the page these live on, not the concept), config, options, profile

**Week start**:
The weekday a displayed week begins on — the first column of the Month grid, the first day of the Week view, and the leftmost column of the mini calendars. Stored as a day index 0–6 (Sunday–Saturday). A viewer's Preference and nothing more: it never reaches an Event's Recurrence rule, whose `WKST` stays at the iCalendar default, so one Event yields the same Occurrences for every viewer and for every CalDAV client. See ADR-0039.
_Avoid_: first day, week offset, WKST (that belongs to the rule, not the viewer)

**Default view**:
The Active view a User's session starts at. Seeds Active view when a Session is established and is never written back — switching to Month mid-session changes Active view alone, and the next load returns to the Default view. Distinct from Active view, which is where the User is now, and deliberately not a memory of where they last were. See ADR-0039.
_Avoid_: initial view, starting view, preferred view, last view

**Time format**:
Whether times are rendered as 12-hour or 24-hour, applied wherever this app formats a time itself — Event chips, timed Event blocks, the hour gutter, and the Email a Reminder sends. Deliberately does not reach the browser's native time input, which renders in the browser's own locale and cannot be told otherwise. See ADR-0039.
_Avoid_: clock format, hour format, locale (this is one axis of a locale, not a locale)

**Working hours**:
The daily hour range a User treats as their working day, shading the hours outside it in Day and Week view. A visual hint only: every hour of the day stays rendered, live, and clickable, and no Occurrence is ever hidden, clipped, or scrolled out of reach by it. Absent by default, in which case nothing is shaded. Carries no availability meaning — it does not affect free/busy, scheduling, or Reminders. See ADR-0039.
_Avoid_: business hours, office hours, availability, day bounds

## Authentication

**User**:
The account record a Session belongs to, validated by the backend. An instance holds many — the first is bootstrapped (ADR-0010), the rest are created by an Admin (ADR-0037). Either enabled or Disabled.
_Avoid_: account, profile

**Admin**:
A User who may create, Disable, delete, and reset the password of other Users, and grant or revoke Admin. Authority over *who exists*, and nothing else — an Admin has no access to another User's Calendars or Events and needs a Share like anyone else. The last remaining Admin can be neither Disabled nor deleted. See ADR-0037.
_Avoid_: owner (that's a Calendar's), superuser, root, administrator (in prose)

**Disabled**:
The reversible account state in which a User cannot log in, refresh a Session, or authenticate over CalDAV, and receives no Reminders. Everything they own stays live and unchanged for everyone else — Disabling an account never degrades another User's Calendars. Distinct from deletion, which removes the account and demands an explicit disposition for the Calendars it owned. See ADR-0037.
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

## Sharing

**Owner**:
The single User a Calendar belongs to — the only one who may rename, recolour, delete, or share it, or bind it to a Subscription. Implicit rather than granted: an Owner has no Share. Every Calendar has exactly one, and it does not change except by an explicit transfer when the Owner's account is deleted. See ADR-0034.
_Avoid_: creator, admin (that's the instance role), author

**Share**:
The grant binding one Calendar to one User with one Role. What an Owner creates and revokes, and what a User may renounce to leave a Calendar. The Owner never has one.
_Avoid_: invite, permission, membership, ACL entry

**Role**:
What a Share permits: **Viewer** (read the Calendar's Events and download their Attachments) or **Editor** (create, edit, and delete them, set their Reminders, and add or remove their Attachments). Neither permits managing the Calendar itself — rename, delete, re-share, revoke, and binding a Subscription are Owner-only. See ADR-0034, ADR-0040.
_Avoid_: permission, level, access level (Access is the resolved value, not this)

**Access**:
The resolved answer to "what may this User do with this Calendar" — Owner, Editor, Viewer, or None. Computed on demand, never stored: ownership first, then a Share's Role, then None, and finally clamped to read-only if the Calendar carries a Subscription. The single question every permission check asks, replacing the single-user era's "does this row's user match". See ADR-0034.
_Avoid_: permission, privilege (that's the CalDAV property), rights

There is deliberately **no term for a Calendar someone shared with you.** "Shared Calendar" points both ways — the Owner shares it out, the recipient sees it shared in — and would sit confusingly beside Subscribed Calendar, which is also a Calendar you see and may not write. Say "a Calendar you have Editor Access to" instead. UI copy may still read "Shared with me"; that is a label, not a term.

## Attachments

**Attachment**:
A file uploaded to this instance and held against one Master, shown on every Occurrence of that series. Always bytes this instance stores — a link to a file elsewhere is not an Attachment and is not modelled. Belongs to the series rather than to any one Occurrence, so an Override cannot carry one of its own: "this week's slides" is deliberately unrepresentable. Downloadable by anyone with Access, added and removed by an Owner or Editor, and never present on a Subscribed Calendar. See ADR-0040.
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

## Subscriptions

**Subscription**:
The binding of a Calendar to an external `.ics` URL, together with the state a Refresh needs and leaves behind — poll cadence, last success, last error, and whether the feed's alarms are kept. The thing a User adds and removes; the Calendar it binds is what they see. See ADR-0032.
_Avoid_: feed (see Subscribed Calendar), subscribed URL, external source

**Subscribed Calendar**:
A Calendar carrying a Subscription. An ordinary Calendar in every respect a User can see — named, coloured, toggleable, rendered on the grid, exposed over CalDAV — except that it is read-only: its Events are written only by Refresh, never by the web app, the API, or a native client. Read-only for its Owner and for every Editor alike, since a Subscription clamps Access to read-only for everyone. Distinct from a Calendar whose Events merely arrived by import, which is an ordinary Calendar the User owns outright. See ADR-0032, ADR-0034.
_Avoid_: external calendar, remote calendar, read-only calendar, feed — "Calendar feed" meant the opposite direction (a feed this instance publishes) in the superseded ADR-0012, so the word points both ways in this repo and is best avoided entirely.

**Refresh**:
One fetch-and-reconcile cycle against a Subscription's URL: a conditional GET that usually ends in "unchanged", and otherwise a per-series reconciliation of the fetched Calendar file against the Events already stored. Deliberately not called sync — Sync in this repo means CalDAV's two-way, change-tracked exchange, and a Refresh is one-way, whole-document, and driven by a poller. See ADR-0033.
_Avoid_: sync, poll, update, pull

**External UID**:
The foreign iCalendar `UID` a Refresh matches a series on, stored alongside the Event rather than as its id — the id stays a minted UUID, so a Subscribed Calendar's Calendar objects keep the `{masterId}.ics` shape ADR-0025 requires. The narrow reopening of ADR-0030's discard-the-foreign-UID rule, and the reason a Refresh can leave an unchanged series untouched instead of rewriting the whole Calendar. See ADR-0033.
_Avoid_: source UID, foreign ID, original UID

## Deployment

**Data directory**:
The single directory (`DATA_DIR`, default `/data`) under which the backend stores all persistent state — the SQLite database and every Attachment's bytes, plus any future runtime data. The one path a self-hoster needs to mount as a volume, and now the only place where losing it loses data the database cannot describe.
_Avoid_: data dir (in prose), storage path
